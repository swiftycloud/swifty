package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
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

var cachedObjSize int64

type S3ObjectData struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	ObjectObjID			bson.ObjectId	`bson:"object-id,omitempty"`	// S3Object
	State				uint32		`json:"state" bson:"state"`
	Size				int64		`json:"size" bson:"size"`
	Data				[]byte		`bson:"data,omitempty"`
}

type S3Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	BackendID			string		`json:"bid" bson:"bid"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`
	State				uint32		`json:"state" bson:"state"`
	Name				string		`json:"name" bson:"name"`
	Acl				string		`json:"acl" bson:"acl"`
	Version				int32		`json:"version" bson:"version"`
	Size				int64		`json:"size" bson:"size"`

	// Todo
	TagSet				[]S3Tag		`json:"tags,omitempty" bson:"tags,omitempty"`
	Policy				string		`json:"policy,omitempty" bson:"policy,omitempty"`

	// Not supported props
	// torrent
	// objects archiving
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

func (bucket *S3Bucket)FindObject(object_name string, version int) (*S3Object, error) {
	var res S3Object

	err := dbS3FindOne(bson.M{"bid": bucket.ObjectBID(object_name, version)}, &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertObject(bucket *S3Bucket, object_name string, version int,
		objct_size int64, acl string) (*S3Object, error) {
	var err error

	object := &S3Object {
		Name:		object_name,
		Version:	int32(version),
		Size:		objct_size,
		Acl:		acl,
		ObjID:		bson.NewObjectId(),
		BucketObjID:	bucket.ObjID,
		BackendID:	bucket.ObjectBID(object_name, version),
		CreationTime:	time.Now().Format(time.RFC3339),
		State:		S3StateNone,
	}

	err = dbS3Insert(object)
	if err != nil {
		log.Errorf("s3: Can't insert object %s: %s", object_name, err.Error())
		return nil, err
	}

	err = bucket.dbAddObj(object.Size)
	if err != nil {
		log.Errorf("s3: Can't +account object %s: %s", object_name, err.Error())
		object.dbRemove()
		return nil, err
	}

	log.Debugf("s3: Inserted object %s", object.BackendID)
	return object, nil
}

func s3CommitObject(namespace string, bucket *S3Bucket, object *S3Object, data []byte) error {
	var err error
	var size int64

	size = int64(len(data))

	if radosDisabled || size <= cachedObjSize {
		objd := S3ObjectData{
			ObjID:		bson.NewObjectId(),
			ObjectObjID:	object.ObjID,
			Size:		size,
			Data:		data,
		}

		if objd.Size > cachedObjSize {
			log.Errorf("s3: Too big object to store %d", objd.Size)
			err = fmt.Errorf("s3: Object is too big")
			goto out
		}

		err = dbS3Insert(objd)
		if err != nil {
			goto out
		}
	} else {
		err = radosWriteObject(bucket.BackendID, object.Name, data)
		if err != nil {
			goto out
		}
	}

	if bucket.BasicNotify != nil {
		s3Notify(namespace, bucket, object, S3NotifyPut)
	}

	err = object.dbSetState(S3StateActive)
	if err != nil {
		log.Errorf("s3: Can't activate object %s: %s",
				object.BackendID, err.Error())
		goto out
	}

	log.Debugf("s3: Committed object %s", object.BackendID)
	return nil

out:
	err1 := bucket.dbDelObj(object.Size)
	if err1 != nil {
		log.Errorf("s3: Can't -account object %s: %s",
				object.BackendID, err1.Error())
	}
	object.dbRemove()
	return err
}

func s3DeleteObject(bucket *S3Bucket, object_name string, version int) error {
	var objdFound *S3ObjectData
	var objectFound *S3Object
	var err error

	objectFound, err = bucket.FindObject(object_name, version)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s: %s", object_name, err.Error())
		return err
	}

	err = objectFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable object %s: %s", object_name, err.Error())
		return err
	}

	if radosDisabled || objectFound.Size <= cachedObjSize {
		objdFound, err = objdFound.dbFind(objectFound)
		if err != nil {
			if err == mgo.ErrNotFound {
				return nil
			}
			log.Errorf("s3: Can't find object stored %s: %s",
					objectFound.BackendID, err.Error())
			return err
		}
		err = objdFound.dbRemove()
		if err != nil {
			log.Errorf("s3: Can't delete object stored %s: %s",
					objectFound.BackendID, err.Error())
			return err
		}
	} else {
		err = radosDeleteObject(bucket.BackendID, objectFound.Name)
		if err != nil {
			return err
		}
	}

	err = bucket.dbDelObj(objectFound.Size)
	if err != nil {
		log.Errorf("s3: Can't -account object %s: %s",
				objectFound.BackendID, err.Error())
		return err
	}

	err = objectFound.dbRemove()
	if err != nil {
		log.Errorf("s3: Can't delete object %s: %s",
				objectFound.BackendID, err.Error())
		return err
	}

	log.Debugf("s3: Deleted object %s", objectFound.BackendID)
	return nil
}

func s3ReadObject(bucket *S3Bucket, object_name string, version int) ([]byte, error) {
	var objdFound *S3ObjectData
	var objectFound *S3Object
	var res []byte
	var err error

	objectFound, err = bucket.FindObject(object_name, version)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find object %s: %s", object_name, err.Error())
		return nil, err
	}

	if radosDisabled || objectFound.Size <= cachedObjSize {
		objdFound, err = objdFound.dbFind(objectFound)
		if err != nil {
			if err == mgo.ErrNotFound {
				return nil, err
			}
			log.Errorf("s3: Can't find object stored %s: %s", object_name, err.Error())
			return nil, err
		}
		res = objdFound.Data
	} else {
		res, err = radosReadObject(bucket.BackendID, objectFound.Name,
						uint64(objectFound.Size))
		if err != nil {
			return nil, err
		}
	}

	log.Debugf("s3: Read object %s", objectFound.BackendID)
	return res, err
}
