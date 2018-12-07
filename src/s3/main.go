/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

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
	"bytes"
	"strings"
	"strconv"
	"errors"
	"time"
	"flag"
	"fmt"
	"os"

	"swifty/s3/mgo"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/secrets"
	"swifty/common/xrest/sysctl"
	"swifty/apis/s3"
)

var s3Secrets xsecret.Store
var s3SecKey []byte
var S3ModeDevel bool

func isLite() bool { return Flavor == "lite" }

type YAMLConfCeph struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	WebPort		string			`yaml:"webport"`
	Token		string			`yaml:"token"`
	LogLevel	string			`yaml:"loglevel"`
	Prometheus	string			`yaml:"prometheus"`
	HTTPS		*xhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
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
	Mimes		string			`yaml:"mime-types"`
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
	id	string
	S	*mgo.Session
	errCode	int
	mime	string
	iam	*s3mgo.Iam
}

func Dbs(ctx context.Context) *mgo.Session {
	return ctx.(*s3Context).S
}

func ctxAuthorize(ctx context.Context, iam *s3mgo.Iam) {
	ctx.(*s3Context).iam = iam
}

func ctxIam(ctx context.Context) *s3mgo.Iam {
	return ctx.(*s3Context).iam
}

func ctxAllowed(ctx context.Context, action int) bool {
	return ctxIam(ctx).Policy.Allowed(action)
}

func ctxMayAccess(ctx context.Context, bname string) bool {
	return ctxIam(ctx).Policy.MayAccess(bname)
}

func mkContext(id string) (context.Context, func(context.Context)) {
	ctx := &s3Context{
		context.Background(), id,
		session.Copy(),
		0, "", nil,
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

var logReqDetails int = 1

func logRequest(r *http.Request) {
	var request []string

	if logReqDetails == 0 {
		return
	}

	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, "\n---")
	request = append(request, url)
	request = append(request, fmt.Sprintf("Host: %v", r.Host))

	if logReqDetails >= 2 {
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
	}

	request = append(request, "---")

	log.Debug(strings.Join(request, "\n"))
}

func handleBucketCloudWatch(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error {
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

	res, e := s3GetBucketMetricOutput(ctx, bname, urlValue(request_map, "MetricName"))
	if e != nil { return e }

	HTTPRespXML(w, res)
	return nil
}

// List all buckets belonging to an account
func handleListBuckets(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_ListAllMyBuckets) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	buckets, err := s3ListBuckets(ctx)
	if err != nil { return err }

	HTTPRespXML(w, buckets)
	return nil
}

func handleListUploads(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_ListBucketMultipartUploads) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	uploads, err := s3Uploads(ctx, bname)
	if err != nil { return err }

	HTTPRespXML(w, uploads)
	return nil
}

func handleListObjects(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_ListBucket) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	var params *S3ListObjectsRP
	listType := getURLValue(r, "list-type")

	switch listType {
	case "2":
		params = &S3ListObjectsRP {
			V2:		true,
			ContToken:	getURLValue(r, "continuation-token"),
			StartAfter:	getURLValue(r, "start-after"),
			FetchOwner:	getURLBool(r, "fetch-owner"),
		}
	case "":
		params = &S3ListObjectsRP {
			Marker:		getURLValue(r, "marker"),
		}
	default:
		return &S3Error{
			ErrorCode: S3ErrInvalidArgument,
			Message: "Invalid list-type",
		}
	}

	params.Prefix = getURLValue(r, "prefix")
	params.Delimiter = getURLValue(r, "delimiter")

	if v, ok := getURLParam(r, "max-keys"); ok {
		params.MaxKeys, _ = strconv.ParseInt(v, 10, 64)
	}

	objects, err := s3ListBucket(ctx, bname, params)
	if err != nil { return err }

	HTTPRespXML(w, objects)
	return nil
}

func handlePutBucket(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_CreateBucket) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	if err := s3InsertBucket(ctx, bname, canned_acl); err != nil {
		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteBucket(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_DeleteBucket) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	err := s3DeleteBucket(ctx, bname, "")
	if err != nil { return err }

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleAccessBucket(ctx context.Context, bname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxMayAccess(ctx, bname) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}
	if !ctxAllowed(ctx, S3P_ListBucket) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	err := s3CheckAccess(ctx, bname, "")
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleBucket(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname string = mux.Vars(r)["BucketName"]

	if bname == "" {
		if r.Method == http.MethodPost {
			//
			// A special case where we
			// hande some subset of cloudwatch
			return handleBucketCloudWatch(ctx, w, r)
		} else if r.Method != http.MethodGet {
			return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
		}
	}

	switch r.Method {
	case http.MethodGet:
		if bname == "" {
			apiCalls.WithLabelValues("b", "ls").Inc()
			return handleListBuckets(ctx, w, r)
		}
		if _, ok := getURLParam(r, "uploads"); ok {
			apiCalls.WithLabelValues("u", "ls").Inc()
			return handleListUploads(ctx, bname, w, r)
		}
		if _, ok := getURLParam(r, "website"); ok {
			return handleGetWebsite(ctx, bname, w, r)
		}
		apiCalls.WithLabelValues("o", "ls").Inc()
		return handleListObjects(ctx, bname, w, r)
	case http.MethodPut:
		if _, ok := getURLParam(r, "website"); ok {
			return handlePutWebsite(ctx, bname, w, r)
		}
		apiCalls.WithLabelValues("b", "put").Inc()
		return handlePutBucket(ctx, bname, w, r)
	case http.MethodDelete:
		if _, ok := getURLParam(r, "website"); ok {
			return handleDelWebsite(ctx, bname, w, r)
		}
		apiCalls.WithLabelValues("b", "del").Inc()
		return handleDeleteBucket(ctx, bname, w, r)
	case http.MethodHead:
		apiCalls.WithLabelValues("b", "acc").Inc()
		return handleAccessBucket(ctx, bname, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	return nil
}

func handleUploadFini(ctx context.Context, uploadId string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var complete swys3api.S3MpuFiniParts

	if !ctxAllowed(ctx, S3P_PutObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrIncompleteBody }
	}

	err = xml.Unmarshal(body, &complete)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrMissingRequestBodyError }
	}

	resp, err := s3UploadFini(ctx, bucket, uploadId, &complete)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchKey }
		} else {
			return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
		}
	}

	HTTPRespXML(w, resp)
	return nil
}

func handleUploadInit(ctx context.Context, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_PutObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}


	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	upload, err := s3UploadInit(ctx, bucket, oname, canned_acl)
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

func handleUploadListParts(ctx context.Context, uploadId, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_ListMultipartUploadParts) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	resp, err := s3UploadList(ctx, bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, resp)
	return nil
}

func getBodySize(r *http.Request) int64 {
	cl := r.Header.Get("Content-Length")
	if cl == "" {
		return 0
	}

	sz, err := strconv.ParseUint(cl, 10, 64)
	if err != nil {
		return 0
	}

	return int64(sz)
}

func handleUploadPart(ctx context.Context, uploadId, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_PutObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	var partno int

	if part, ok := getURLParam(r, "partNumber"); ok {
		partno, _ = strconv.Atoi(part)
	} else {
		return &S3Error{ ErrorCode: S3ErrInvalidArgument }
	}

	sz := getBodySize(r)
	if sz == 0 {
		return &S3Error{ ErrorCode: S3ErrMissingContentLength, Message: "content-length header missing" }
	}

	etag, err := s3UploadPart(ctx, bucket, oname, uploadId, partno, &ioChunkReader{sz: sz, r: r.Body})
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}
	w.Header().Set("ETag", etag)

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleUploadAbort(ctx context.Context, uploadId, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_AbortMultipartUpload) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	err := s3UploadAbort(ctx, bucket, oname, uploadId)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleGetObject(ctx context.Context, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_GetObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	object, err := FindCurObject(ctx, bucket, oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchKey }
		}

		downloadErrors.WithLabelValues("db_obj").Inc()
		log.Errorf("s3: Can't find object %s on %s: %s", oname, infoLong(bucket), err.Error())
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	err = acctDownload(ctx, bucket.NamespaceID, object.Size)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrOperationAborted, Message: "Downloads are limited" }
	}

	if m := ctx.(*s3Context).mime; m != "" {
		w.Header().Set("Content-Type", m)
	}

	w.Header().Set("ETag", object.ETag)
	w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))

	if c := ctx.(*s3Context).errCode; c == 0 {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(c)
	}

	var downloaded int64

	err = s3ObjectPartsIter(ctx, object.ObjID, func(p *s3mgo.ObjectPart) error {
		return s3IterChunks(ctx, p, func(ch *s3mgo.DataChunk) error {
			w.Write(ch.Bytes)
			downloaded += int64(len(ch.Bytes))
			return nil
		})
	})

	if err != nil {
		/*
		 * Too late for download abort. Hope, that caller checks
		 * the ETag value against the received data.
		 */
		downloadErrors.WithLabelValues("db_parts").Inc()
		log.Errorf("s3: Can't complete object %s download: %s", infoLong(object), err.Error())
	}

	if downloaded != object.Size {
		downloadErrors.WithLabelValues("miscount").Inc()
		log.Errorf("s3: Object size != sum of its parts (%s), call fsck", infoLong(object))
		requestFsck()
	}

	return nil
}

func handleCopyObject(ctx context.Context, copy_source, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	var bname_source, oname_source string
	var bucket_source *s3mgo.Bucket
	var object *s3mgo.Object
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

	if !ctxMayAccess(ctx, bname_source) {
		return &S3Error{ ErrorCode: S3ErrAccessDenied }
	}

	bucket_source, err = FindBucket(ctx, bname_source)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	body, err := ReadObject(ctx, bucket_source, oname_source, 0, 1)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	object, err = AddObject(ctx, bucket, oname, canned_acl, &ioChunkReader{sz: int64(len(body)), r: bytes.NewReader(body)})
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	HTTPRespXML(w, &swys3api.CopyObjectResult{
		ETag:		object.ETag,
		LastModified:	object.CreationTime,
	})
	return nil
}

func handlePutObject(ctx context.Context, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_PutObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	copy_source := r.Header.Get("X-Amz-Copy-Source")
	if copy_source != "" {
		return handleCopyObject(ctx, copy_source, oname, bucket, w, r)
	}

	//object_size, err := strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
	//if err != nil {
	//	object_size = 0
	//}

	canned_acl := r.Header.Get("x-amz-acl")
	if verifyAclValue(canned_acl, BucketCannedAcls) == false {
		canned_acl = swys3api.S3BucketAclCannedPrivate
	}

	sz := getBodySize(r)
	if sz == 0 {
		return &S3Error{ ErrorCode: S3ErrMissingContentLength, Message: "content-length header missing" }
	}

	cr := &ioChunkReader{sz: sz, r: r.Body}

	o, err := AddObject(ctx, bucket, oname, canned_acl, cr)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	if cr.read != sz {
		s3DeleteObject(ctx, bucket, oname)
		return &S3Error{ ErrorCode: S3ErrIncompleteBody, Message: "trimmed body" }
	}

	w.Header().Set("ETag", o.ETag)
	w.WriteHeader(http.StatusOK)
	return nil
}

func handleDeleteObject(ctx context.Context, oname string, bucket *s3mgo.Bucket, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_DeleteObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	err := s3DeleteObject(ctx, bucket, oname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleAccessObject(ctx context.Context, bname, oname string, w http.ResponseWriter, r *http.Request) *S3Error {
	if !ctxAllowed(ctx, S3P_GetObject) {
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	err := s3CheckAccess(ctx, bname, oname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidRequest, Message: err.Error() }
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleObjectReq(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error {
	var oname string = mux.Vars(r)["ObjName"]
	if oname == "" {
		return handleBucket(ctx, w, r)
	}

	var bname string = mux.Vars(r)["BucketName"]
	return handleObject(ctx, w, r, bname, oname)
}

func handleObject(ctx context.Context, w http.ResponseWriter, r *http.Request, bname, oname string) *S3Error {
	var bucket *s3mgo.Bucket
	var err error

	if bname == "" {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	} else if oname == "" {
		return &S3Error{ ErrorCode: S3ErrSwyInvalidObjectName }
	}

	if !ctxMayAccess(ctx, bname) {
		goto e_access
	}

	bucket, err = FindBucket(ctx, bname)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInvalidBucketName }
	}

	switch r.Method {
	case http.MethodPost:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "fin").Inc()
			return handleUploadFini(ctx, uploadId, bucket, w, r)
		} else if _, ok := getURLParam(r, "uploads"); ok {
			apiCalls.WithLabelValues("u", "ini").Inc()
			return handleUploadInit(ctx, oname, bucket, w, r)
		}
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	case http.MethodGet:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "lp").Inc()
			return handleUploadListParts(ctx, uploadId, oname, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "get").Inc()
		return handleGetObject(ctx, oname, bucket, w, r)
	case http.MethodPut:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "put").Inc()
			return handleUploadPart(ctx, uploadId, oname, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "put").Inc()
		return handlePutObject(ctx, oname, bucket, w, r)
	case http.MethodDelete:
		if uploadId, ok := getURLParam(r, "uploadId"); ok {
			apiCalls.WithLabelValues("u", "del").Inc()
			return handleUploadAbort(ctx, uploadId, oname, bucket, w, r)
		}
		apiCalls.WithLabelValues("o", "del").Inc()
		return handleDeleteObject(ctx, oname, bucket, w, r)
	case http.MethodHead:
		apiCalls.WithLabelValues("o", "acc").Inc()
		return handleAccessObject(ctx, bname, oname, w, r)
	default:
		return &S3Error{ ErrorCode: S3ErrMethodNotAllowed }
	}

	w.WriteHeader(http.StatusOK)
	return nil

e_access:
	return &S3Error{ ErrorCode: S3ErrAccessDenied }
}

func s3AuthorizeGetKey(ctx context.Context, r *http.Request) (*s3mgo.AccessKey, int, error) {
	akey, code, err := s3AuthorizeUser(ctx, r)
	if akey != nil || err != nil {
		return akey, code, err
	}

	akey, err = s3AuthorizeAdmin(ctx, r)
	if akey != nil || err != nil {
		return akey, S3ErrAccessDenied, err
	}

	return nil, S3ErrAccessDenied, errors.New("Not authorized")
}

func s3Authorize(ctx context.Context, r *http.Request) (int, error) {
	key, code, err := s3AuthorizeGetKey(ctx, r)
	if err != nil {
		return code, err
	}

	if key.Expired() {
		return S3ErrAccessDenied, errors.New("Key is expired")
	}

	iam, err := s3IamFind(ctx, key)
	if err == nil {
		log.Debugf("Authorized user, key %s", key.AccessKeyID)
	}

	ctxAuthorize(ctx, iam)
	return 0, nil
}

func handleS3API(cb func(ctx context.Context, w http.ResponseWriter, r *http.Request) *S3Error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		ctx, done := mkContext("api")
		defer done(ctx)

		logRequest(r)

		if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		code, err := s3Authorize(ctx, r)
		if err != nil {
			HTTPRespError(w, code, err.Error())
			return
		}

		if e := cb(ctx, w, r); e != nil {
			HTTPRespS3Error(w, e)
		}
	})
}

func handleKeygen(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var akey *s3mgo.AccessKey
	var kg swys3api.KeyGen
	var err error

	err = xhttp.RReq(r, &kg)
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

	err = xhttp.Respond(w, &swys3api.KeyGenResult{
			AccessKeyID:	akey.AccessKeyID,
			AccessKeySecret:s3DecryptAccessKeySecret(akey),
			AccID:		akey.AccountObjID.Hex(),
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
	var kd swys3api.KeyDel
	var err error

	err = xhttp.RReq(r, &kd)
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

func handleStats(w http.ResponseWriter, r *http.Request) {
	var st *s3mgo.AcctStats

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	ctx, done := mkContext("statsreq")
	defer done(ctx)
	ns := mux.Vars(r)["ns"]
	log.Debugf("Getting stats for %s", ns)

	act, err := s3AccountFind(ctx, ns)
	if err != nil {
		http.Error(w, "No such namespace", http.StatusNotFound)
		return
	}

	st, err = StatsFindFor(ctx, act)
	if err != nil {
		http.Error(w, "Error getting stats", http.StatusInternalServerError)
		return
	}

	err = xhttp.Respond(w, &swys3api.AcctStats{
		CntObjects:	st.CntObjects,
		CntBytes:	st.CntBytes,
		OutBytes:	st.OutBytes,
		OutBytesWeb:	st.OutBytesWeb,
	})
	if err != nil {
		http.Error(w, "Bad response", http.StatusNoContent)
	}
}

func handleLimits(w http.ResponseWriter, r *http.Request) {
	var lim swys3api.AcctLimits

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = xhttp.RReq(r, &lim)
	if err != nil {
		http.Error(w, "Cannot read limits", http.StatusBadRequest)
		return
	}

	ctx, done := mkContext("statsreq")
	defer done(ctx)
	ns := mux.Vars(r)["ns"]
	log.Debugf("Setting limits for %s", ns)

	act, err := s3AccountFind(ctx, ns)
	if err != nil {
		http.Error(w, "No such namespace", http.StatusNotFound)
		return
	}

	err = LimitsSetFor(ctx, act, &lim)
	if err != nil {
		log.Errorf("Error setting limits: %s", err.Error())
		http.Error(w, "Error setting limits", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleNotify(w http.ResponseWriter, r *http.Request) {
	var params swys3api.Subscribe

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ctx, done := mkContext("notifyreq")
	defer done(ctx)

	/* For now make it admin-only op */
	err := s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	switch r.Method {
	case "POST":
		err = s3Subscribe(ctx, &params)
	case "DELETE":
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

func handleKeys(w http.ResponseWriter, r *http.Request) {
	var err error

	ctx, done := mkContext("keysreq")
	defer done(ctx)

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err = s3VerifyAdmin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "POST":
		handleKeygen(ctx, w, r)
	case "DELETE":
		handleKeydel(ctx, w, r)
	}
}

func makeAdminURL(clienturl, admport string) string {
	return strings.Split(clienturl, ":")[0] + ":" + admport
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
		xh.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	s3Secrets, err = xsecret.Init("s3")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	conf.SecKey, err = s3Secrets.Get(conf.SecKey)
	if err != nil {
		log.Error("Cannot find seckey secret: %s", err.Error())
		return
	}

	s3SecKey, err = hex.DecodeString(conf.SecKey)
	if err != nil || len(s3SecKey) < 16 {
		log.Error("Secret key should be decodable and be 16 bytes long at least")
		return
	}

	adminAccToken, err = s3Secrets.Get(conf.Daemon.Token)
	if err != nil || len(adminAccToken) < 16 {
		log.Debugf(">> %s vs %s", conf.Daemon.Token, adminAccToken)
		log.Errorf("Bad admin access token: %s", err)
		return
	}

	err = webReadMimes(conf.Mimes)
	if err != nil {
		log.Error("Cannot read mime-types: %s", err.Error())
		return
	}

	sysctl.AddIntSysctl("s3_req_verb", &logReqDetails)
	sysctl.AddRoSysctl("s3_version", func() string { return Version })
	sysctl.AddRoSysctl("s3_mode", func() string {
		ret := "mode:"
		if S3ModeDevel {
			ret += "devel"
		} else {
			ret += "prod"
		}

		ret += ", flavor:" + Flavor
		ret += ", rados:"
		if radosDisabled {
			ret += "no"
		} else {
			ret += "yes"
		}

		return ret
	})


	// Service operations
	rgatesrv := mux.NewRouter()
	match_bucket := fmt.Sprintf("/{BucketName:%s*}",
		S3BucketName_Letter)
	match_object := fmt.Sprintf("/{BucketName:%s+}/{ObjName:%s*}",
		S3BucketName_Letter, S3ObjectName_Letter)

	rgatesrv.Handle(match_bucket,	handleS3API(handleBucket))
	rgatesrv.Handle(match_object,	handleS3API(handleObjectReq))

	// Web server operations
	rwebsrv := mux.NewRouter()
	rwebsrv.Methods("GET", "HEAD").HandlerFunc(handleWebReq)
	if conf.Daemon.WebPort == "" {
		conf.Daemon.WebPort = "8080"
	}

	// Admin operations
	radminsrv := mux.NewRouter()
	radminsrv.HandleFunc("/v1/api/keys", handleKeys).Methods("POST", "DELETE")
	radminsrv.HandleFunc("/v1/api/notify", handleNotify).Methods("POST", "DELETE")
	radminsrv.HandleFunc("/v1/api/stats/{ns}", handleStats).Methods("GET")
	radminsrv.HandleFunc("/v1/api/stats/{ns}/limits", handleLimits).Methods("PUT")
	radminsrv.HandleFunc("/v1/sysctl", handleSysctls).Methods("GET", "OPTIONS")
	radminsrv.HandleFunc("/v1/sysctl/{name}", handleSysctl).Methods("GET", "PUT", "OPTIONS")

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
			Addr:         makeAdminURL(conf.Daemon.Addr, conf.Daemon.AdminPort),
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}

		err = adminsrv.ListenAndServe()
		if err != nil {
			log.Errorf("ListenAndServe: adminsrv %s", err.Error())
		}
	}()

	go func() {
		webRoot = strings.SplitN(conf.Daemon.Addr, ":", 2)[0]
		websrv := &http.Server{
			Handler:	rwebsrv,
			Addr:		webRoot + ":" + conf.Daemon.WebPort,
		}

		err = websrv.ListenAndServe()
		if err != nil {
			log.Errorf("ListenAndServe: websrv %s", err.Error())
		}
	}()

	err = xhttp.ListenAndServe(
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
