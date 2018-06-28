package main

import (
	"go.uber.org/zap"
	"gopkg.in/mgo.v2"
	"github.com/gorilla/mux"

	"io/ioutil"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/url"
	"context"
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

func isLite() bool { return Flavor == "lite" }

type YAMLConfCeph struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	Token		string			`yaml:"token"`
	LogLevel	string			`yaml:"loglevel"`
	Prometheus	string			`yaml:"prometheus"`
	HTTPS		*swyhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConfNotify struct {
	Rabbit		string			`yaml:"rabbitmq,omitempty"`
}

type YAMLConf struct {
	DB		string			`yaml:"db"`
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
}

type s3Context struct {
	context.Context
	S	*mgo.Session
}

func Dbs(ctx context.Context) *mgo.Session {
	return ctx.(*s3Context).S
}

func mkContext(id string) (context.Context, func(context.Context)) {
	ctx := &s3Context{
		context.Background(),
		session.Copy(),
	}

	return ctx, func(c context.Context) {
				Dbs(c).Close()
			}
}

var CORS_Headers = []string {
	swys3api.SwyS3_AccessKey,
	swys3api.SwyS3_AdminToken,
	"Content-Type",
	"Content-Length",
	"Content-MD5",
	"Authorization",
	"X-Amz-Date",
	"x-amz-acl",
	"x-amz-copy-source",
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
		if len(content_type) >= 21 &&
			string(content_type[0:20]) == "multipart/form-data;" {
			r.ParseMultipartForm(0)
			request = append(request, fmt.Sprintf("MultipartForm: %v", r.MultipartForm))
		}
	}

	request = append(request, "---")
	log.Debug(strings.Join(request, "\n"))
}

func handleBucketCloudWatch(ctx context.Context, iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname, v string

	content_type := r.Header.Get("Content-Type")
	if !strings.HasPrefix(content_type, "application/x-www-form-urlencoded")  {
		return &S3Error{
			ErrorCode: S3ErrInvalidURI,
			Message: "Unexpected Content-Type",
		}
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	request_map, err := url.ParseQuery(string(body[:]))
	if err != nil {
		return &S3Error{
			ErrorCode: S3ErrIncompleteBody,
			Message: "Unable to decode metrics query",
		}
	}

	v = urlValue(request_map, "Namespace")
	if v != "AWS/S3" {
		return &S3Error{
			ErrorCode: S3ErrIncompleteBody,
			Message: "Wrong/missing 'Namespace'",
		}
	}

	v = urlValue(request_map, "Action")
	if v != "GetMetricStatistics" {
		return &S3Error{
			ErrorCode: S3ErrIncompleteBody,
			Message: "Wrong/missing 'Action'",
		}
	}

	if urlValue(request_map, "Dimensions.member.1.Name") == "BucketName" {
		bname = urlValue(request_map, "Dimensions.member.1.Value")
	} else if urlValue(request_map, "Dimensions.member.2.Name") == "BucketName" {
		bname = urlValue(request_map, "Dimensions.member.2.Value")
	} else {
		return &S3Error{
			ErrorCode: S3ErrIncompleteBody,
			Message: "Wrong/missing 'BucketName'",
		}
	}

	res, e := s3GetBucketMetricOutput(ctx, iam, bname, urlValue(request_map, "MetricName"))
	if e != nil { return e }

	HTTPRespXML(w, res)
	return nil
}

// List all buckets belonging to an account
func handleListBuckets(ctx context.Context, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	buckets, err := s3ListBuckets(ctx, iam)
	if err != nil { return err }

	HTTPRespXML(w, buckets)
	return nil
}

func handleListUploads(ctx context.Context, bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	uploads, err := s3Uploads(ctx, iam, bname)
	if err != nil { return err }

	HTTPRespXML(w, uploads)
	return nil
}

func handleListObjects(ctx context.Context, bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	listType := getURLValue(r, "list-type")
	if listType != "2" {
		return &S3Error{
			ErrorCode: S3ErrInvalidArgument,
			Message: "Invalid list-type",
		}
	}

	params := &S3ListObjectsRP {
		Delimiter:	getURLValue(r, "delimiter"),
		Prefix:		getURLValue(r, "prefix"),
		ContToken:	getURLValue(r, "continuation-token"),
		StartAfter:	getURLValue(r, "start-after"),
		FetchOwner:	getURLBool(r, "fetch-owner"),
	}

	if v, ok := getURLParam(r, "max-keys"); ok {
		params.MaxKeys, _ = strconv.ParseInt(v, 10, 64)
	}

	objects, err := s3ListBucket(ctx, iam, bname, params)
	if err != nil { return err }

	HTTPRespXML(w, objects)
	return nil
}

func handlePutBucket(ctx context.Context, bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	if err := s3InsertBucket(ctx, iam, bname, canned_acl); err != nil {
		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteBucket(ctx context.Context, bname string, iam *S3Iam, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3DeleteBucket(ctx, iam, bname, "")
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

func handleBucket(ctx context.Context, iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname string = mux.Vars(r)["BucketName"]
	var policy = &iam.Policy

	if bname == "" {
		if r.Method == http.MethodPost {
			//
			// A special case where we
			// hande some subset of cloudwatch
			return handleBucketCloudWatch(ctx, iam, akey, w, r)
		} else if r.Method != http.MethodGet {
			return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
		}
	}

	switch r.Method {
	case http.MethodGet:
		if bname == "" {
			apiCalls.WithLabelValues("b", "ls").Inc()
			if !policy.isRoot() { goto e_access }
			return handleListBuckets(ctx, iam, w, r)
		}
		if _, ok := getURLParam(r, "uploads"); ok {
			if !policy.mayAccess(bname) { goto e_access }
			apiCalls.WithLabelValues("u", "ls").Inc()
			return handleListUploads(ctx, bname, iam, w, r)
		}
		if !policy.mayAccess(bname) { goto e_access }
		apiCalls.WithLabelValues("o", "ls").Inc()
		return handleListObjects(ctx, bname, iam, w, r)
	case http.MethodPut:
		if !policy.isRoot() { goto e_access }
		apiCalls.WithLabelValues("b", "put").Inc()
		return handlePutBucket(ctx, bname, iam, w, r)
	case http.MethodDelete:
		if !policy.isRoot() { goto e_access }
		apiCalls.WithLabelValues("b", "del").Inc()
		return handleDeleteBucket(ctx, bname, iam, w, r)
	case http.MethodHead:
		if !policy.mayAccess(bname) { goto e_access }
		apiCalls.WithLabelValues("b", "acc").Inc()
		return handleAccessBucket(bname, iam, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	return nil

e_access:
	return &S3Error{ ErrorCode: S3ErrAccessDenied }
}

func handleUploadFini(ctx context.Context, uploadId string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var complete swys3api.S3MpuFiniParts

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	err = xml.Unmarshal(body, &complete)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrMissingRequestBodyError }
	}

	resp, err := s3UploadFini(ctx, iam, bucket, uploadId, &complete)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadInit(ctx context.Context, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	upload, err := s3UploadInit(ctx, iam, bucket, oname, canned_acl)
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

func handleUploadListParts(ctx context.Context, uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	resp, err := s3UploadList(ctx, bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadPart(ctx context.Context, uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
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

	etag, err = s3UploadPart(ctx, iam, bucket, oname, uploadId, partno, body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}
	w.Header().Set("ETag", etag)

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleUploadAbort(ctx context.Context, uploadId, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3UploadAbort(ctx, bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleGetObject(ctx context.Context, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	body, err := s3ReadObject(ctx, bucket, oname, 0, 1)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body)
	return nil
}

func handleCopyObject(ctx context.Context, copy_source, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname_source, oname_source string
	var bucket_source *S3Bucket
	var object *S3Object
	var err error

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	if copy_source[0] == '/' { copy_source = copy_source[1:] }
	v := strings.SplitAfterN(copy_source, "/", 2)
	if len(v) < 2 {
		return &S3Error{
			ErrorCode:	S3ErrInvalidRequest,
			Message:	"Wrong source " + copy_source,
		}
	} else {
		bname_source = v[0][:(len(v[0]) - 1)]
		oname_source = v[1]
	}

	if !iam.Policy.mayAccess(bname_source) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}

	bucket_source, err = iam.FindBucket(ctx, bname_source)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	body, err := s3ReadObject(ctx, bucket_source, oname_source, 0, 1)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	object, err = s3AddObject(ctx, iam, bucket, oname, canned_acl, body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, &swys3api.CopyObjectResult{
		ETag:		object.ETag,
		LastModified:	object.CreationTime,
	})
	return nil
}

func handlePutObject(ctx context.Context, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	copy_source := r.Header.Get("X-Amz-Copy-Source")
	if copy_source != "" {
		return handleCopyObject(ctx, copy_source, oname, iam, bucket, w, r)
	}

	//object_size, err := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	//if err != nil {
	//	object_size = 0
	//}

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	_, err = s3AddObject(ctx, iam, bucket, oname, canned_acl, body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteObject(ctx context.Context, oname string, iam *S3Iam, bucket *S3Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	err := s3DeleteObject(ctx, iam, bucket, oname)
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

func handleObject(ctx context.Context, iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error {
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

	bucket, err = iam.FindBucket(ctx, bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	switch r.Method {
	case http.MethodPost:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "fin").Inc()
			return handleUploadFini(ctx, uploadId, iam, bucket, w, r)
		} else if _, ok := getURLParam(r, "uploads"); ok {
			apiCalls.WithLabelValues("u", "ini").Inc()
			return handleUploadInit(ctx, oname, iam, bucket, w, r)
		}
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	case http.MethodGet:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "lp").Inc()
			return handleUploadListParts(ctx, uploadId, oname, iam, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "get").Inc()
		return handleGetObject(ctx, oname, iam, bucket, w, r)
	case http.MethodPut:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "put").Inc()
			return handleUploadPart(ctx, uploadId, oname, iam, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "put").Inc()
		return handlePutObject(ctx, oname, iam, bucket, w, r)
	case http.MethodDelete:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "del").Inc()
			return handleUploadAbort(ctx, uploadId, oname, iam, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "del").Inc()
		return handleDeleteObject(ctx, oname, iam, bucket, w, r)
	case http.MethodHead:
		apiCalls.WithLabelValues("o", "acc").Inc()
		return handleAccessObject(bname, oname, iam, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	w.WriteHeader(http.StatusOK)
	return nil

e_access:
	return &S3Error{ ErrorCode: S3ErrAccessDenied }
}

func handleS3API(cb func(ctx context.Context, iam *S3Iam, akey *S3AccessKey, w http.ResponseWriter, r *http.Request) *S3Error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var akey *S3AccessKey
		var iam *S3Iam
		var err error

		defer r.Body.Close()

		ctx, done := mkContext("s3 req")
		defer done(ctx)

		logRequest(r)

		if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		// Admin is allowed to process without signing a request
		if s3VerifyAdmin(r) == nil {
			access_key := r.Header.Get(swys3api.SwyS3_AccessKey)
			akey, err = LookupAccessKey(ctx, access_key)
		} else {
			akey, err = s3VerifyAuthorization(ctx, r)
		}

		if err == nil {
			if !akey.Expired() {
				iam, err = akey.s3IamFind(ctx)
			} else {
				err = fmt.Errorf("The access key is expired")
			}
		}

		if akey == nil || iam == nil || err != nil {
			HTTPRespError(w, S3ErrAccessDenied, err.Error())
		} else if e := cb(ctx, iam, akey, w, r); e != nil {
			HTTPRespS3Error(w, e)
		}
	})
}

func handleKeygen(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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

	akey, err = genNewAccessKey(ctx, kg.Namespace, kg.Bucket, kg.Lifetime)
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

func handleKeydel(ctx context.Context, w http.ResponseWriter, r *http.Request) {
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

	err = dbRemoveAccessKey(ctx, kd.AccessKeyID)
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

	ctx, done := mkContext("adminreq")
	defer done(ctx)

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err = s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch op {
	case "keygen":
		handleKeygen(ctx, w, r)
		return
	case "keydel":
		handleKeydel(ctx, w, r)
		return
	}

	http.Error(w, fmt.Sprintf("Unknown operation"), http.StatusBadRequest)
}

func handleNotify(w http.ResponseWriter, r *http.Request, subscribe bool) {
	var params swys3api.S3Subscribe

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ctx, done := mkContext("notifyreq")
	defer done(ctx)

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
		err = s3Subscribe(ctx, &params)
	} else {
		err = s3Unsubscribe(ctx, &params)
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

	match_bucket := fmt.Sprintf("/{BucketName:%s*}",
		S3BucketName_Letter)
	match_object := fmt.Sprintf("/{BucketName:%s+}/{ObjName:%s+}",
		S3BucketName_Letter, S3ObjectName_Letter)

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

	ctx, done := mkContext("init")

	err = radosInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to rados: %s",
				err.Error())
	}

	err = notifyInit(&conf.Notify)
	if err != nil {
		log.Fatalf("Can't setup notifications: %s", err.Error())
	}

	err = dbRepair(ctx)
	if err != nil {
		log.Fatalf("Can't process db test/repair: %s", err.Error())
	}

	err = gcInit(0)
	if err != nil {
		log.Fatalf("Can't setup garbage collector: %s", err.Error())
	}

	err = PrometheusInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup prometheus: %s", err.Error())
	}

	done(ctx)

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

	err = swyhttp.ListenAndServe(
		&http.Server{
			Handler:      rgatesrv,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, S3ModeDevel || isLite(), func(s string) { log.Debugf(s) })
	if err != nil {
		log.Errorf("ListenAndServe: gatesrv %s", err.Error())
	}

	radosFini()
	dbDisconnect()
}
