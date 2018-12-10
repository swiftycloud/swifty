/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2"
	"github.com/gorilla/mux"

	"io/ioutil"
	"encoding/xml"
	"net/http"
	"net/url"
	"context"
	"strings"
	"strconv"
	"math"

	"swifty/s3/mgo"
	"swifty/apis/s3"
)

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

	etag, err := s3UploadPart(ctx, bucket, oname, uploadId, partno, &ChunkReader{size: sz, r: r.Body})
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

	var from, to int64
	to = math.MaxInt64

	rng := r.Header.Get("Range")
	if rng != "" {
		var err error
		from, to, err = parseRange(rng)
		if err != nil {
			return &S3Error{ ErrorCode: S3ErrInvalidRange, Message: err.Error() }
		}
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

	if from > object.Size {
		return &S3Error{ ErrorCode: S3ErrInvalidRange, Message: "Object is too smal" }
	}

	if to > object.Size {
		to = object.Size
	}
	ds := to - from
	if ds > object.Size {
		ds = object.Size
	}

	err = acctDownload(ctx, bucket.NamespaceID, ds)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrOperationAborted, Message: "Downloads are limited" }
	}

	if m := ctx.(*s3Context).mime; m != "" {
		w.Header().Set("Content-Type", m)
	}

	w.Header().Set("ETag", object.ETag)
	w.Header().Set("Content-Length", strconv.FormatInt(ds, 10))

	if c := ctx.(*s3Context).errCode; c == 0 {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(c)
	}

	var downloaded int64
	var rover int64

	err = IterParts(ctx, object.ObjID, func(p *s3mgo.ObjectPart) error {
		re := rover + p.Size
		if re  < from {
			rover = re
			return nil
		}

		if rover >= to {
			rover = re
			return nil /* FIXME -- report "STOP" marker */
		}

		return IterChunks(ctx, p, func(ch *s3mgo.DataChunk) error {
			re := rover + int64(len(ch.Bytes))
			if re  < from {
				rover = re
				return nil
			}
			if rover >= to {
				rover = re
				return nil /* FIXME -- repot "STOP" marker */
			}

			s_off := 0
			if from > rover {
				s_off = int(from - rover)
			}
			e_off := len(ch.Bytes)
			if to <= re {
				e_off = int(to - rover)
			}

			w.Write(ch.Bytes[s_off:e_off])
			downloaded += int64(e_off - s_off)
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

	if downloaded != ds {
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

	object, err = CopyObject(ctx, bucket, oname, canned_acl, bucket_source, oname_source)
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

	cr := &ChunkReader{size: sz, r: r.Body}

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

