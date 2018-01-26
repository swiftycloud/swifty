package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"

	"../apis/apps/s3"
)

var BucketAcls = []string {
	swys3api.S3BucketAclPrivate,
	swys3api.S3BucketAclPublicRead,
	swys3api.S3BucketAclPublicReadWrite,
	swys3api.S3BucketAclAuthenticatedRead,
}

type S3BucketNotify struct {
	Events				uint64		`bson:"events"`
	Queue				string		`bson:"queue"`
}

type S3Tag struct {
	Key				string		`json:"key" bson:"key"`
	Value				string		`json:"value,omitempty" bson:"value,omitempty"`
}

type S3BucketEncrypt struct {
	Algo				string		`json:"algo" bson:"algo"`
	MasterKeyID			string		`json:"algo,omitempty" bson:"algo,omitempty"`
}

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`json:"mtime,omitempty" bson:"mtime,omitempty"`
	State				uint32		`json:"state" bson:"state"`

	BackendID			string		`json:"bid,omitempty" bson:"bid,omitempty"`
	NamespaceID			string		`json:"nsid,omitempty" bson:"nsid,omitempty"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`

	// Todo
	Versioning			bool		`json:"versioning,omitempty" bson:"versioning,omitempty"`
	TagSet				[]S3Tag		`json:"tags,omitempty" bson:"tags,omitempty"`
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

	CntObjects			int64		`json:"cnt-objects" bson:"cnt-objects"`
	CntBytes			int64		`json:"cnt-bytes" bson:"cnt-bytes"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`
	BasicNotify			*S3BucketNotify	`bson:"notify,omitempty"`

	MaxObjects			int64		`json:"max-objects" bson:"max-objects"`
	MaxBytes			int64		`json:"max-bytes" bson:"max-bytes"`
}

func (bucket *S3Bucket)dbRemove() (error) {
	query := bson.M{ "cnt-objects": 0 }
	return dbS3RemoveOnState(bucket, S3StateInactive, query)
}

func (bucket *S3Bucket)dbAddObj(size int64) (error) {
	m := bson.M{ "cnt-objects": 1, "cnt-bytes": size }
	err := dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, bucket)
	if err != nil {
		log.Errorf("s3: Can't +account %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	}

	return err
}

func (bucket *S3Bucket)dbDelObj(size int64) (error) {
	m := bson.M{ "cnt-objects": -1, "cnt-bytes": -size }
	err := dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, bucket)
	if err != nil {
		log.Errorf("s3: Can't -account %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	}
	return err
}

func (iam *S3Iam)FindBucket(key *S3AccessKey, bname string) (*S3Bucket, error) {
	var res S3Bucket
	var err error

	err = key.CheckBucketAccess(bname)
	if err != nil {
		return nil, err
	}

	query := bson.M{ "bid": iam.BucketBID(bname), "state": S3StateActive }
	err = dbS3FindOne(query, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3RepairBucket() error {
	var buckets []S3Bucket
	var err error

	log.Debugf("s3: Running buckets consistency test")

	err = dbS3FindAll(bson.M{ "state": S3StateNone }, &buckets)
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

	log.Debugf("s3: Buckets consistency passed")
	return nil
}

func s3InsertBucket(iam *S3Iam, akey *S3AccessKey, bname, acl string) error {
	var err error

	err = akey.CheckBucketAccess(bname)
	if err != nil {
		return err
	}

	bucket := &S3Bucket{
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		Name:		bname,
		Acl:		acl,
		BackendID:	iam.BucketBID(bname),
		NamespaceID:	iam.NamespaceID(),
		CreationTime:	time.Now().Format(time.RFC3339),
		MaxObjects:	S3StorageMaxObjects,
		MaxBytes:	S3StorageMaxBytes,
	}

	if err = dbS3Insert(bucket); err != nil {
		return err
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
	return err
}

func s3DeleteBucket(iam *S3Iam, akey *S3AccessKey, bname, acl string) error {
	var bucket *S3Bucket
	var err error

	bucket, err = iam.FindBucket(akey, bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s", bname, err.Error())
		return err
	}

	err = dbS3SetState(bucket, S3StateInactive, bson.M{"cnt-objects": 0})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	err = radosDeletePool(bucket.BackendID)
	if err != nil {
		return err
	}

	err = bucket.dbRemove()
	if err != nil {
		return err
	}

	log.Debugf("s3: Deleted %s", infoLong(bucket))
	return nil
}

func (bucket *S3Bucket)dbFindAll() ([]S3Object, error) {
	var res []S3Object

	query := bson.M{ "bucket-id": bucket.ObjID, "state": S3StateActive }
	err := dbS3FindAll(query, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3ListBucket(iam *S3Iam, akey *S3AccessKey, bname, acl string) (*swys3api.S3Bucket, error) {
	var bucketList swys3api.S3Bucket
	var bucket *S3Bucket
	var err error

	bucket, err = iam.FindBucket(akey, bname)
	if err != nil {
		log.Errorf("s3: Can't find bucket %s: %s", bname, err.Error())
		return nil, err
	}

	bucketList.Name		= bucket.Name
	bucketList.KeyCount	= 0
	bucketList.MaxKeys	= bucket.MaxObjects
	bucketList.IsTruncated	= false


	objects, err := bucket.dbFindAll()
	if err != nil {
		if err == mgo.ErrNotFound {
			return &bucketList, nil
		}

		log.Errorf("s3: Can't find objects %s: %s",
			infoLong(bucket), err.Error())
		return nil, err
	}

	for _, k := range objects {
		bucketList.Contents = append(bucketList.Contents,
			swys3api.S3Object {
				Key:		k.Key,
				Size:		k.Size,
				LastModified:	k.CreationTime,
			})
		bucketList.KeyCount++
	}

	return &bucketList, nil
}

func s3ListBuckets(iam *S3Iam, akey *S3AccessKey) (*swys3api.S3BucketList, error) {
	var list swys3api.S3BucketList
	var buckets []S3Bucket
	var err error

	buckets, err = iam.FindBuckets(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			err = nil
		}
		return nil, err
	}

	list.Owner.DisplayName	= "Unknown"
	list.Owner.ID		= "Unknown"

	for _, b := range buckets {
		list.Buckets.Bucket = append(list.Buckets.Bucket,
			swys3api.S3BucketListEntry{
				Name:		b.Name,
				CreationDate:	b.CreationTime,
			})
	}

	return &list, nil
}
