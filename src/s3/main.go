package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"io/ioutil"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"strings"
	"strconv"
	"errors"
	"time"
	"flag"
	"fmt"
	"os"

	"../common"
	"../common/http"
	"../common/secrets"
	"../apis/apps/s3"
)

var s3Secrets map[string]string
var s3SecKey []byte
var S3ModeDevel bool

type YAMLConfCeph struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	Token		string			`yaml:"token"`
	LogLevel	string			`yaml:"loglevel"`
}

type YAMLConfDB struct {
	Name		string			`yaml:"state"`
	Addr		string			`yaml:"address"`
	User		string			`yaml:"user"`
	Pass		string			`yaml:"password"`
}

type YAMLConfNotifyRMQ struct {
	Target		string			`yaml:"target"`
	User		string			`yaml:"user"`
	Pass		string			`yaml:"password"`
}

type YAMLConfNotify struct {
	Rabbit		*YAMLConfNotifyRMQ	`yaml:"rabbitmq,omitempty"`
}

type YAMLConf struct {
	DB		YAMLConfDB		`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Ceph		YAMLConfCeph		`yaml:"ceph"`
	SecKey		string			`yaml:"secretskey"`
	Notify		YAMLConfNotify		`yaml:"notify"`
}

var conf YAMLConf
var log *zap.SugaredLogger
var adminsrv *http.Server
var gatesrv *http.Server

func setupLogger(conf *YAMLConf) {
	lvl := zap.WarnLevel

	if conf != nil {
		switch conf.Daemon.LogLevel {
		case "debug":
			lvl = zap.DebugLevel
			break
		case "info":
			lvl = zap.InfoLevel
			break
		case "warn":
			lvl = zap.WarnLevel
			break
		case "error":
			lvl = zap.ErrorLevel
			break
		}
	}

	zcfg := zap.Config {
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      true,
		DisableStacktrace:true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, _ := zcfg.Build()
	log = logger.Sugar()

	swy.InitLogger(log)
}

var CORS_Headers = []string {
	"Content-Type",
	"Content-Length",
	"X-SwyS3-Token",
	"Authorization",
	"X-Amz-Date",
	"x-amz-acl",
	"X-Amz-Content-Sha256",
	"X-Amz-User-Agent",
}

var CORS_Methods = []string {
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
	http.MethodGet,
	http.MethodHead,
}

func formatRequest(prefix string, r *http.Request) string {
	var request []string

	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, prefix)
	request = append(request, "---")
	request = append(request, url)
	request = append(request, fmt.Sprintf("Host: %v", r.Host))

	for name, headers := range r.Header {
		for _, h := range headers {
			request = append(request, fmt.Sprintf("%v:%v", name, h))
		}
	}

	content_type := r.Header.Get("Content-Type")
	if content_type != "" {
		if string(content_type[0:20]) == "multipart/form-data;" {
			r.ParseMultipartForm(0)
			request = append(request, fmt.Sprintf("MultipartForm: %v", r.MultipartForm))
		}
	}

	request = append(request, "---")
	return strings.Join(request, "\n")
//	return prefix
}

func handleListBuckets(w http.ResponseWriter, iam *S3Iam, akey *S3AccessKey) {
	var list *swys3api.S3BucketList
	var err error

	list, err = s3ListBuckets(iam, akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			HTTPRespError(w, S3ErrNoSuchBucket, err.Error())
		} else {
			HTTPRespError(w, S3ErrInternalError, err.Error())
		}
		return
	}

	HTTPRespXML(w, list)
}

func handleBucket(w http.ResponseWriter, r *http.Request) {
	var bname string = mux.Vars(r)["BucketName"]
	var acl string
	var akey *S3AccessKey
	var iam *S3Iam
	var err error
	var ok bool

	log.Debug(formatRequest(fmt.Sprintf("handleBucket: bucket %v",
						bname), r))

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	akey, err = s3VerifyAuthorization(r)
	if err == nil {
		iam, err = akey.s3IamFind()
	}

	if akey == nil || iam == nil || err != nil {
		HTTPRespError(w, S3ErrAccessDenied, err.Error())
		return
	}

	switch r.Method {
	case http.MethodPut:
	case http.MethodGet:
	case http.MethodDelete:
	case http.MethodHead:
		break
	default:
		HTTPRespError(w, S3ErrMethodNotAllowed)
		return
		break
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), BucketCannedAcls) == false {
		r.Header.Set("x-amz-acl", swys3api.S3BucketAclCannedPrivate)
	}

	acl = r.Header.Get("x-amz-acl")

	if bname == "" {
		if r.Method == http.MethodGet {
			// List all buckets belonging to us
			handleListBuckets(w, iam, akey)
			return
		} else {
			HTTPRespError(w, S3ErrInvalidBucketName)
			return
		}
	}

	switch r.Method {
	case http.MethodPut:
		// Create a new bucket
		err = s3InsertBucket(iam, akey, bname, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		// List active uploads on a bucket
		if _, ok = r.URL.Query()["uploads"]; ok {
			resp, err := s3Uploads(iam, akey, bname)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			HTTPRespXML(w, resp)
			return
		}
		// List all objects
		objects, err := s3ListBucket(iam, akey, bname, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		HTTPRespXML(w, objects)
		return
		break
	case http.MethodDelete:
		// Delete a bucket
		err = s3DeleteBucket(iam, akey, bname, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodHead:
		// Check if we can access a bucket
		err = s3CheckAccess(iam, bname, "")
		if err != nil {
			if err == mgo.ErrNotFound {
				HTTPRespError(w, S3ErrNoSuchBucket, "No bucket found")
			} else {
				HTTPRespError(w, S3ErrInternalError, err.Error())
			}
			return
		}
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	w.WriteHeader(http.StatusOK)
}


func handleObject(w http.ResponseWriter, r *http.Request) {
	var bname string = mux.Vars(r)["BucketName"]
	var oname string = mux.Vars(r)["ObjName"]
	var acl string
	var object_size int64
	var akey *S3AccessKey
	var iam *S3Iam
	var bucket *S3Bucket
	var upload *S3Upload
	var body []byte
	var err error
	var ok bool

	defer r.Body.Close()

	log.Debug(formatRequest(fmt.Sprintf("handleObject: bucket %v object %v",
						bname, oname), r))

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	akey, err = s3VerifyAuthorization(r)
	if err == nil {
		iam, err = akey.s3IamFind()
	}

	if akey == nil || iam == nil || err != nil {
		HTTPRespError(w, S3ErrAccessDenied, err.Error())
		return
	}

	switch r.Method {
	case http.MethodPost:
	case http.MethodPut:
	case http.MethodGet:
	case http.MethodDelete:
	case http.MethodHead:
		break
	default:
		HTTPRespError(w, S3ErrMethodNotAllowed)
		return
		break
	}

	if bname == "" {
		HTTPRespError(w, S3ErrInvalidBucketName)
		return
	} else if oname == "" {
		HTTPRespError(w, S3ErrInvalidObjectName)
		return
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), ObjectAcls) == false {
		r.Header.Set("x-amz-acl", swys3api.S3ObjectAclPrivate)
	}

	bucket, err = iam.FindBucket(akey, bname)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	object_size, err = strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		object_size = 0
	}

	acl = r.Header.Get("x-amz-acl")

	switch r.Method {
	case http.MethodPost:
		if _, ok = r.URL.Query()["uploads"]; ok {
			// Initialize an upload
			upload, err = s3UploadInit(iam, bucket, oname, acl)
			if err != nil {
				HTTPRespError(w, S3ErrInternalError,
					"Failed to initiate multipart upload")
				return
			}

			resp := swys3api.S3MpuInit{
				Bucket:		bucket.Name,
				Key:		oname,
				UploadId:	upload.UploadID,
			}
			HTTPRespXML(w, resp)
			return
		} else if _, ok = r.URL.Query()["uploadId"]; ok {
			// Finalize an upload

			var complete swys3api.S3MpuFiniParts

			body, err = ioutil.ReadAll(r.Body)
			if err != nil {
				HTTPRespError(w, S3ErrIncompleteBody,
					"Failed to read body")
				return
			}
			err = xml.Unmarshal(body, &complete)
			if err != nil {
				HTTPRespError(w, S3ErrIncompleteBody,
					"Failed to unmarshal body")
				return
			}
			resp, err := s3UploadFini(iam, bucket,
					r.URL.Query()["uploadId"][0], &complete)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			HTTPRespXML(w, resp)
			return
		} else if _, ok = r.URL.Query()["restore"]; ok {
			HTTPRespError(w, S3ErrNotImplemented,
				"Version restore not yet implemented")
		} else {
			HTTPRespError(w, S3ErrNotImplemented,
				"Form post not yet implemented")
		}
		return
		break
	case http.MethodPut:
		body, err = ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Can't read data: %s", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if _, ok = r.URL.Query()["uploadId"]; ok {
			// Upload a part of an object

			var etag string
			var part int

			if _, ok = r.URL.Query()["partNumber"]; ok {
				part, _ = strconv.Atoi(r.URL.Query()["partNumber"][0])
			}

			if !ok || part == 0 {
				HTTPRespError(w, S3ErrInvalidArgument,
						"Invalid part number")
				return
			}

			etag, err = s3UploadPart(iam, bucket,
				oname, r.URL.Query()["uploadId"][0],
				part, body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Create new object
		_, err = s3AddObject(iam, bucket, oname, acl, object_size, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		if _, ok = r.URL.Query()["uploadId"]; ok {
			// List parts of object in the upload

			resp, err := s3UploadList(bucket, oname,
						r.URL.Query()["uploadId"][0])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			HTTPRespXML(w, resp)
			return
		}

		// Read an object
		body, err = s3ReadObject(bucket, oname, 0, 1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
		break
	case http.MethodDelete:
		if _, ok = r.URL.Query()["uploadId"]; ok {
			// Delete upload and all parts
			err = s3UploadAbort(bucket, oname, r.URL.Query()["uploadId"][0])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			// Delete a bucket
			err = s3DeleteObject(bucket, oname)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		break
	case http.MethodHead:
		// Check if we can access an object
		err = s3CheckAccess(iam, bname, oname)
		if err != nil {
			if err == mgo.ErrNotFound {
				http.Error(w, "No object found", http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		break
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	w.WriteHeader(http.StatusOK)
}

func handleKeygen(w http.ResponseWriter, r *http.Request) {
	var akey *S3AccessKey
	var kg swys3api.S3CtlKeyGen
	var err error

	err = swyhttp.ReadAndUnmarshalReq(r, &kg)
	if err != nil {
		goto out
	}

	// FIXME Check for allowed values
	if kg.Namespace == "" {
		err = errors.New("Missing namespace name")
		goto out
	}

	akey, err = genNewAccessKey(kg.Namespace, kg.Bucket)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, &swys3api.S3CtlKeyGenResult{
			AccessKeyID:	akey.AccessKeyID,
			AccessKeySecret:akey.AccessKeySecret,
		})
	if err != nil {
		goto out
	}
	return

out:
	log.Errorf("Can't: %s", err.Error())
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleKeydel(w http.ResponseWriter, r *http.Request) {
	var kd swys3api.S3CtlKeyDel
	var err error

	err = swyhttp.ReadAndUnmarshalReq(r, &kd)
	if err != nil {
		goto out
	}

	if kd.AccessKeyID == "" {
		err = errors.New("Missing key")
		goto out
	}

	err = dbRemoveAccessKey(kd.AccessKeyID)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return
out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleBreq(w http.ResponseWriter, r *http.Request, op string) {
	var err error
	var breq swys3api.S3CtlBucketReq
	var iam *S3Iam
	var key *S3AccessKey
	var code int

	err = swyhttp.ReadAndUnmarshalReq(r, &breq)
	if err != nil {
		goto out
	}

	iam, err = s3IamFindByNamespace(breq.Namespace)
	if err != nil {
		goto out
	}

	if breq.Acl == "" {
		breq.Acl = swys3api.S3BucketAclCannedPrivate
	}

	key = iam.MakeBucketKey(breq.Bucket, breq.Acl)

	if op == "badd" {
		err = s3InsertBucket(iam, key, breq.Bucket, breq.Acl)
		if err != nil {
			goto out
		}

		code = http.StatusCreated
	} else {
		err = s3DeleteBucket(iam, key, breq.Bucket, breq.Acl)
		if err != nil {
			goto out
		}

		code = http.StatusNoContent
	}

	w.WriteHeader(code)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleAdminOp(w http.ResponseWriter, r *http.Request) {
	var op string = mux.Vars(r)["op"]
	var err error

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err = s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch op {
	case "keygen":
		handleKeygen(w, r)
		return
	case "keydel":
		handleKeydel(w, r)
		return
	case "badd":
		handleBreq(w, r, op)
		return
	case "bdel":
		handleBreq(w, r, op)
		return
	}

	http.Error(w, fmt.Sprintf("Unknown operation"), http.StatusBadRequest)
}

func handleNotify(w http.ResponseWriter, r *http.Request, subscribe bool) {
	var params swys3api.S3Subscribe

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	/* For now make it admin-only op */
	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if subscribe {
		err = s3Subscribe(&params)
	} else {
		err = s3Unsubscribe(&params)
	}

	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusAccepted)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleNotifyAdd(w http.ResponseWriter, r *http.Request) {
	handleNotify(w, r, true)
}

func handleNotifyDel(w http.ResponseWriter, r *http.Request) {
	handleNotify(w, r, false)
}

func main() {
	var config_path string
	var showVersion bool
	var err error

	flag.BoolVar(&S3ModeDevel, "devel", false, "launch in development mode")
	flag.StringVar(&config_path,
			"conf",
				"/etc/swifty/conf/s3.yaml",
				"path to a config file")
	flag.BoolVar(&radosDisabled,
			"no-rados",
				false,
				"disable rados")
	flag.BoolVar(&showVersion,
			"version",
				false,
				"show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("Version %s\n", Version)
		return
	}

	if _, err := os.Stat(config_path); err == nil {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	s3Secrets, err = swysec.ReadSecrets("s3")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	s3SecKey, err = hex.DecodeString(s3Secrets[conf.SecKey])
	if err != nil || len(s3SecKey) < 16 {
		log.Error("Secret key should be decodable and be 16 bytes long at least")
		return
	}

	// Service operations
	rgatesrv := mux.NewRouter()

	match_bucket := "/{BucketName:[a-zA-Z0-9-.]*}"
	match_object := "/{BucketName:[a-zA-Z0-9-.]+}/{ObjName:[a-zA-Z0-9-./]+}"

	rgatesrv.HandleFunc(match_bucket,	handleBucket)
	rgatesrv.HandleFunc(match_object,	handleObject)

	// Admin operations
	radminsrv := mux.NewRouter()
	radminsrv.HandleFunc("/v1/api/admin/{op:[a-zA-Z0-9-.]+}", handleAdminOp)
	radminsrv.HandleFunc("/v1/api/notify/subscribe", handleNotifyAdd)
	radminsrv.HandleFunc("/v1/api/notify/unsubscribe", handleNotifyDel)

	err = dbConnect(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to backend: %s",
				err.Error())
	}

	err = radosInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to rados: %s",
				err.Error())
	}

	err = notifyInit(&conf.Notify)
	if err != nil {
		log.Fatalf("Can't setup notifications: %s", err.Error())
	}

	err = dbRepair()
	if err != nil {
		log.Fatalf("Can't process db test/repair: %s", err.Error())
	}

	go func() {
		adminsrv = &http.Server{
			Handler:      radminsrv,
			Addr:         swy.MakeAdminURL(conf.Daemon.Addr, conf.Daemon.AdminPort),
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}

		err = adminsrv.ListenAndServe()
		if err != nil {
			log.Errorf("ListenAndServe: adminsrv %s", err.Error())
		}
	}()

	gatesrv = &http.Server{
			Handler:      rgatesrv,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
	}

	err = gatesrv.ListenAndServe()
	if err != nil {
		log.Errorf("ListenAndServe: gatesrv %s", err.Error())
	}

	radosFini()
	dbDisconnect()
}
