package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"io/ioutil"
	"encoding/hex"
	"net/http"
	"strings"
	"strconv"
	"errors"
	"time"
	"flag"
	"fmt"

	"../common"
	"../common/http"
	"../common/secrets"
	"../apis/apps/s3"
)

var s3Secrets map[string]string
var s3SecKey []byte

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
	request = append(request, "---")
	return strings.Join(request, "\n")
//	return prefix
}

func handleListBuckets(w http.ResponseWriter, akey *S3AccessKey) {
	var list *ListAllMyBucketsResult
	var err error

	list, err = s3ListBuckets(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			HTTPRespError(w, S3ErrNoSuchBucket, err.Error())
		} else {
			HTTPRespError(w, S3ErrInternalError, err.Error())
		}
		return
	}

	err = HTTPRespXML(w, list)
}

func handleBucket(w http.ResponseWriter, r *http.Request) {
	var bucket_name string = mux.Vars(r)["BucketName"]
	var acl string
	var akey *S3AccessKey
	var err error

	log.Debug(formatRequest(fmt.Sprintf("handleBucket: bucket %v",
						bucket_name), r))

	akey, err = s3VerifyAuthorization(r)
	if err != nil {
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), BucketAcls) == false {
		r.Header.Set("x-amz-acl", S3BucketAclPrivate)
	}

	acl = r.Header.Get("x-amz-acl")

	if bucket_name == "" {
		if r.Method == http.MethodGet {
			handleListBuckets(w, akey)
			return
		} else {
			http.Error(w, "Empty bucket name provided", http.StatusBadRequest)
			return
		}
	}

	switch r.Method {
	case http.MethodPut:
		// Create a new bucket
		err = s3InsertBucket(akey, bucket_name, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		// List all objects
		objects, err := s3ListBucket(akey, bucket_name, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		HTTPRespXML(w, objects)
		return
		break
	case http.MethodDelete:
		// Delete a bucket
		err = s3DeleteBucket(akey, bucket_name, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodHead:
		// Check if we can access a bucket
		err = s3CheckAccess(akey, bucket_name, "")
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
	var bucket_name string = mux.Vars(r)["BucketName"]
	var object_name string = mux.Vars(r)["ObjName"]
	var acl string
	var object_size int64
	var akey *S3AccessKey
	var bucket *S3Bucket
	var body []byte
	var err error

	defer r.Body.Close()

	log.Debug(formatRequest(fmt.Sprintf("handleObject: bucket %v object %v",
						bucket_name, object_name), r))

	akey, err = s3VerifyAuthorization(r)
	if err != nil {
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	}

	if bucket_name == "" {
		http.Error(w, "Empty bucket name provided", http.StatusBadRequest)
		return
	} else if object_name == "" {
		http.Error(w, "Empty object name provided", http.StatusBadRequest)
		return
	}

	// Setup default ACL
	if verifyAclValue(r.Header.Get("x-amz-acl"), ObjectAcls) == false {
		r.Header.Set("x-amz-acl", S3ObjectAclPrivate)
	}

	bucket, err = akey.FindBucket(bucket_name)
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
		// Create new object in bucket
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
		break
	case http.MethodPut:
		body, err = ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("Can't read data: %s", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var object *S3Object
		// Create new object
		object, err = s3InsertObject(bucket, object_name, 1, object_size, acl)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err = s3CommitObject(akey.Namespace, bucket, object, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodGet:
		// List all objects
		body, err = s3ReadObject(bucket, object_name, 1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
		break
	case http.MethodDelete:
		// Delete a bucket
		err = s3DeleteObject(bucket, object_name, 1)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		break
	case http.MethodHead:
		// Check if we can access a bucket
		err = s3CheckAccess(akey, bucket_name, object_name)
		if err != nil {
			if err == mgo.ErrNotFound {
				http.Error(w, "No bucket/object found", http.StatusBadRequest)
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
	var kg swys3ctl.S3CtlKeyGen
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

	akey, err = genNewAccessKey(kg.Namespace)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, &swys3ctl.S3CtlKeyGenResult{
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
	var kd swys3ctl.S3CtlKeyDel
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

func handleAdminOp(w http.ResponseWriter, r *http.Request) {
	var op string = mux.Vars(r)["op"]
	var err error

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
	}

	http.Error(w, fmt.Sprintf("Unknown operation"), http.StatusBadRequest)
}

func handleNotify(w http.ResponseWriter, r *http.Request, subscribe bool) {
	var params swys3ctl.S3Subscribe

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

var S3ModeDevel bool

func main() {
	var config_path string
	var err error

	flag.BoolVar(&S3ModeDevel, "devel", false, "launch in development mode")
	flag.StringVar(&config_path,
			"conf",
				"",
				"path to a config file")
	flag.BoolVar(&radosDisabled,
			"no-rados",
				false,
				"disable rados")
	flag.Int64Var(&cachedObjSize,
			"cached-obj-size",
				S3StorageSizePerObj,
				"object size in bytes to put into cache")
	flag.Parse()

	if config_path != "" {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	if cachedObjSize > S3StorageSizePerObj {
		log.Errorf("Caching more than %d bytes is not allowed",
				S3StorageSizePerObj)
		return
	}

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
	match_object := "/{BucketName:[a-zA-Z0-9-.]+}/{ObjName:[a-zA-Z0-9-.]+}"

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
