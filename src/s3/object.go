package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"

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

func (object *S3Object)dbRemoveF() (error) {
	err := dbS3Remove(object, bson.M{"_id": object.ObjID})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't force remove %s: %s",
			infoLong(object), err.Error())
	}
	return err
}

func (object *S3Object)dbRemove() (error) {
	err := dbS3RemoveCond(
			bson.M{	"_id": object.ObjID,
				"state": S3StateInactive},
			&S3Object{})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't remove %s: %s",
			infoLong(object), err.Error())
	}
	return err
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
	var objd *S3ObjectData
	var etag string
	var err error

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

	if err = dbS3Insert(object); err != nil {
		return nil, err
	}
	log.Debugf("s3: Inserted %s", infoLong(object))

	err = bucket.dbAddObj(object.Size)
	if err != nil {
		goto out_no_size
	}

	objd, etag, err = s3ObjectDataAdd(object.ObjID, bucket.BackendID,
					object.BackendID, data)
	if err != nil {
		goto out_obj
	}

	err = dbS3SetOnState(object, S3StateActive, nil,
		bson.M{ "state": S3StateActive, "etag": etag })
	if err != nil {
		goto out
	}

	if bucket.BasicNotify != nil {
		s3Notify(namespace, bucket, object, S3NotifyPut)
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out:
	s3ObjectDataDel(objd)
out_obj:
	bucket.dbDelObj(object.Size)
out_no_size:
	object.dbRemoveF()
	return nil, err
}

func s3DeleteObjectFound(bucket *S3Bucket, objectFound *S3Object) error {
	var objdFound *S3ObjectData
	var err error

	err = dbS3SetState(objectFound, S3StateInactive, nil)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	objdFound, err = s3ObjectDataFind(objectFound.ObjID)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object data %s: %s",
				infoLong(objectFound), err.Error())
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
		return err
	}

	err = objectFound.dbRemove()
	if err != nil {
		return err
	}

	log.Debugf("s3: Deleted %s", infoLong(objectFound))
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
		log.Errorf("s3: Can't find object %s on %s: %s",
			oname, infoLong(bucket), err.Error())
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
				infoLong(object), err.Error())
			return nil, err
		}
		return nil, err
	}

	res, err = s3ObjectDataGet(objd)
	if err != nil {
		return nil, err
	}

	log.Debugf("s3: Read %s", infoLong(object))
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
		log.Errorf("s3: Can't find object %s on %s: %s",
				oname, infoLong(bucket), err.Error())
		return nil, err
	}

	return s3ReadObjectData(bucket, object)
}
