package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
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

func (object *S3Object)infoLong() (string) {
	return fmt.Sprintf("object: %s/%s/%s/%d/%s",
			object.ObjID, object.BucketObjID,
			object.BackendID, object.State,
			object.Key)
}

func (object *S3Object)dbRemove() (error) {
	var res S3Object

	return dbS3RemoveCond(
			bson.M{	"_id": object.ObjID,
				"state": S3StateInactive},
			&res)
}

func (object *S3Object)dbSet(state uint32, fields bson.M) (error) {
	var res S3Object

	return dbS3Update(
			bson.M{"_id": object.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": fields},
			&res)
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

func s3AddObject(namespace string, bucket *S3Bucket, oname string,
		acl string, size int64, data []byte) (*S3Object, error) {
	var etag string
	var err, err1 error

	object := &S3Object {
		S3ObjectPorps: S3ObjectPorps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		Version:	1,
		Size:		size,
		ObjID:		bson.NewObjectId(),
		BucketObjID:	bucket.ObjID,
		BackendID:	bucket.ObjectBID(oname, 1),
		State:		S3StateNone,
	}

	err = dbS3Insert(object)
	if err != nil {
		log.Errorf("s3: Can't insert object %s/%s/%s: %s",
			 bucket.BackendID, object.BackendID, oname, err.Error())
		return nil, err
	}

	err = bucket.dbAddObj(object.Size)
	if err != nil {
		log.Errorf("s3: Can't +account object %s/%s/%s: %s",
			bucket.BackendID, object.BackendID, oname, err.Error())
		goto out_no_size
	}

	etag, err = s3ObjectDataAdd(object.ObjID, bucket.BackendID, object.BackendID, data)
	if err != nil {
		goto out
	}

	err = object.dbSetStateEtag(S3StateActive, etag)
	if err != nil {
		log.Errorf("s3: Can't activate object %s: %s",
				object.BackendID, err.Error())
		goto out
	}

	if bucket.BasicNotify != nil {
		s3Notify(namespace, bucket, object, S3NotifyPut)
	}

	log.Debugf("s3: Inserted object %s/%s", bucket.BackendID, object.BackendID)
	return object, nil

out:
	err1 = bucket.dbDelObj(object.Size)
	if err1 != nil {
		log.Errorf("s3: Can't -account object %s: %s", oname, err1.Error())
	}
out_no_size:
	dbS3Remove(object, bson.M{"_id": object.ObjID})
	return nil, err
}

func s3DeleteObjectFound(bucket *S3Bucket, objectFound *S3Object) error {
	var objdFound *S3ObjectData
	var err error

	err = objectFound.dbSetState(S3StateInactive)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable object %s: %s", objectFound.BackendID, err.Error())
		return err
	}

	objdFound, err = s3ObjectDataFind(objectFound.ObjID)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object data %s: %s",
				objectFound.BackendID, err.Error())
			return err
		}
	} else {
		err = s3ObjectDataDel(objdFound)
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
		log.Errorf("s3: Can't find object %s/%s: %s",
				bucket.Name, oname, err.Error())
		return err
	}

	return s3DeleteObjectFound(bucket, objectFound)
}

func s3ReadObjectData(bucket *S3Bucket, object *S3Object) ([]byte, error) {
	var objd *S3ObjectData
	var res []byte
	var err error

	objd, err = s3ObjectDataFind(object.ObjID)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object data %s: %s",
				object.BackendID, err.Error())
			return nil, err
		}
		return nil, err
	}

	res, err = s3ObjectDataGet(objd)
	if err != nil {
		return nil, err
	}

	log.Debugf("s3: Read object data %s", object.BackendID)
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
				object.BackendID, err.Error())
		return nil, err
	}

	return s3ReadObjectData(bucket, object)
}
