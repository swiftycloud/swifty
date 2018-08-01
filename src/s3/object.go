package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"context"
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
	Rover				int64		`bson:"rover"`
	Size				int64		`bson:"size"`
	ETag				string		`bson:"etag"`

	S3ObjectPorps					`bson:",inline"`
}

func s3RepairObjectInactive(ctx context.Context) error {
	var objects []S3Object
	var err error

	log.Debugf("s3: Processing inactive objects")

	err = dbS3FindAllInactive(ctx, &objects)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairObjectInactive failed: %s", err.Error())
		return err
	}

	for _, object := range objects {
		log.Debugf("s3: Detected stale object %s", infoLong(&object))

		if err = s3DeactivateObjectData(ctx, object.ObjID); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object data %s: %s",
					infoLong(object), err.Error())
				return err
			}
		}

		err = dbS3Remove(ctx, &object)
		if err != nil {
			log.Errorf("s3: Can't delete object %s: %s",
				infoLong(&object), err.Error())
			return err
		}

		log.Debugf("s3: Removed stale object %s", infoLong(&object))
	}

	return nil
}

func s3RepairObject(ctx context.Context) error {
	var err error

	log.Debugf("s3: Running objects consistency test")

	if err = s3RepairObjectInactive(ctx); err != nil {
		return err
	}

	log.Debugf("s3: Objects consistency passed")
	return nil
}

func (bucket *S3Bucket)FindCurObject(ctx context.Context, oname string) (*S3Object, error) {
	var res S3Object

	query := bson.M{ "ocookie": bucket.OCookie(oname, 1), "state": S3StateActive }
	err := dbS3FindOneTop(ctx, query, "-rover", &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func (o *S3Object)Activate(ctx context.Context, b *S3Bucket, etag string) error {
	err := dbS3SetOnState(ctx, o, S3StateActive, nil,
			bson.M{ "state": S3StateActive, "etag": etag, "rover": b.Rover })
	if err == nil {
		err = b.dbCmtObj(ctx, o.Size, -1)
	}

	if err == nil {
		ioSize.Observe(float64(o.Size) / KiB)
		go gcOldVersions(b, o.Key, b.Rover)
	}

	return err
}

func (bucket *S3Bucket)ToObject(ctx context.Context, iam *S3Iam, upload *S3Upload) (*S3Object, error) {
	var err error

	size, etag, err := s3ObjectPartsResum(ctx, upload)
	if err != nil {
		return nil, err
	}

	object := &S3Object {
		/* We just inherit the objid form another collection
		 * not to update all the data objects
		 */
		ObjID:		upload.ObjID,
		IamObjID:	iam.ObjID,
		State:		S3StateNone,

		S3ObjectPorps: S3ObjectPorps {
			Key:		upload.Key,
			Acl:		upload.Acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		Version:	1,
		Size:		size,
		BucketObjID:	bucket.ObjID,
		OCookie:	bucket.OCookie(upload.Key, 1),
	}

	if err = dbS3Insert(ctx, object); err != nil {
		return nil, err
	}
	log.Debugf("s3: Converted %s", infoLong(object))

	err = bucket.dbAddObj(ctx, object.Size, 1)
	if err != nil {
		goto out_remove
	}

	err = object.Activate(ctx, bucket, etag)
	if err != nil {
		goto out_remove
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Put > 0 {
		s3Notify(ctx, iam, bucket, object, "put")
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out_remove:
	dbS3Remove(ctx, object)
	return nil, err
}
func (bucket *S3Bucket)AddObject(ctx context.Context, iam *S3Iam, oname string,
		acl string, data []byte) (*S3Object, error) {
	var objp *S3ObjectPart
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
		Size:		int64(len(data)),
		BucketObjID:	bucket.ObjID,
		OCookie:	bucket.OCookie(oname, 1),
	}

	if err = dbS3Insert(ctx, object); err != nil {
		return nil, err
	}
	log.Debugf("s3: Inserted %s", infoLong(object))

	err = bucket.dbAddObj(ctx, object.Size, 1)
	if err != nil {
		goto out_remove
	}

	objp, err = s3ObjectPartAdd(ctx, iam, object.ObjID, bucket.BCookie, object.OCookie, 0, data)
	if err != nil {
		goto out_acc
	}

	err = object.Activate(ctx, bucket, objp.ETag)
	if err != nil {
		goto out
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Put > 0 {
		s3Notify(ctx, iam, bucket, object, "put")
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out:
	s3ObjectPartDelOne(ctx, bucket, object.OCookie, objp)
out_acc:
	bucket.dbDelObj(ctx, object.Size, -1)
out_remove:
	dbS3Remove(ctx, object)
	return nil, err
}

func s3DeleteObject(ctx context.Context, iam *S3Iam, bucket *S3Bucket, oname string) error {
	var object *S3Object
	var err error

	object, err = bucket.FindCurObject(ctx, oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s on %s: %s",
			oname, infoLong(bucket), err.Error())
		return err
	}

	err = bucket.DropObject(ctx, object)
	if err != nil {
		if err == mgo.ErrNotFound {
			err = nil
		}
		return err
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Delete > 0 {
		s3Notify(ctx, iam, bucket, object, "delete")
	}

	log.Debugf("s3: Deleted %s", infoLong(object))
	return nil
}

func (bucket *S3Bucket)DropObject(ctx context.Context, object *S3Object) error {
	var objp []*S3ObjectPart

	err := dbS3SetState(ctx, object, S3StateInactive, nil)
	if err != nil {
		return err
	}

	objp, err = s3ObjectPartFind(ctx, object.ObjID)
	if err != nil {
		log.Errorf("s3: Can't find object data %s: %s",
			infoLong(object), err.Error())
		return err
	}

	err = s3ObjectPartDel(ctx, bucket, object.OCookie, objp)
	if err != nil {
		return err
	}

	err = bucket.dbDelObj(ctx, object.Size, 0)
	if err != nil {
		return err
	}

	err = dbS3RemoveOnState(ctx, object, S3StateInactive, nil)
	if err != nil {
		return err
	}

	return nil
}

func (object *S3Object)ReadData(ctx context.Context, bucket *S3Bucket) ([]byte, error) {
	var objp []*S3ObjectPart
	var res []byte
	var err error

	objp, err = s3ObjectPartFindFull(ctx, object.ObjID)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object data %s: %s",
				infoLong(object), err.Error())
			return nil, err
		}
		return nil, err
	}

	/* FIXME -- push io.Writer and write data into it, do not carry bytes over */
	res, err = s3ObjectPartRead(ctx, bucket, object.OCookie, objp)
	if err != nil {
		return nil, err
	}

	log.Debugf("s3: Read %s", infoLong(object))
	return res, err
}

func (bucket *S3Bucket)ReadObject(ctx context.Context, oname string, part, version int) ([]byte, error) {
	var object *S3Object
	var err error

	object, err = bucket.FindCurObject(ctx, oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find object %s on %s: %s",
				oname, infoLong(bucket), err.Error())
		return nil, err
	}

	return object.ReadData(ctx, bucket)
}
