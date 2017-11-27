package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"fmt"
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

var ObjectAcls = []string {
	S3ObjectAclPrivate,
	S3ObjectAclPublicRead,
	S3ObjectAclPublicReadWrite,
	S3ObjectAclAuthenticatedRead,
	S3ObjectAclAwsExecRead,
	S3ObjectAclBucketOwnerRead,
	S3ObjectAclBucketOwnerFullControl,
}

type S3ObjectData struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	NextObjID			bson.ObjectId	`bson:"next-id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`	// S3Bucket
	ObjectObjID			bson.ObjectId	`bson:"object-id,omitempty"`	// S3Object
	State				uint32		`json:"state" bson:"state"`
	Size				int64		`json:"size" bson:"size"`
	Data				[]byte		`bson:"data,omitempty"`
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

func (objd *S3ObjectData)dbRemove() (error) {
	return dbS3Remove(
			objd,
			bson.M{"_id": objd.ObjID},
		)
}

func (objd *S3ObjectData)dbFind(object *S3Object) (*S3ObjectData, error) {
	var res S3ObjectData

	err := dbS3FindOne(
			bson.M{"object-id": object.ObjID},
			&res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (object *S3Object)GenOID(akey *S3AccessKey, bucket *S3Bucket) string {
	return bucket.GenOID(akey) + "-" + fmt.Sprintf("%d", object.Version) + "-" + object.Name
}

func (object *S3Object)GetName(akey *S3AccessKey, bucket *S3Bucket) string {
	index := len(akey.Namespace()) + len(object.Name) + 2
	index += len(fmt.Sprintf("%d", object.Version))
	return object.Name[index:]
}

func (object *S3Object)dbRemove() (error) {
	var res S3Object

	return dbS3RemoveCond(
			bson.M{	"_id": object.ObjID,
				"state": S3StateInactive},
			&res,
		)
}

func (object *S3Object)dbSetState(state uint32) (error) {
	var res S3Object

	return dbS3Update(
			bson.M{"_id": object.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": bson.M{"state": state}},
			&res,
		)
}

func (object *S3Object)dbFindOID(akey *S3AccessKey, bucket *S3Bucket) (*S3Object, error) {
	var res S3Object

	err := dbS3FindOne(
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

	err = dbS3Insert(object)
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
	var size int64

	size = int64(len(data))

	if radosDisabled || size <= S3StorageSizePerObj {
		objd := S3ObjectData{
			ObjID:		bson.NewObjectId(),
			BucketObjID:	bucket.ObjID,
			ObjectObjID:	object.ObjID,
			Size:		size,
			Data:		data,
		}

		if objd.Size > S3StorageSizePerObj {
			log.Errorf("s3: Too big object to store %d", objd.Size)
			err = fmt.Errorf("s3: Object is too big")
			goto out
		}

		err = dbS3Insert(objd)
		if err != nil {
			goto out
		}
	} else {
		err = radosWriteObject(bucket.OID, object.Name, data)
		if err != nil {
			goto out
		}
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
	var objdFound *S3ObjectData
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

	if radosDisabled || objectFound.Size <= S3StorageSizePerObj {
		objdFound, err = objdFound.dbFind(objectFound)
		if err != nil {
			if err == mgo.ErrNotFound {
				return nil
			}
			log.Errorf("s3: Can't find object stored %s: %s",
					objectFound.OID, err.Error())
			return err
		}
		err = objdFound.dbRemove()
		if err != nil {
			log.Errorf("s3: Can't delete object stored %s: %s",
					objectFound.OID, err.Error())
			return err
		}
	} else {
		err = radosDeleteObject(bucketFound.OID, objectFound.OID)
		if err != nil {
			return err
		}
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

func s3ReadObject(akey *S3AccessKey, bucket *S3Bucket, object *S3Object) ([]byte, error) {
	var objdFound *S3ObjectData
	var bucketFound *S3Bucket
	var objectFound *S3Object
	var res []byte
	var err error

	bucketFound, err = bucket.dbFindOID(akey)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find bucket %s: %s",
				bucket.GenOID(akey), err.Error())
		return nil, err
	}

	objectFound, err = object.dbFindOID(akey, bucketFound)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find object %s: %s",
				object.GenOID(akey, bucket), err.Error())
		return nil, err
	}

	if radosDisabled || objectFound.Size <= S3StorageSizePerObj {
		objdFound, err = objdFound.dbFind(objectFound)
		if err != nil {
			if err == mgo.ErrNotFound {
				return nil, err
			}
			log.Errorf("s3: Can't find object stored %s: %s",
					objectFound.OID, err.Error())
			return nil, err
		}
		res = objdFound.Data
	} else {
		res, err = radosReadObject(bucketFound.OID, objectFound.OID,
						uint64(objectFound.Size))
		if err != nil {
			return nil, err
		}
	}

	log.Debugf("s3: Read object %s", objectFound.OID)
	return res, err
}
