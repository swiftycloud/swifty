package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

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
	swys3api.SwyS3_AccessKey,
	swys3api.SwyS3_AdminToken,
	"Content-Type",
	"Content-Length",
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

func logRequest(r *http.Request) {
	var request []string

	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, "\n---")
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
	log.Debug(strings.Join(request, "\n"))
}

// List all buckets belonging to an account
func handleListBuckets(iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	buckets, err := s3ListBuckets(iam)
	if err != nil { return err }

	HTTPRespXML(w, buckets)
	return nil
}

func handleListUploads(bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	uploads, err := s3Uploads(iam, bname)
	if err != nil { return err }

	HTTPRespXML(w, uploads)
	return nil
}

func handleListObjects(bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	objects, err := s3ListBucket(iam, bname, "")
	if err != nil { return err }

	HTTPRespXML(w, objects)
	return nil
}

func handlePutBucket(bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	if err := s3InsertBucket(iam, bname, canned_acl); err != nil {
		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteBucket(bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3DeleteBucket(iam, bname, "")
	if err != nil { return err }

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleAccessBucket(bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3CheckAccess(iam, bname, "")
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleBucket(iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname string = mux.Vars(r)["BucketName"]
	var policy = &iam.Policy

	if bname == "" && r.Method != http.MethodGet {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	switch r.Method {
	case http.MethodGet:
		if bname == "" {
			if !policy.isRoot() { goto e_access }
			return handleListBuckets(iam, w, r)
		}
		if _, ok := getURLParam(r, "uploads"); ok {
			if !policy.mayAccess(bname) { goto e_access }
			return handleListUploads(bname, iam, w, r)
		}
		if !policy.mayAccess(bname) { goto e_access }
		return handleListObjects(bname, iam, w, r)
	case http.MethodPut:
		if !policy.isRoot() { goto e_access }
		return handlePutBucket(bname, iam, w, r)
	case http.MethodDelete:
		if !policy.isRoot() { goto e_access }
		return handleDeleteBucket(bname, iam, w, r)
	case http.MethodHead:
		if !policy.mayAccess(bname) { goto e_access }
		return handleAccessBucket(bname, iam, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	return nil

e_access:
	return &S3Error{ ErrorCode: S3ErrAccessDenied }
}

func handleUploadFini(uploadId string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var complete swys3api.S3MpuFiniParts

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	err = xml.Unmarshal(body, &complete)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrMissingRequestBodyError }
	}

	resp, err := s3UploadFini(iam, bucket, uploadId, &complete)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadInit(oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	upload, err := s3UploadInit(iam, bucket, oname, canned_acl)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	resp := swys3api.S3MpuInit{
		Bucket:		bucket.Name,
		Key:		oname,
		UploadId:	upload.UploadID,
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadListParts(uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	resp, err := s3UploadList(bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadPart(uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var etag string
	var partno int

	if part, ok := getURLParam(r, "partNumber"); ok {
		partno, _ = strconv.Atoi(part)
	} else {
		return &S3Error{ ErrorCode: S3ErrInvalidArgument }
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	etag, err = s3UploadPart(iam, bucket, oname, uploadId, partno, body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}
	w.Header().Set("ETag", etag)

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleUploadAbort(uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3UploadAbort(bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleGetObject(oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	body, err := s3ReadObject(bucket, oname, 0, 1)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body)
	return nil
}

func handlePutObject(oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	object_size, err := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		object_size = 0
	}

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	_, err = s3AddObject(iam, bucket, oname, canned_acl, object_size, body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteObject(oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3DeleteObject(bucket, oname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleAccessObject(bname, oname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3CheckAccess(iam, bname, oname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleObject(iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname string = mux.Vars(r)["BucketName"]
	var oname string = mux.Vars(r)["ObjName"]
	var policy = &iam.Policy
	var bucket *S3Bucket
	var err error

	if bname == "" {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	} else if oname == "" {
		return &S3Error{ ErrorCode: S3ErrSwyInvalidObjectName }
	}

	if !policy.mayAccess(bname) {
		goto e_access
	}

	bucket, err = iam.FindBucket(bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	switch r.Method {
	case http.MethodPost:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			return handleUploadFini(uploadId, iam, bucket, w, r)
		} else if _, ok := getURLParam(r, "uploads"); ok {
			return handleUploadInit(oname, iam, bucket, w, r)
		}
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	case http.MethodGet:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			return handleUploadListParts(uploadId, oname, iam, bucket, w, r)
		}
		return handleGetObject(oname, iam, bucket, w, r)
	case http.MethodPut:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			return handleUploadPart(uploadId, oname, iam, bucket, w, r)
		}
		return handlePutObject(oname, iam, bucket, w, r)
	case http.MethodDelete:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			return handleUploadAbort(uploadId, oname, iam, bucket, w, r)
		}
		return handleDeleteObject(oname, iam, bucket, w, r)
	case http.MethodHead:
		return handleAccessObject(bname, oname, iam, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	w.WriteHeader(http.StatusOK)
	return nil

e_access:
	return &S3Error{ ErrorCode: S3ErrAccessDenied }
}

func handleS3API(cb func(iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var akey *S3AccessKey
		var iam *S3Iam
		var err error

		defer r.Body.Close()

		logRequest(r)

		if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		// Admin is allowed to process without signing a request
		if s3VerifyAdmin(r) == nil {
			access_key := r.Header.Get(swys3api.SwyS3_AccessKey)
			akey, err = LookupAccessKey(access_key)
		} else {
			akey, err = s3VerifyAuthorization(r)
		}

		if err == nil {
			if !akey.Expired() {
				iam, err = akey.s3IamFind()
			} else {
				err = fmt.Errorf("The access key is expired")
			}
		}

		if akey == nil || iam == nil || err != nil {
			HTTPRespError(w, S3ErrAccessDenied, err.Error())
		} else if e := cb(iam, akey, w, r); e != nil {
			HTTPRespS3Error(w, e)
		}
	})
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

	akey, err = genNewAccessKey(kg.Namespace, kg.Bucket, kg.Lifetime)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, &swys3api.S3CtlKeyGenResult{
			AccessKeyID:	akey.AccessKeyID,
			AccessKeySecret:s3DecryptAccessKeySecret(akey),
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
	var code int

	err = swyhttp.ReadAndUnmarshalReq(r, &breq)
	if err != nil {
		goto out
	}

	if breq.Acl == "" {
		breq.Acl = swys3api.S3BucketAclCannedPrivate
	}

	iam, err = s3FindFullAccessIam(breq.Namespace)
	if err != nil {
		goto out
	}

	if op == "badd" {
		err1 := s3InsertBucket(iam, breq.Bucket, breq.Acl)
		if err1 != nil {
			err = fmt.Errorf("%v", err1.ErrorCode)
			goto out
		}

		code = http.StatusCreated
	} else {
		err1 := s3DeleteBucket(iam, breq.Bucket, breq.Acl)
		if err1 != nil {
			err = fmt.Errorf("%v", err1.ErrorCode)
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

	bname_regex := `[a-zA-Z0-9\-]`
	oname_regex := `[a-zA-Z0-9\/\!\-\_\.\*\'\(\)]`
	match_bucket := fmt.Sprintf("/{BucketName:%s*}", bname_regex)
	match_object := fmt.Sprintf("/{BucketName:%s+}/{ObjName:%s+}", bname_regex, oname_regex)

	rgatesrv.Handle(match_bucket,	handleS3API(handleBucket))
	rgatesrv.Handle(match_object,	handleS3API(handleObject))

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

	err = gcInit(0)
	if err != nil {
		log.Fatalf("Can't setup garbage collector: %s", err.Error())
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
