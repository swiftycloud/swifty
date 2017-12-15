package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"fmt"
)

const (
	S3BucketAclPrivate			= "private"
	S3BucketAclPublicRead			= "public-read"
	S3BucketAclPublicReadWrite		= "public-read-write"
	S3BucketAclAuthenticatedRead		= "authenticated-read"
)

var BucketAcls = []string {
	S3BucketAclPrivate,
	S3BucketAclPublicRead,
	S3BucketAclPublicReadWrite,
	S3BucketAclAuthenticatedRead,
}

type S3BucketNotify struct {
	Events				uint64		`bson:"events"`
	Queue				string		`bson:"queue"`
}

type S3BucketTag struct {
	Key				string		`json:"key" bson:"key"`
	Value				string		`json:"value,omitempty" bson:"value,omitempty"`
}

type S3BucketEncrypt struct {
	Algo				string		`json:"algo" bson:"algo"`
	MasterKeyID			string		`json:"algo,omitempty" bson:"algo,omitempty"`
}

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BackendID			string		`json:"bid,omitempty" bson:"bid,omitempty"`
	NamespaceID			string		`json:"nsid,omitempty" bson:"nsid,omitempty"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`

	// Todo
	Versioning			bool		`json:"versioning,omitempty" bson:"versioning,omitempty"`
	TagSet				[]S3BucketTag	`json:"tags,omitempty" bson:"tags,omitempty"`
	Encrypt				S3BucketEncrypt	`json:"encrypt,omitempty" bson:"encrypt,omitempty"`
	Location			string		`json:"location,omitempty" bson:"location,omitempty"`
	Policy				string		`json:"policy,omitempty" bson:"policy,omitempty"`
	Logging				bool		`json:"logging,omitempty" bson:"logging,omitempty"`
	Lifecycle			string		`json:"lifecycle,omitempty" bson:"lifecycle,omitempty"`
	RequestPayment			string		`json:"request-payment,omitempty" bson:"request-payment,omitempty"`

	// Not supported props
	// analytics
	// cors
	// metrics
	// replication
	// website
	// accelerate
	// inventory
	// notification

	State				uint32		`json:"state" bson:"state"`
	CntObjects			int64		`json:"cnt-objects" bson:"cnt-objects"`
	CntBytes			int64		`json:"cnt-bytes" bson:"cnt-bytes"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`
	BasicNotify			*S3BucketNotify	`bson:"notify,omitempty"`

	MaxObjects			int64		`json:"max-objects" bson:"max-objects"`
	MaxBytes			int64		`json:"max-bytes" bson:"max-bytes"`
}

func (bucket *S3Bucket)ObjectBID(object_name string, version int) string {
	return bucket.BackendID + "-" + fmt.Sprintf("%d", version) + "-" + object_name
}

func (bucket *S3Bucket)dbRemove() (error) {
	var res S3Bucket

	return dbS3RemoveCond(
			bson.M{	"_id": bucket.ObjID,
				"state": S3StateInactive,
				"cnt-objects": 0},
			&res,
		)
}

func (bucket *S3Bucket)dbSetState(state uint32) (error) {
	var res S3Bucket

	return dbS3Update(
			bson.M{"_id": bucket.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]},
				"cnt-objects": 0},
			bson.M{"$set": bson.M{"state": state}},
			&res,
		)
}

func (bucket *S3Bucket)dbAddObj(size int64) (error) {
	var res S3Bucket

	return dbS3Update(
			bson.M{"_id": bucket.ObjID,
				"state": S3StateActive,
			},
			bson.M{"$inc":
				bson.M{
					"cnt-objects": 1,
					"cnt-bytes": size},
				},
			&res,
		)
}

func (bucket *S3Bucket)dbDelObj(size int64) (error) {
	var res S3Bucket

	return dbS3Update(
			bson.M{"_id": bucket.ObjID,
				"state": S3StateActive,
			},
			bson.M{"$inc":
				bson.M{
					"cnt-objects": -1,
					"cnt-bytes": -size},
				},
			&res,
		)
}

func (akey *S3AccessKey)FindBucket(bucket_name string) (*S3Bucket, error) {
	var res S3Bucket

	err := dbS3FindOne(bson.M{"bid": akey.BucketBID(bucket_name)}, &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertBucket(akey *S3AccessKey, bucket_name, acl string) error {
	var err error

	bucket := &S3Bucket{
		Name:		bucket_name,
		Acl:		acl,
		ObjID:		bson.NewObjectId(),
		BackendID:	akey.BucketBID(bucket_name),
		NamespaceID:	akey.NamespaceID(),
		CreationTime:	time.Now().Format(time.RFC3339),
		State:		S3StateNone,
		MaxObjects:	S3StogateMaxObjects,
		MaxBytes:	S3StogateMaxBytes,
	}

	err = dbS3Insert(bucket)
	if err != nil {
		log.Errorf("s3: Can't insert bucket %s: %s",
				bucket.BackendID, err.Error())
		return err
	}

	err = radosCreatePool(bucket.BackendID, uint64(bucket.MaxObjects), uint64(bucket.MaxBytes))
	if err != nil {
		goto out_nopool
	}

	err = bucket.dbSetState(S3StateActive)
	if err != nil {
		log.Errorf("s3: Can't activate bucket %s: %s",
				bucket.BackendID, err.Error())
		goto out
	}

	log.Debugf("s3: Inserted bucket %s", bucket.BackendID)
	return nil

out:
	radosDeletePool(bucket.BackendID)
out_nopool:
	bucket.dbRemove()
	return err
}

func s3DeleteBucket(akey *S3AccessKey, bucket_name, acl string) error {
	var bucketFound *S3Bucket
	var err error

	bucketFound, err = akey.FindBucket(bucket_name)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s", bucket_name, err.Error())
		return err
	}

	err = bucketFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable bucket %s: %s",
				bucketFound.BackendID, err.Error())
		return err
	}

	err = radosDeletePool(bucketFound.BackendID)
	if err != nil {
		return err
	}

	err = bucketFound.dbRemove()
	if err != nil {
		log.Errorf("s3: Can't delete bucket %s: %s",
				bucketFound.BackendID, err.Error())
		return err
	}

	log.Debugf("s3: Deleted bucket %s", bucketFound.BackendID)
	return nil
}

func (bucket *S3Bucket)dbFindAll() ([]S3Object, error) {
	var res []S3Object

	err := dbS3FindOne(
			bson.M{"bucket-id": bucket.ObjID},
			&res)
	if err != nil {
		return nil, err
	}

	return res,nil
}

func s3ListBucket(akey *S3AccessKey, bucket_name, acl string) (*S3BucketList, error) {
	var bucketList S3BucketList
	var bucketFound *S3Bucket
	var r []S3ObjectEntry
	var err error

	bucketFound, err = akey.FindBucket(bucket_name)
	if err != nil {
		log.Errorf("s3: Can't find bucket %s: %s", bucket_name, err.Error())
		return nil, err
	}

	bucketList.Name		= bucketFound.Name
	bucketList.KeyCount	= 0
	bucketList.MaxKeys	= bucketFound.MaxObjects
	bucketList.IsTruncated	= false

	objects, err := bucketFound.dbFindAll()
	if err != nil {
		for _, k := range objects {
			r = append(r,
				S3ObjectEntry {
					Key:	k.Name,
					Size:	k.Size,
				})
			bucketList.KeyCount++
		}
	}

	return &bucketList, nil
}

func s3ListBuckets(akey *S3AccessKey) (*ListAllMyBucketsResult, error) {
	var list ListAllMyBucketsResult
	var buckets []S3Bucket
	var err error

	buckets, err = akey.FindBuckets()
	if err != nil {
		if err == mgo.ErrNotFound {
			err = nil
		}
		return nil, err
	}

	list.Owner.DisplayName	= "Unknown"
	list.Owner.ID		= "Unknown"

	for _, b := range buckets {
		list.Buckets = append(list.Buckets,
			ListAllMyBucketsResultBucket{
				Name:		b.Name,
				CreationDate:	b.CreationTime,
			})
	}

	return &list, nil
}
