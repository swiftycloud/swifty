package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"fmt"
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
	CreationTime			string		`bson:"creation-time,omitempty"`
	Acl				string		`bson:"acl,omitempty"`
	Key				string		`bson:"key"`

	// Todo
	Meta				[]S3Tag		`bson:"meta,omitempty"`
	TagSet				[]S3Tag		`bson:"tags,omitempty"`
	Policy				string		`bson:"policy,omitempty"`

	// Not supported props
	// torrent
	// objects archiving
}

type S3Object struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`
	OCookie				string		`bson:"ocookie"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	Version				int		`bson:"version"`
	Size				int64		`bson:"size"`
	ETag				string		`bson:"etag"`

	S3ObjectPorps					`bson:",inline"`
}

func s3RepairObjectInactive() error {
	var objects []S3Object
	var err error

	log.Debugf("s3: Processing inactive objects")

	err = dbS3FindAllInactive(&objects)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairObjectInactive failed: %s", err.Error())
		return err
	}

	for _, object := range objects {
		log.Debugf("s3: Detected stale object %s", infoLong(&object))

		if err = s3DeactivateObjectData(object.ObjID); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object data %s: %s",
					infoLong(object), err.Error())
				return err
			}
		}

		err = dbS3Remove(&object)
		if err != nil {
			log.Errorf("s3: Can't delete object %s: %s",
				infoLong(&object), err.Error())
			return err
		}

		log.Debugf("s3: Removed stale object %s", infoLong(&object))
	}

	return nil
}

func s3RepairObject() error {
	var err error

	log.Debugf("s3: Running objects consistency test")

	if err = s3RepairObjectInactive(); err != nil {
		return err
	}

	log.Debugf("s3: Objects consistency passed")
	return nil
}

func (bucket *S3Bucket)FindObject(oname string) (*S3Object, error) {
	var res S3Object

	query := bson.M{ "ocookie": bucket.OCookie(oname, 1), "state": S3StateActive }
	err := dbS3FindOne(query, &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func s3AddObject(iam *S3Iam, bucket *S3Bucket, oname string,
		acl string, size int64, data []byte) (*S3Object, error) {
	var objd *S3ObjectData
	var err error

	object := &S3Object {
		ObjID:		bson.NewObjectId(),
		IamObjID:	iam.ObjID,
		State:		S3StateNone,

		S3ObjectPorps: S3ObjectPorps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		Version:	1,
		Size:		size,
		BucketObjID:	bucket.ObjID,
		OCookie:	bucket.OCookie(oname, 1),
	}

	if err = dbS3Insert(object); err != nil {
		return nil, err
	}
	log.Debugf("s3: Inserted %s", infoLong(object))

	err = bucket.dbAddObj(object.Size, 1)
	if err != nil {
		goto out_remove
	}

	objd, err = s3ObjectDataAdd(iam, object.ObjID, bucket.BCookie,
					object.OCookie, data)
	if err != nil {
		goto out_acc
	}

	err = dbS3SetOnState(object, S3StateActive, nil,
		bson.M{ "state": S3StateActive, "etag": fmt.Sprintf("%x", objd.ETag) })
	if err != nil {
		goto out
	}

	bucket.dbCmtObj(object.Size, -1)
	if err != nil {
		goto out
	}

	ioSize.Observe(float64(object.Size) / KiB)

	if bucket.BasicNotify != nil && bucket.BasicNotify.Put > 0 {
		s3Notify(iam, bucket, object, "put")
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out:
	s3ObjectDataDel(bucket, object.OCookie, objd)
out_acc:
	bucket.dbDelObj(object.Size, -1)
out_remove:
	dbS3Remove(object)
	return nil, err
}

func s3DeleteObject(iam *S3Iam, bucket *S3Bucket, oname string) error {
	var object *S3Object
	var objd *S3ObjectData
	var err error

	object, err = bucket.FindObject(oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s on %s: %s",
			oname, infoLong(bucket), err.Error())
		return err
	}

	err = dbS3SetState(object, S3StateInactive, nil)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	objd, err = s3ObjectDataFind(object.ObjID)
	if err != nil {
		log.Errorf("s3: Can't find object data %s: %s",
			infoLong(object), err.Error())
		return err
	}

	err = s3ObjectDataDel(bucket, object.OCookie, objd)
	if err != nil {
		return err
	}

	err = bucket.dbDelObj(object.Size, 0)
	if err != nil {
		return err
	}

	err = dbS3RemoveOnState(object, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Delete > 0 {
		s3Notify(iam, bucket, object, "delete")
	}

	log.Debugf("s3: Deleted %s", infoLong(object))
	return nil
}

func s3ReadObjectData(bucket *S3Bucket, object *S3Object) ([]byte, error) {
	var objd *S3ObjectData
	var res []byte
	var err error

	objd, err = s3ObjectDataFindFull(object.ObjID)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object data %s: %s",
				infoLong(object), err.Error())
			return nil, err
		}
		return nil, err
	}

	res, err = s3ObjectDataGet(bucket, object.OCookie, objd)
	if err != nil {
		return nil, err
	}

	log.Debugf("s3: Read %s", infoLong(object))
	return res, err
}

func s3ReadObject(bucket *S3Bucket, oname string, part, version int) ([]byte, error) {
	var object *S3Object
	var err error

	object, err = bucket.FindObject(oname)
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
