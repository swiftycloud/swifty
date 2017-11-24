package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"net/http"
	"fmt"
)

const (
	S3BucketAclPrivate			= "private"
	S3BucketAclPublicRead			= "public-read"
	S3BucketAclPublicReadWrite		= "public-read-write"
	S3BucketAclAuthenticatedRead		= "authenticated-read"
)

const (
	S3ObjectAclPrivate			= "private"
	S3ObjectAclPublicRead			= "public-read"
	S3ObjectAclPublicReadWrite		= "public-read-write"
	S3ObjectAclAuthenticatedRead		= "authenticated-read"
	S3ObjectAclAwsExecRead			= "aws-exec-read"
	S3ObjectAclBucketOwnerRead		= "bucket-owner-read"
	S3ObjectAclBucketOwnerFullControl	= "bucket-owner-full-control"
)

var BucketAcls = []string {
	S3BucketAclPrivate,
	S3BucketAclPublicRead,
	S3BucketAclPublicReadWrite,
	S3BucketAclAuthenticatedRead,
}

var ObjectAcls = []string {
	S3ObjectAclPrivate,
	S3ObjectAclPublicRead,
	S3ObjectAclPublicReadWrite,
	S3ObjectAclAuthenticatedRead,
	S3ObjectAclAwsExecRead,
	S3ObjectAclBucketOwnerRead,
	S3ObjectAclBucketOwnerFullControl,
}

func verifyAclValue(acl string, acls []string) bool {
	for _, v := range acls {
		if acl == v {
			return true
		}
	}

	return false
}

const (
	S3StateNone			= 0
	S3StateActive			= 1
	S3StateInactive			= 2
)

type S3Bucket struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	OID				string		`json:"oid,omitempty" bson:"oid,omitempty"`
	State				uint32		`json:"state" bson:"state"`
	CntObjects			int64		`json:"cnt-objects" bson:"cnt-objects"`
	CntBytes			int64		`json:"cnt-bytes" bson:"cnt-bytes"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`

	MaxObjects			int64		`json:"max-objects" bson:"max-objects"`
	MaxBytes			int64		`json:"max-bytes" bson:"max-bytes"`
}

type S3Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	OID				string		`json:"oid" bson:"oid"`
	State				uint32		`json:"state" bson:"state"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`
	Version				int32		`json:"version" bson:"version"`
	Size				int64		`json:"size" bson:"size"`
}

const (
	S3StogateMaxObjects		= int64(10000)
	S3StogateMaxBytes		= int64(100 << 20)
)

func dbS3Insert(collection string, o interface{}) (error) {
	return dbSession.DB(dbName).C(collection).Insert(o)
}

func dbS3Remove(collection string, query bson.M) (error) {
	return dbSession.DB(dbName).C(collection).Remove(query)
}

func dbS3Update(collection string, query bson.M, update bson.M, o interface{}) (error) {
	c := dbSession.DB(dbName).C(collection)
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3RemoveCond(collection string, query bson.M, o interface{}) (error) {
	c := dbSession.DB(dbName).C(collection)
	change := mgo.Change{
		Upsert:		false,
		Remove:		true,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3FindOne(collection string, query bson.M, o interface{}) (error) {
	return dbSession.DB(dbName).C(collection).Find(query).One(o)
}

func dbS3FindAll(collection string, query bson.M, o interface{}) (error) {
	return dbSession.DB(dbName).C(collection).Find(query).All(o)
}

func (bucket *S3Bucket)GenOID(akey *S3AccessKey) string {
	return akey.Namespace() + "-" + bucket.Name
}

func (bucket *S3Bucket)GetName(akey *S3AccessKey) string {
	index := len(akey.Namespace()) + 1
	return bucket.Name[index:]
}

func (object *S3Object)GenOID(akey *S3AccessKey, bucket *S3Bucket) string {
	return bucket.GenOID(akey) + "-" + fmt.Sprintf("%d", object.Version) + "-" + object.Name
}

func (object *S3Object)GetName(akey *S3AccessKey, bucket *S3Bucket) string {
	index := len(akey.Namespace()) + len(object.Name) + 2
	index += len(fmt.Sprintf("%d", object.Version))
	return object.Name[index:]
}

func (bucket *S3Bucket)dbCollection() (string) {
	return DBColS3Buckets
}

func (bucket *S3Bucket)dbInsert() (error) {
	return dbS3Insert(bucket.dbCollection(), bucket)
}

func (bucket *S3Bucket)dbRemove() (error) {
	var res S3Bucket

	return dbS3RemoveCond(
			bucket.dbCollection(),
			bson.M{	"_id": bucket.ObjID,
				"state": S3StateInactive,
				"cnt-objects": 0},
			&res,
		)
}

var s3StateTransition = map[uint32][]uint32 {
	S3StateNone:		[]uint32{ S3StateNone, },
	S3StateActive:		[]uint32{ S3StateNone, },
	S3StateInactive:	[]uint32{ S3StateActive, },
}

func (bucket *S3Bucket)dbSetState(state uint32) (error) {
	var res S3Bucket

	return dbS3Update(
			bucket.dbCollection(),
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
			bucket.dbCollection(),
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
			bucket.dbCollection(),
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
	query := bson.M{"oid": bson.M{"$regex": bson.RegEx{regex, ""}}}

	err := dbS3FindOne(
			bucket.dbCollection(),
			query,
			&res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func (bucket *S3Bucket)dbFindOID(akey *S3AccessKey) (*S3Bucket, error) {
	var res S3Bucket

	err := dbS3FindOne(
			bucket.dbCollection(),
			bson.M{"oid": bucket.GenOID(akey)},
			&res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertBucket(akey *S3AccessKey, bucket *S3Bucket) error {
	var err error

	bucket.ObjID		= bson.NewObjectId()
	bucket.OID		= bucket.GenOID(akey)
	bucket.State		= S3StateNone
	bucket.CntObjects	= 0
	bucket.CntBytes		= 0
	bucket.MaxObjects	= S3StogateMaxObjects
	bucket.MaxBytes		= S3StogateMaxBytes

	err = bucket.dbInsert()
	if err != nil {
		log.Errorf("s3: Can't insert bucket %s: %s",
				bucket.OID, err.Error())
		return err
	}

	err = radosCreatePool(bucket.OID, uint64(bucket.MaxObjects), uint64(bucket.MaxBytes))
	if err != nil {
		goto out_nopool
	}

	err = bucket.dbSetState(S3StateActive)
	if err != nil {
		log.Errorf("s3: Can't activate bucket %s: %s",
				bucket.OID, err.Error())
		goto out
	}

	log.Debugf("s3: Inserted bucket %s", bucket.OID)
	return nil

out:
	radosDeletePool(bucket.OID)
out_nopool:
	bucket.dbRemove()
	return err
}

func s3DeleteBucket(akey *S3AccessKey, bucket *S3Bucket) error {
	var bucketFound *S3Bucket
	var err error

	bucketFound, err = bucket.dbFindOID(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenOID(akey), err.Error())
		return err
	}

	err = bucketFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable bucket %s: %s",
				bucketFound.OID, err.Error())
		return err
	}

	err = radosDeletePool(bucketFound.OID)
	if err != nil {
		return err
	}

	err = bucketFound.dbRemove()
	if err != nil {
		log.Errorf("s3: Can't delete bucket %s: %s",
				bucketFound.OID, err.Error())
		return err
	}

	log.Debugf("s3: Deleted bucket %s", bucketFound.OID)
	return nil
}

func (bucket *S3Bucket)dbFindAll() ([]S3Object, error) {
	var res []S3Object
	var t S3Object

	err := dbS3FindOne(
			t.dbCollection(),
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

	bucketFound, err = bucket.dbFindOID(akey)
	if err != nil {
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenOID(akey), err.Error())
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

func (object *S3Object)dbCollection() (string) {
	return DBColS3Objects
}

func (object *S3Object)dbInsert() (error) {
	return dbS3Insert(object.dbCollection(), object)
}

func (object *S3Object)dbRemove() (error) {
	var res S3Object

	return dbS3RemoveCond(
			object.dbCollection(),
			bson.M{	"_id": object.ObjID,
				"state": S3StateInactive},
			&res,
		)
}

func (object *S3Object)dbSetState(state uint32) (error) {
	var res S3Object

	return dbS3Update(
			object.dbCollection(),
			bson.M{"_id": object.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": bson.M{"state": state}},
			&res,
		)
}

func (object *S3Object)dbFindOID(akey *S3AccessKey, bucket *S3Bucket) (*S3Object, error) {
	var res S3Object

	err := dbS3FindOne(
			object.dbCollection(),
			bson.M{"oid": object.GenOID(akey, bucket)},
			&res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertObject(akey *S3AccessKey, bucket *S3Bucket, object *S3Object) error {
	var bucketFound *S3Bucket
	var err error

	bucketFound, err = bucket.dbFindOID(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenOID(akey), err.Error())
		return err
	}

	object.ObjID		= bson.NewObjectId()
	object.BucketObjID	= bucketFound.ObjID
	object.OID		= object.GenOID(akey, bucketFound)
	object.State		= S3StateNone

	err = object.dbInsert()
	if err != nil {
		log.Errorf("s3: Can't insert object %s: %s",
				object.OID, err.Error())
		return err
	}

	err = bucketFound.dbAddObj(object.Size)
	if err != nil {
		log.Errorf("s3: Can't +account object %s: %s",
				object.OID, err.Error())
		object.dbRemove()
	}

	log.Debugf("s3: Inserted object %s", object.OID)
	return nil
}

func s3CommitObject(bucket *S3Bucket, object *S3Object, data []byte) error {
	var err error

	err = radosWriteObject(bucket.OID, object.Name, data)
	if err != nil {
		goto out
	}

	err = object.dbSetState(S3StateActive)
	if err != nil {
		log.Errorf("s3: Can't activate object %s: %s",
				object.OID, err.Error())
		goto out
	}

	log.Debugf("s3: Committed object %s", object.OID)
	return nil

out:
	err1 := bucket.dbDelObj(object.Size)
	if err1 != nil {
		log.Errorf("s3: Can't -account object %s: %s",
				object.OID, err1.Error())
	}
	object.dbRemove()
	return err
}

func s3DeleteObject(akey *S3AccessKey, bucket *S3Bucket, object *S3Object) error {
	var bucketFound *S3Bucket
	var objectFound *S3Object
	var err error

	bucketFound, err = bucket.dbFindOID(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenOID(akey), err.Error())
		return err
	}

	objectFound, err = object.dbFindOID(akey, bucketFound)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s: %s",
				object.GenOID(akey, bucket), err.Error())
		return err
	}

	err = objectFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable object %s: %s",
				objectFound.OID, err.Error())
		return err
	}

	err = radosDeleteObject(bucketFound.OID, objectFound.OID)
	if err != nil {
		return err
	}

	err = bucketFound.dbDelObj(objectFound.Size)
	if err != nil {
		log.Errorf("s3: Can't -account object %s: %s",
				objectFound.OID, err.Error())
		return err
	}

	err = objectFound.dbRemove()
	if err != nil {
		log.Errorf("s3: Can't delete object %s: %s",
				objectFound.OID, err.Error())
		return err
	}

	log.Debugf("s3: Deleted object %s", objectFound.OID)
	return nil
}

func s3ReadObject(akey *S3AccessKey, bucket *S3Bucket, object *S3Object) error {
	return nil
}

func s3CheckAccess(akey *S3AccessKey, bucket_name, object_name string) error {
	// FIXME Implement lookup and ACL, for now just allow
	return nil
}

func s3VerifyAuthorization(r *http.Request) (*S3AccessKey, error) {
	var akey *S3AccessKey = nil
	var err error = nil

	accessKey := member(r.Header.Get("Authorization"),
				"Credential=", "/")
	if accessKey != "" {
		akey, _, _ = dbLookupAccessKey(accessKey)
		if akey == nil {
			err = fmt.Errorf("Authorization: No access key %v found", accessKey)
		}
	} else {
		err = fmt.Errorf("Authorization: No access key supplied")
	}

	return akey, err
}
