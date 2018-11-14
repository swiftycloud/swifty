/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"context"
	"time"

	"swifty/apis/s3"
	"swifty/s3/mgo"
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

func s3RepairObjectInactive(ctx context.Context) error {
	var objects []s3mgo.Object
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

func FindCurObject(ctx context.Context, bucket *s3mgo.Bucket, oname string) (*s3mgo.Object, error) {
	var res s3mgo.Object

	query := bson.M{ "ocookie": bucket.OCookie(oname, 1), "state": S3StateActive }
	err := dbS3FindOneTop(ctx, query, "-rover", &res)
	if err != nil {
		return nil, err
	}

	return &res,nil
}

func Activate(ctx context.Context, b *s3mgo.Bucket, o *s3mgo.Object, etag string) error {
	err := dbS3SetOnState(ctx, o, S3StateActive, nil,
			bson.M{ "state": S3StateActive, "etag": etag, "rover": b.Rover })
	if err == nil {
		err = commitObj(ctx, b, o.Size)
	}

	if err == nil {
		ioSize.Observe(float64(o.Size) / KiB)
		go gcOldVersions(b, o.Key, b.Rover)
	}

	return err
}

func UploadToObject(ctx context.Context, bucket *s3mgo.Bucket, upload *S3Upload) (*s3mgo.Object, error) {
	var err error

	size, etag, err := s3ObjectPartsResum(ctx, upload)
	if err != nil {
		return nil, err
	}

	object := &s3mgo.Object {
		/* We just inherit the objid form another collection
		 * not to update all the data objects
		 */
		ObjID:		upload.ObjID,
		State:		S3StateNone,

		ObjectProps: s3mgo.ObjectProps {
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

	err = acctObj(ctx, bucket, object.Size)
	if err != nil {
		goto out_remove
	}

	err = Activate(ctx, bucket, object, etag)
	if err != nil {
		goto out_acc
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Put > 0 {
		s3Notify(ctx, bucket, object, "put")
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out_acc:
	unacctObj(ctx, bucket, object.Size, true)
out_remove:
	dbS3Remove(ctx, object)
	return nil, err
}
func AddObject(ctx context.Context, bucket *s3mgo.Bucket, oname string,
		acl string, data []byte) (*s3mgo.Object, error) {
	var objp *s3mgo.ObjectPart
	var err error

	object := &s3mgo.Object {
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		ObjectProps: s3mgo.ObjectProps {
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

	err = acctObj(ctx, bucket, object.Size)
	if err != nil {
		goto out_remove
	}

	objp, err = s3ObjectPartAdd(ctx, object.ObjID, bucket.BCookie, object.OCookie, 0, data)
	if err != nil {
		goto out_acc
	}

	err = Activate(ctx, bucket, object, objp.ETag)
	if err != nil {
		goto out
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Put > 0 {
		s3Notify(ctx, bucket, object, "put")
	}

	log.Debugf("s3: Added %s", infoLong(object))
	return object, nil

out:
	s3ObjectPartDelOne(ctx, bucket, object.OCookie, objp)
out_acc:
	unacctObj(ctx, bucket, object.Size, true)
out_remove:
	dbS3Remove(ctx, object)
	return nil, err
}

func s3DeleteObject(ctx context.Context, bucket *s3mgo.Bucket, oname string) error {
	var object *s3mgo.Object
	var err error

	object, err = FindCurObject(ctx, bucket, oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't find object %s on %s: %s",
			oname, infoLong(bucket), err.Error())
		return err
	}

	err = DropObject(ctx, bucket, object)
	if err != nil {
		if err == mgo.ErrNotFound {
			err = nil
		}
		return err
	}

	if bucket.BasicNotify != nil && bucket.BasicNotify.Delete > 0 {
		s3Notify(ctx, bucket, object, "delete")
	}

	log.Debugf("s3: Deleted %s", infoLong(object))
	return nil
}

func DropObject(ctx context.Context, bucket *s3mgo.Bucket, object *s3mgo.Object) error {
	var objp []*s3mgo.ObjectPart

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

	err = unacctObj(ctx, bucket, object.Size, false)
	if err != nil {
		return err
	}

	err = dbS3RemoveOnState(ctx, object, S3StateInactive, nil)
	if err != nil {
		return err
	}

	return nil
}

func ReadData(ctx context.Context, bucket *s3mgo.Bucket, object *s3mgo.Object) ([]byte, error) {
	var objp []*s3mgo.ObjectPart
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

func ReadObject(ctx context.Context, bucket *s3mgo.Bucket, oname string, part, version int) ([]byte, error) {
	var object *s3mgo.Object
	var err error

	object, err = FindCurObject(ctx, bucket, oname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, err
		}
		log.Errorf("s3: Can't find object %s on %s: %s",
				oname, infoLong(bucket), err.Error())
		return nil, err
	}

	return ReadData(ctx, bucket, object)
}
