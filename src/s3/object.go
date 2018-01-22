package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"crypto/md5"
	"time"
	"fmt"

	"../apis/apps/s3"
)

var ObjectAcls = []string {
	swys3api.S3ObjectAclPrivate,
	swys3api.S3ObjectAclPublicRead,
	swys3api.S3ObjectAclPublicReadWrite,
	swys3api.S3ObjectAclAuthenticatedRead,
	swys3api.S3ObjectAclAwsExecRead,
	swys3api.S3ObjectAclBucketOwnerRead,
	swys3api.S3ObjectAclBucketOwnerFullControl,
}

type S3ObjectData struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	ObjectObjID			bson.ObjectId	`bson:"object-id,omitempty"`	// S3Object
	State				uint32		`json:"state" bson:"state"`
	Size				int64		`json:"size" bson:"size"`
	Data				[]byte		`bson:"data,omitempty"`
}

type S3ObjectPorps struct {
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`
	Acl				string		`json:"acl,omitempty" bson:"acl,omitempty"`
	Key				string		`json:"key" bson:"key"`

	// Todo
	Meta				[]S3Tag		`json:"meta,omitempty" bson:"meta,omitempty"`
	TagSet				[]S3Tag		`json:"tags,omitempty" bson:"tags,omitempty"`
	Policy				string		`json:"policy,omitempty" bson:"policy,omitempty"`

	// Not supported props
	// torrent
	// objects archiving
}

type S3Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	BackendID			string		`json:"bid" bson:"bid"`
	State				uint32		`json:"state" bson:"state"`
	Version				int		`json:"version" bson:"version"`
	Size				int64		`json:"size" bson:"size"`
	ETag				string		`json:"etag" bson:"etag"`

	S3ObjectPorps					`json:",inline" bson:",inline"`
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

func (object *S3Object)dbSet(state uint32, fields bson.M) (error) {
	var res S3Object

	return dbS3Update(
			bson.M{"_id": object.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": fields},
			&res,
		)
}

func (object *S3Object)dbSetState(state uint32) (error) {
	return object.dbSet(state, bson.M{"state": state})
}

func (object *S3Object)dbSetStateEtag(state uint32, etag string) (error) {
	return object.dbSet(state, bson.M{"state": state, "etag": etag})
}

func (bucket *S3Bucket)FindObject(oname string, version int) (*S3Object, error) {
	var res S3Object

	err := dbS3FindOne(bson.M{"bid": bucket.ObjectBID(oname, version)}, &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3InsertObject(bucket *S3Bucket, oname string, version int,
			size int64, acl string) (*S3Object, error) {
	var err error

	object := &S3Object {
		S3ObjectPorps: S3ObjectPorps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		Version:	version,
		Size:		size,
		ObjID:		bson.NewObjectId(),
		BucketObjID:	bucket.ObjID,
		BackendID:	bucket.ObjectBID(oname, version),
		State:		S3StateNone,
	}

	err = dbS3Insert(object)
	if err != nil {
		log.Errorf("s3: Can't insert object %s: %s", oname, err.Error())
		return nil, err
	}

	err = bucket.dbAddObj(object.Size)
	if err != nil {
		log.Errorf("s3: Can't +account object %s: %s", oname, err.Error())
		object.dbRemove()
		return nil, err
	}

	log.Debugf("s3: Inserted object %s", object.BackendID)
	return object, nil
}

func s3CommitObject(namespace string, bucket *S3Bucket, object *S3Object, data []byte) (string, error) {
	var err error
	var size int64
	var etag string

	size = int64(len(data))

	if radosDisabled || size <= S3StorageSizePerObj {
		objd := S3ObjectData{
			ObjID:		bson.NewObjectId(),
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
		err = radosWriteObject(bucket.BackendID, object.BackendID, data, 0)
		if err != nil {
			goto out
		}
	}

	if bucket.BasicNotify != nil {
		s3Notify(namespace, bucket, object, S3NotifyPut)
	}

	etag = fmt.Sprintf("%x", md5.Sum(data))
	if etag == "" {
		log.Errorf("s3: Can't calculate ETag on object %s: %s",
				object.BackendID, err.Error())
		goto out
	}

	err = object.dbSetStateEtag(S3StateActive, etag)
	if err != nil {
		log.Errorf("s3: Can't activate object %s: %s",
				object.BackendID, err.Error())
		goto out
	}

	log.Debugf("s3: Committed object %s", object.BackendID)
	return etag, nil

out:
	err1 := bucket.dbDelObj(object.Size)
	if err1 != nil {
		log.Errorf("s3: Can't -account object %s: %s",
				object.BackendID, err1.Error())
	}
	object.dbRemove()
	return "", err
}

func s3DeleteObjectFound(bucket *S3Bucket, objectFound *S3Object) error {
	var objdFound *S3ObjectData
	var err error

	err = objectFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable object %s: %s", objectFound.Key, err.Error())
		return err
	}

	if radosDisabled || objectFound.Size <= S3StorageSizePerObj {
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
		err = radosDeleteObject(bucket.BackendID, objectFound.BackendID)
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

func s3DeleteObject(bucket *S3Bucket, oname string, version int) error {
	var objectFound *S3Object
	var err error

	objectFound, err = bucket.FindObject(oname, version)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s: %s", oname, err.Error())
		return err
	}

	return s3DeleteObjectFound(bucket, objectFound)
}

func s3ReadObjectData(bucket *S3Bucket, object *S3Object) ([]byte, error) {
	var objd *S3ObjectData
	var res []byte
	var err error

	if radosDisabled || object.Size <= S3StorageSizePerObj {
		objd, err = objd.dbFind(object)
		if err != nil {
			if err == mgo.ErrNotFound {
				return nil, err
			}
			log.Errorf("s3: Can't find object stored %s: %s",
					object.Key, err.Error())
			return nil, err
		}
		res = objd.Data
	} else {
		res, err = radosReadObject(bucket.BackendID, object.BackendID,
						uint64(object.Size), 0)
		if err != nil {
			return nil, err
		}
	}

	log.Debugf("s3: Read object %s", object.BackendID)
	return res, err
}

func s3ReadObject(bucket *S3Bucket, oname string, part, version int) ([]byte, error) {
	var object *S3Object
	var err error

	object, err = bucket.FindObject(oname, version)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find object %s: %s",
				oname, err.Error())
		return nil, err
	}

	return s3ReadObjectData(bucket, object)
}
