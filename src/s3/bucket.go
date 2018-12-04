/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"strings"
	"regexp"
	"context"
	"sort"
	"time"

	"swifty/apis/s3"
	"swifty/s3/mgo"
)

var BucketCannedAcls = []string {
	swys3api.S3BucketAclCannedPrivate,
	swys3api.S3BucketAclCannedPublicRead,
	swys3api.S3BucketAclCannedPublicReadWrite,
	swys3api.S3BucketAclCannedAuthenticatedRead,
}

type S3ListObjectsRP struct {
	Delimiter		string
	MaxKeys			int64
	Prefix			string
	ContToken		string
	FetchOwner		bool
	StartAfter		string
	Marker			string

	// Private fields
	ContTokenDecoded	string
	V2			bool
}

type S3Website struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	State				uint32		`bson:"state"`
	BCookie				string		`bson:"bcookie,omitempty"`
	IdxDoc				string		`bson:"index-doc,omitempty"`
	ErrDoc				string		`bson:"error-doc,omitempty"`
}

func (ws *S3Website)index() string {
	s := ws.IdxDoc
	if s == "" {
		s = "index.html"
	}
	return s
}

func s3WebsiteLookup(ctx context.Context, b *s3mgo.Bucket) (*S3Website, error) {
	var res S3Website

	query := bson.M{ "bcookie": b.BCookie, "state": S3StateActive }
	err := dbS3FindOne(ctx, query, &res)
	return &res, err
}

func s3WebsiteInsert(ctx context.Context, b *s3mgo.Bucket, cfg *swys3api.S3WebsiteConfig) (*S3Website, error) {
	var ws S3Website
	var err error

	insert := bson.M{
		"_id":			bson.NewObjectId(),
		"bcookie":		b.BCookie,
		"state":		S3StateActive,
		"index-doc":		cfg.IndexDoc.Suff,
		"error-doc":		cfg.ErrDoc.Key,
	}

	query := bson.M{ "bcookie": b.BCookie, "state": S3StateActive }
	update := bson.M{ "$setOnInsert": insert }

	log.Debugf("s3: Upserting website for %s", b.Name)
	if err = dbS3Upsert(ctx, query, update, &ws); err != nil {
		return nil, err
	}

	return &ws, nil
}

func s3DirtifyBucket(ctx context.Context, id bson.ObjectId) error {
	query := bson.M{ "_id": id, "ref": bson.M{ "$eq":  0 } }
	update := bson.M{ "$set": bson.M{ "ref": 1 } }

	return dbS3Update(ctx, query, update, false, &s3mgo.Bucket{})
}

func RemoveFromDB(ctx context.Context, bucket *s3mgo.Bucket) (error) {
	query := bson.M{ "cnt-objects": 0 }
	return dbS3RemoveOnState(ctx, bucket, S3StateInactive, query)
}

func FindBucket(ctx context.Context, bname string) (*s3mgo.Bucket, error) {
	var res s3mgo.Bucket
	var err error

	account, err := s3AccountLookup(ctx)
	if err != nil { return nil, err }

	query := bson.M{ "bcookie": account.BCookie(bname), "state": S3StateActive }
	err = dbS3FindOne(ctx, query, &res)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find bucket %s/%s: %s",
				infoLong(account), infoLong(ctxIam(ctx)), err.Error())
		}
		return nil, err
	}

	return &res, nil
}

func s3RepairBucketReference(ctx context.Context, bucket *s3mgo.Bucket) error {
	var cnt_objects int64 = 0
	var cnt_bytes int64 = 0
	var objects []s3mgo.Object

	query := bson.M{ "bucket-id": bucket.ObjID, "state": S3StateActive }
	err := dbS3FindAll(ctx, query, &objects)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find objects on bucket %s: %s",
				infoLong(bucket), err.Error())
			return err
		}

	} else {
		cnt_objects = int64(len(objects))
		for _, object := range objects {
			cnt_bytes += object.Size
		}
	}

	update := bson.M{ "$set": bson.M{ "cnt-objects": cnt_objects,
			"cnt-bytes": cnt_bytes, "ref": 0} }
	err = dbS3Update(ctx, nil, update, false, bucket)
	if err != nil {
		log.Errorf("s3: Can't repair bucket %s: %s",
			infoLong(bucket), err.Error())
		return err
	}

	log.Debugf("s3: Repaired reference on %s", infoLong(bucket))
	return nil
}

func s3RepairBucketReferenced(ctx context.Context) error {
	var buckets []s3mgo.Bucket
	var err error

	log.Debugf("s3: Processing referenced buckets")

	err = dbS3FindAll(ctx, bson.M{ "ref":  bson.M{ "$ne": 0 } }, &buckets)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairReferenced failed: %s", err.Error())
		return err
	}

	for _, bucket := range buckets {
		log.Debugf("s3: Detected referenced bucket %s", infoLong(&bucket))
		err = s3RepairBucketReference(ctx, &bucket)
		if err != nil {
			return err
		}
	}

	return nil
}

func s3RepairBucketInactive(ctx context.Context) error {
	var buckets []s3mgo.Bucket
	var err error

	log.Debugf("s3: Processing inactive buckets")

	err = dbS3FindAllInactive(ctx, &buckets)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairBucket failed: %s", err.Error())
		return err
	}

	for _, bucket := range buckets {
		log.Debugf("s3: Detected stale bucket %s", infoLong(&bucket))

		err = radosDeletePool(bucket.BCookie)
		if err != nil {
			log.Errorf("s3: %s backend bucket may stale", bucket.BCookie)
		}

		err = dbS3Remove(ctx, &bucket)
		if err != nil {
			log.Debugf("s3: Can't remove bucket %s", infoLong(&bucket))
			return err
		}

		log.Debugf("s3: Removed stale bucket %s", infoLong(&bucket))
	}

	return nil
}

func s3RepairBucket(ctx context.Context) error {
	var err error

	log.Debugf("s3: Running buckets consistency test")

	if err = s3RepairBucketInactive(ctx); err != nil {
		return err
	}

	if err = s3RepairBucketReferenced(ctx); err != nil {
		return err
	}

	log.Debugf("s3: Buckets consistency passed")
	return nil
}

func s3InsertBucket(ctx context.Context, bname, canned_acl string) (*S3Error) {
	var err error

	account, err := s3AccountLookup(ctx)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	bucket := &s3mgo.Bucket{
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		Name:		bname,
		CannedAcl:	canned_acl,
		BCookie:	account.BCookie(bname),
		NamespaceID:	account.NamespaceID(),
		CreationTime:	time.Now().Format(time.RFC3339),
		MaxObjects:	S3StorageMaxObjects,
		MaxBytes:	S3StorageMaxBytes,
	}

	if err = dbS3Insert(ctx, bucket); err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = radosCreatePool(bucket.BCookie, uint64(bucket.MaxObjects), uint64(bucket.MaxBytes))
	if err != nil {
		goto out_nopool
	}

	if err = dbS3SetState(ctx, bucket, S3StateActive, nil); err != nil {
		goto out
	}

	log.Debugf("s3: Inserted %s", infoLong(bucket))
	return nil

out:
	radosDeletePool(bucket.BCookie)
out_nopool:
	RemoveFromDB(ctx, bucket)
	return &S3Error{ ErrorCode: S3ErrInternalError }
}

func s3DeleteBucket(ctx context.Context, bname, acl string) (*S3Error) {
	var bucket *s3mgo.Bucket
	var err error

	bucket, err = FindBucket(ctx, bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = dbS3SetState(ctx, bucket, S3StateInactive, bson.M{"cnt-objects": 0})
	if err != nil {
		if err == mgo.ErrNotFound {
			if bucket.CntObjects > 0 {
				return &S3Error{ ErrorCode: S3ErrBucketNotEmpty }
			}
		}
		log.Errorf("s3: Can't delete %s: %s", infoLong(bucket), err.Error())
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = radosDeletePool(bucket.BCookie)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = RemoveFromDB(ctx, bucket)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	log.Debugf("s3: Deleted %s", infoLong(bucket))
	return nil
}

func FindAllObjects(ctx context.Context, bucket *s3mgo.Bucket, query bson.M) ([]s3mgo.Object, error) {
	if query == nil { query = make(bson.M) }
	var res []s3mgo.Object

	query["bucket-id"] = bucket.ObjID
	query["state"] = S3StateActive

	err := dbS3FindAll(ctx, query, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3GetBucketMetricOutput(ctx context.Context, bname, metric_name string) (*swys3api.S3GetMetricStatisticsOutput, *S3Error) {
	var res swys3api.S3GetMetricStatisticsOutput
	var point swys3api.S3Datapoint

	bucket, err := FindBucket(ctx, bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return nil, &S3Error{ ErrorCode: S3ErrInternalError}
	}

	if metric_name == "BucketSizeBytes" {
		point.Timestamp	= bucket.CreationTime
		point.Average	= float64(bucket.CntBytes)
		point.Unit	= "Bytes"
	} else if metric_name == "NumberOfObjects" {
		point.Timestamp	= bucket.CreationTime
		point.Average	= float64(bucket.CntObjects)
		point.Unit	= "Count"
	} else {
		return nil, &S3Error{
			ErrorCode: S3ErrIncompleteBody,
			Message: "Wrong metric name",
		}
	}

	point.SampleCount = 1

	res.Result.Datapoints.Points = append(res.Result.Datapoints.Points, point)
	res.Result.Label = metric_name
	return &res, nil
}

func (params *S3ListObjectsRP) Validate() (bool) {
	re := regexp.MustCompile(S3ObjectName_Letter)

	if params.Delimiter != "" { if !re.MatchString(params.Delimiter) { return false } }
	if params.Prefix != "" { if !re.MatchString(params.Prefix) { return false } }
	if params.StartAfter != "" { if !re.MatchString(params.StartAfter) { return false } }

	if params.ContToken != "" {
		token := base64_decode(params.ContToken)
		if token == nil { return false }
		if !re.MatchString(string(token[:])) { return false }

		params.ContTokenDecoded = string(token[:])
	}

	if params.Marker != "" {
		params.ContTokenDecoded = params.Marker
	}

	if len(params.Delimiter) > 1 { return false }

	if params.MaxKeys <= 0 {
		params.MaxKeys = S3StorageDefaultListObjects
	} else if params.MaxKeys > S3StorageMaxObjects {
		return false
	}

	return true
}

func s3ListBucket(ctx context.Context, bname string, params *S3ListObjectsRP) (*swys3api.S3Bucket, *S3Error) {
	var start_after, cont_after bool
	var prefixes_map map[string]bool
	var list swys3api.S3Bucket
	var bucket *s3mgo.Bucket
	var object s3mgo.Object
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var err error
	var pkey string

	if params.Validate() == false {
		return nil, &S3Error{ ErrorCode: S3ErrInvalidArgument }
	}

	bucket, err = FindBucket(ctx, bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return nil, &S3Error{ ErrorCode: S3ErrInternalError }
	}

	if params.StartAfter != "" { start_after = true }
	if params.ContTokenDecoded != "" {
		start_after = false
		cont_after = true
	}

	list.Name	= bucket.Name
	list.KeyCount	= 0
	list.MaxKeys	= params.MaxKeys
	list.IsTruncated= false

	query := bson.M{ "bucket-id": bucket.ObjID, "state": S3StateActive}

	if params.Prefix != "" {
		query["key"] = bson.M{ "$regex": "^" + params.Prefix, }
	}

	prefixes_map = make(map[string]bool)

	pipe = dbS3Pipe(ctx, &object, []bson.M{{"$match": query}, {"$sort": bson.M{"key": 1, "rover": -1}}})
	iter = pipe.Iter()
	for iter.Next(&object) {
		if object.Key == pkey {
			continue
		}

		pkey = object.Key

		if start_after {
			if object.Key != params.StartAfter { continue }
			start_after = false
			continue
		}
		if cont_after {
			if object.Key != params.ContTokenDecoded { continue }
			cont_after = false
			continue
		}
		if params.Delimiter != "" {
			len_pfx := len(params.Prefix)
			len_dlm := len(params.Delimiter)
			len_key := len(object.Key)
			pos := strings.Index(object.Key[len_pfx:], params.Delimiter)
			if pos >= 0 && (pos + len_pfx + len_dlm) <= len_key {
				if pos == 0 { continue }
				prefix := object.Key[:len_pfx+pos+len_dlm]
				if _, ok := prefixes_map[prefix]; !ok {
					prefixes_map[prefix] = true
				}
				continue
			}
		}
		o := swys3api.S3Object {
			Key:		object.Key,
			Size:		object.Size,
			LastModified:	object.CreationTime,
			ETag:		object.ETag,
			StorageClass:	swys3api.S3StorageClassStandard,
		}

		if params.FetchOwner {
			iam := ctxIam(ctx)
			o.Owner.DisplayName = iam.User
			o.Owner.ID = iam.AwsID
		}

		list.Contents = append(list.Contents, o)
		list.KeyCount++

		if list.KeyCount >= list.MaxKeys {
			list.IsTruncated = true
			if params.V2 {
				list.ContinuationToken = params.ContToken
				list.NextContinuationToken = base64_encode([]byte(object.Key))
			} else {
				list.Marker = params.Marker
				list.NextMarker = object.Key
			}
			break
		}
	}
	iter.Close()

	keys := []string{ }
	for k, _ := range prefixes_map {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		list.CommonPrefixes = append(list.CommonPrefixes,
			swys3api.S3Prefix {
				Prefix: k,
			})
	}

	return &list, nil
}

func s3ListBuckets(ctx context.Context) (*swys3api.S3BucketList, *S3Error) {
	var list swys3api.S3BucketList
	var buckets []s3mgo.Bucket
	var err error

	buckets, err = FindBuckets(ctx)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}

		log.Errorf("s3: Can't find buckets %s: %s", infoLong(ctxIam(ctx)), err.Error())
		return nil, &S3Error{ ErrorCode: S3ErrInternalError }
	}

	iam := ctxIam(ctx)
	list.Owner.DisplayName	= iam.User
	list.Owner.ID		= iam.AwsID[:16]

	for _, b := range buckets {
		list.Buckets.Bucket = append(list.Buckets.Bucket,
			swys3api.S3BucketListEntry{
				Name:		b.Name,
				CreationDate:	b.CreationTime,
			})
	}

	return &list, nil
}
