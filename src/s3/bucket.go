package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
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

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BackendID			string		`json:"bid,omitempty" bson:"bid,omitempty"`
	State				uint32		`json:"state" bson:"state"`
	CntObjects			int64		`json:"cnt-objects" bson:"cnt-objects"`
	CntBytes			int64		`json:"cnt-bytes" bson:"cnt-bytes"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`

	MaxObjects			int64		`json:"max-objects" bson:"max-objects"`
	MaxBytes			int64		`json:"max-bytes" bson:"max-bytes"`
}

func (bucket *S3Bucket)GenBackendId(akey *S3AccessKey) string {
	return akey.Namespace() + "-" + bucket.Name
}

func (bucket *S3Bucket)GetName(akey *S3AccessKey) string {
	index := len(akey.Namespace()) + 1
	return bucket.Name[index:]
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

func (bucket *S3Bucket)dbFindByKey(akey *S3AccessKey) (*S3Bucket, error) {
	var res S3Bucket

	regex := "^" + akey.Namespace() + ".+"
	query := bson.M{"bid": bson.M{"$regex": bson.RegEx{regex, ""}}}

	err := dbS3FindOne(
			query,
			&res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func (bucket *S3Bucket)dbFindByBackendID(akey *S3AccessKey) (*S3Bucket, error) {
	var res S3Bucket

	err := dbS3FindOne(
			bson.M{"bid": bucket.GenBackendId(akey)},
			&res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertBucket(akey *S3AccessKey, bucket *S3Bucket) error {
	var err error

	bucket.ObjID		= bson.NewObjectId()
	bucket.BackendID	= bucket.GenBackendId(akey)
	bucket.State		= S3StateNone
	bucket.CntObjects	= 0
	bucket.CntBytes		= 0
	bucket.MaxObjects	= S3StogateMaxObjects
	bucket.MaxBytes		= S3StogateMaxBytes

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

func s3DeleteBucket(akey *S3AccessKey, bucket *S3Bucket) error {
	var bucketFound *S3Bucket
	var err error

	bucketFound, err = bucket.dbFindByBackendID(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenBackendId(akey), err.Error())
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

func s3ListBucket(akey *S3AccessKey, bucket *S3Bucket) (*S3BucketList, error) {
	var bucketList S3BucketList
	var bucketFound *S3Bucket
	var r []S3ObjectEntry
	var err error

	bucketFound, err = bucket.dbFindByBackendID(akey)
	if err != nil {
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenBackendId(akey), err.Error())
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
