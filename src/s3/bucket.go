package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"strings"
	"regexp"
	"sort"
	"time"

	"../apis/apps/s3"
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

	// Private fields
	ContTokenDecoded	string
}

type S3BucketNotify struct {
	Events				uint64		`bson:"events"`
	Queue				string		`bson:"queue"`
}

type S3Tag struct {
	Key				string		`bson:"key"`
	Value				string		`bson:"value,omitempty"`
}

type S3BucketEncrypt struct {
	Algo				string		`bson:"algo"`
	MasterKeyID			string		`bson:"algo,omitempty"`
}

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	BackendID			string		`bson:"bid,omitempty"`
	NamespaceID			string		`bson:"nsid,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`

	// Todo
	Versioning			bool		`bson:"versioning,omitempty"`
	TagSet				[]S3Tag		`bson:"tags,omitempty"`
	Encrypt				S3BucketEncrypt	`bson:"encrypt,omitempty"`
	Location			string		`bson:"location,omitempty"`
	Policy				string		`bson:"policy,omitempty"`
	Logging				bool		`bson:"logging,omitempty"`
	Lifecycle			string		`bson:"lifecycle,omitempty"`
	RequestPayment			string		`bson:"request-payment,omitempty"`

	// Not supported props
	// analytics
	// cors
	// metrics
	// replication
	// website
	// accelerate
	// inventory
	// notification

	Ref				int64		`bson:"ref"`
	CntObjects			int64		`bson:"cnt-objects"`
	CntBytes			int64		`bson:"cnt-bytes"`
	Name				string		`bson:"name"`
	CannedAcl			string		`bson:"canned-acl"`
	BasicNotify			*S3BucketNotify	`bson:"notify,omitempty"`

	MaxObjects			int64		`bson:"max-objects"`
	MaxBytes			int64		`bson:"max-bytes"`
}

func s3DirtifyBucket(id bson.ObjectId) error {
	query := bson.M{ "_id": id, "ref": bson.M{ "$eq":  0 } }
	update := bson.M{ "$set": bson.M{ "ref": 1 } }

	return dbS3Update(query, update, false, &S3Bucket{})
}

func (bucket *S3Bucket)dbRemove() (error) {
	query := bson.M{ "cnt-objects": 0 }
	return dbS3RemoveOnState(bucket, S3StateInactive, query)
}

func (bucket *S3Bucket)dbCmtObj(size, ref int64) (error) {
	m := bson.M{ "ref": ref }
	err := dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, true, bucket)
	if err != nil {
		log.Errorf("s3: Can't !account %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	} else {
		log.Debugf("s3: !account %d bytes %s",
			size, infoLong(bucket))
	}
	return err
}

func (bucket *S3Bucket)dbAddObj(size, ref int64) (error) {
	m := bson.M{ "cnt-objects": 1, "cnt-bytes": size, "ref": ref }
	err := dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, true, bucket)
	if err != nil {
		log.Errorf("s3: Can't +account %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	} else {
		log.Debugf("s3: +account %d bytes %s",
			size, infoLong(bucket))
	}
	return err
}

func (bucket *S3Bucket)dbDelObj(size, ref int64) (error) {
	m := bson.M{ "cnt-objects": -1, "cnt-bytes": -size, "ref": ref  }
	err := dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, true, bucket)
	if err != nil {
		log.Errorf("s3: Can't -account %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	} else {
		log.Debugf("s3: -account %d bytes %s",
			size, infoLong(bucket))
	}
	return err
}

func (iam *S3Iam)FindBucket(bname string) (*S3Bucket, error) {
	var res S3Bucket
	var err error

	account, err := iam.s3AccountLookup()
	if err != nil { return nil, err }

	query := bson.M{ "bid": account.BucketBID(bname), "state": S3StateActive }
	err = dbS3FindOne(query, &res)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find bucket %s/%s: %s",
				infoLong(account), infoLong(iam), err.Error())
		}
		return nil, err
	}

	return &res, nil
}

func s3RepairBucketReference(bucket *S3Bucket) error {
	var cnt_objects int64 = 0
	var cnt_bytes int64 = 0
	var objects []S3Object

	query := bson.M{ "bucket-id": bucket.ObjID, "state": S3StateActive }
	err := dbS3FindAll(query, &objects)
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
	err = dbS3Update(nil, update, false, bucket)
	if err != nil {
		log.Errorf("s3: Can't repair bucket %s: %s",
			infoLong(bucket), err.Error())
		return err
	}

	log.Debugf("s3: Repaired reference on %s", infoLong(bucket))
	return nil
}

func s3RepairBucketReferenced() error {
	var buckets []S3Bucket
	var err error

	log.Debugf("s3: Processing referenced buckets")

	err = dbS3FindAll(bson.M{ "ref":  bson.M{ "$ne": 0 } }, &buckets)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairReferenced failed: %s", err.Error())
		return err
	}

	for _, bucket := range buckets {
		log.Debugf("s3: Detected referenced bucket %s", infoLong(&bucket))
		err = s3RepairBucketReference(&bucket)
		if err != nil {
			return err
		}
	}

	return nil
}

func s3RepairBucketInactive() error {
	var buckets []S3Bucket
	var err error

	log.Debugf("s3: Processing inactive buckets")

	err = dbS3FindAllInactive(&buckets)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairBucket failed: %s", err.Error())
		return err
	}

	for _, bucket := range buckets {
		log.Debugf("s3: Detected stale bucket %s", infoLong(&bucket))

		err = radosDeletePool(bucket.BackendID)
		if err != nil {
			log.Errorf("s3: %s backend bucket may stale",
				bucket.BackendID)
		}

		err = dbS3Remove(&bucket)
		if err != nil {
			log.Debugf("s3: Can't remove bucket %s", infoLong(&bucket))
			return err
		}

		log.Debugf("s3: Removed stale bucket %s", infoLong(&bucket))
	}

	return nil
}

func s3RepairBucket() error {
	var err error

	log.Debugf("s3: Running buckets consistency test")

	if err = s3RepairBucketInactive(); err != nil {
		return err
	}

	if err = s3RepairBucketReferenced(); err != nil {
		return err
	}

	log.Debugf("s3: Buckets consistency passed")
	return nil
}

func s3InsertBucket(iam *S3Iam, bname, canned_acl string) (*S3Error) {
	var err error

	account, err := iam.s3AccountLookup()
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	bucket := &S3Bucket{
		ObjID:		bson.NewObjectId(),
		IamObjID:	iam.ObjID,
		State:		S3StateNone,

		Name:		bname,
		CannedAcl:	canned_acl,
		BackendID:	account.BucketBID(bname),
		NamespaceID:	account.NamespaceID(),
		CreationTime:	time.Now().Format(time.RFC3339),
		MaxObjects:	S3StorageMaxObjects,
		MaxBytes:	S3StorageMaxBytes,
	}

	if err = dbS3Insert(bucket); err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = radosCreatePool(bucket.BackendID, uint64(bucket.MaxObjects), uint64(bucket.MaxBytes))
	if err != nil {
		goto out_nopool
	}

	if err = dbS3SetState(bucket, S3StateActive, nil); err != nil {
		goto out
	}

	log.Debugf("s3: Inserted %s", infoLong(bucket))
	return nil

out:
	radosDeletePool(bucket.BackendID)
out_nopool:
	bucket.dbRemove()
	return &S3Error{ ErrorCode: S3ErrInternalError }
}

func s3DeleteBucket(iam *S3Iam, bname, acl string) (*S3Error) {
	var bucket *S3Bucket
	var err error

	bucket, err = iam.FindBucket(bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = dbS3SetState(bucket, S3StateInactive, bson.M{"cnt-objects": 0})
	if err != nil {
		if err == mgo.ErrNotFound {
			if bucket.CntObjects > 0 {
				return &S3Error{ ErrorCode: S3ErrBucketNotEmpty }
			}
		}
		log.Errorf("s3: Can't delete %s: %s", infoLong(bucket), err.Error())
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = radosDeletePool(bucket.BackendID)
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	err = bucket.dbRemove()
	if err != nil {
		return &S3Error{ ErrorCode: S3ErrInternalError }
	}

	log.Debugf("s3: Deleted %s", infoLong(bucket))
	return nil
}

func (bucket *S3Bucket)dbFindAll(query bson.M) ([]S3Object, error) {
	if query == nil { query = make(bson.M) }
	var res []S3Object

	query["bucket-id"] = bucket.ObjID
	query["state"] = S3StateActive

	err := dbS3FindAll(query, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3GetBucketMetricOutput(iam *S3Iam, bname, metric_name string) (*swys3api.S3GetMetricStatisticsOutput, *S3Error) {
	var res swys3api.S3GetMetricStatisticsOutput
	var point swys3api.S3Datapoint

	bucket, err := iam.FindBucket(bname)
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

	if len(params.Delimiter) > 1 { return false }

	if params.MaxKeys <= 0 {
		params.MaxKeys = S3StorageDefaultListObjects
	} else if params.MaxKeys > S3StorageMaxObjects {
		return false
	}

	return true
}

func s3ListBucket(iam *S3Iam, bname string, params *S3ListObjectsRP) (*swys3api.S3Bucket, *S3Error) {
	var start_after, cont_after bool
	var prefixes_map map[string]bool
	var list swys3api.S3Bucket
	var bucket *S3Bucket
	var object S3Object
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var err error

	if params.Validate() == false {
		return nil, &S3Error{ ErrorCode: S3ErrInvalidArgument }
	}

	bucket, err = iam.FindBucket(bname)
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

	pipe = dbS3Pipe(&object, []bson.M{{"$match": query}, {"$sort": bson.M{"key": 1}}})
	iter = pipe.Iter()
	for iter.Next(&object) {
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
			o.Owner.DisplayName = iam.User
			o.Owner.ID = iam.AwsID
		}

		list.Contents = append(list.Contents, o)
		list.KeyCount++

		if list.KeyCount >= list.MaxKeys {
			list.IsTruncated = true
			list.ContinuationToken = params.ContToken
			list.NextContinuationToken = base64_encode([]byte(object.Key))
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

func s3ListBuckets(iam *S3Iam) (*swys3api.S3BucketList, *S3Error) {
	var list swys3api.S3BucketList
	var buckets []S3Bucket
	var err error

	buckets, err = iam.FindBuckets()
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}

		log.Errorf("s3: Can't find buckets %s: %s", infoLong(iam), err.Error())
		return nil, &S3Error{ ErrorCode: S3ErrInternalError }
	}

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
