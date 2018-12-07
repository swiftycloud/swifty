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
	"fmt"

	"swifty/s3/mgo"
	"swifty/apis/s3"
)

type S3Upload struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	UploadID			string		`bson:"uid"`
	Ref				int64		`bson:"ref"`
	Lock				uint32		`bson:"lock"`

	s3mgo.ObjectProps					`bson:",inline"`
}

func s3RepairUploadsInactive(ctx context.Context) error {
	var uploads []S3Upload
	var err error

	log.Debugf("s3: Processing inactive uploads")

	if err = dbS3FindAllInactive(ctx, &uploads); err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairUploadsInactive failed: %s", err.Error())
		return err
	}

	for _, upload := range uploads {
		log.Debugf("s3: Detected stale upload %s", infoLong(&upload))

		update := bson.M{ "$set": bson.M{ "state": S3StateInactive } }
		query := bson.M{ "ref-id": upload.ObjID }
		if err = dbS3Update(ctx, query, update, false, &s3mgo.ObjectPart{}); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't deactivate parts on upload %s: %s",
					infoLong(&upload), err.Error())
				return err
			}
		}

		err = dbS3Remove(ctx, &upload)
		if err != nil {
			log.Debugf("s3: Can't remove upload %s", infoLong(&upload))
			return err
		}

		log.Debugf("s3: Removed stale upload %s", infoLong(&upload))
	}

	return nil
}

func s3RepairPartsInactive(ctx context.Context) error {
	var objp []*s3mgo.ObjectPart
	var err error

	log.Debugf("s3: Processing inactive datas")

	if err = dbS3FindAllInactive(ctx, &objp); err != nil {
		log.Debugf("Found zero inactives: %s", err.Error())
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairPartsInactive failed: %s", err.Error())
		return err
	}

	log.Debugf("Found %d inactives", len(objp))

	for _, od := range objp {
		log.Debugf("s3: Detected stale part %s", infoLong(&od))

		if err = s3DeactivateObjectData(ctx, od.ObjID); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't deactivate data on part %s: %s",
					infoLong(&od), err.Error())
				return err
			}
		}

		err = dbS3Remove(ctx, &od)
		if err != nil {
			log.Debugf("s3: Can't remove part %s", infoLong(&od))
			return err
		}

		log.Debugf("s3: Removed stale part %s", infoLong(&od))
	}

	return nil
}

func s3RepairUpload(ctx context.Context) error {
	var err error

	log.Debugf("s3: Running uploads consistency test")

	if err = s3RepairUploadsInactive(ctx); err != nil {
		return err
	}

	if err = s3RepairPartsInactive(ctx); err != nil {
		return err
	}

	log.Debugf("s3: Uploads consistency passed")
	return nil
}

func (upload *S3Upload)dbLock(ctx context.Context) (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0, "ref": 0 }
	update := bson.M{ "$inc": bson.M{ "lock": 1 } }
	err := dbS3Update(ctx, query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't lock %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbUnlock(ctx context.Context) (error) {
	query := bson.M{ "state": S3StateActive, "lock": 1, "ref": 0 }
	update := bson.M{ "$inc": bson.M{ "lock": -1 } }
	err := dbS3Update(ctx, query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't unclock %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbRefInc(ctx context.Context) (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0 }
	update := bson.M{ "$inc": bson.M{ "ref": 1 } }
	err := dbS3Update(ctx, query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't +ref %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbRefDec(ctx context.Context) (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0 }
	update := bson.M{ "$inc": bson.M{ "ref": -1 } }
	err := dbS3Update(ctx, query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't -ref %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func VerifyUploadUID(bucket *s3mgo.Bucket, oname, uid string) error {
	genuid := bucket.UploadUID(oname)
	if genuid != uid {
		err := fmt.Errorf("uploadId mismatch")
		log.Errorf("s3: uploadId mismatch %s/%s", genuid, uid)
		return err
	}
	return nil
}

func s3UploadRemoveLocked(ctx context.Context, bucket *s3mgo.Bucket, upload *S3Upload, data bool) (error) {
	var objp []*s3mgo.ObjectPart
	var err error

	err = dbS3SetState(ctx, upload, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if data {
		err = dbS3FindAll(ctx, bson.M{"ref-id": upload.ObjID}, &objp)
		if err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find parts %s: %s",
					infoLong(upload), err.Error())
				return err
			}
		} else {
			for _, od := range objp {
				err = DeletePart(ctx, od)
				if err != nil {
					return err
				}
			}
		}
	}

	err = dbS3RemoveOnState(ctx, upload, S3StateInactive, bson.M{ "ref": 0 })
	if err != nil {
		return err
	}

	log.Debugf("s3: Removed %s", infoLong(upload))
	return nil
}

func s3UploadInit(ctx context.Context, bucket *s3mgo.Bucket, oname, acl string) (*S3Upload, error) {
	var err error

	upload := &S3Upload{
		ObjID:		bson.NewObjectId(),
		IamObjID:	ctxIam(ctx).ObjID,
		State:		S3StateActive,

		ObjectProps: s3mgo.ObjectProps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		BucketObjID:	bucket.ObjID,
		UploadID:	bucket.UploadUID(oname),
	}

	if err = dbS3Insert(ctx, upload); err != nil {
		return nil, err
	}

	log.Debugf("s3: Inserted upload %s", upload.UploadID)
	return upload, err
}

func s3UploadPart(ctx context.Context, bucket *s3mgo.Bucket, oname,
			uid string, partno int, data *ChunkReader) (string, error) {
	var objp *s3mgo.ObjectPart
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return "", err
	}

	query := bson.M{"uid": uid, "state": S3StateActive}
	err = dbS3FindOne(ctx, query, &upload)
	if err != nil {
		return "", err
	}

	err = upload.dbRefInc(ctx)
	if err != nil {
		return "", err
	}

	objp, err = AddPart(ctx, upload.ObjID, bucket.BCookie, upload.UCookie(oname, partno), partno, data)
	if err != nil {
		upload.dbRefDec(ctx)
		log.Errorf("s3: Can't store data %s: %s", infoLong(objp), err.Error())
		return "", err
	}

	ioSize.Observe(float64(objp.Size) / KiB)

	upload.dbRefDec(ctx)

	log.Debugf("s3: Inserted %s", infoLong(objp))
	return objp.ETag, nil
}

func s3UploadFini(ctx context.Context, bucket *s3mgo.Bucket, uid string,
			compete *swys3api.S3MpuFiniParts) (*swys3api.S3MpuFini, error) {
	var res swys3api.S3MpuFini
	var object *s3mgo.Object
	var upload S3Upload
	var err error

	query := bson.M{"uid": uid, "state": S3StateActive}
	err = dbS3FindOne(ctx, query, &upload)
	if err != nil {
		return nil, err
	}

	err = upload.dbLock(ctx)
	if err != nil {
		return nil, err
	}

	object, err = UploadToObject(ctx, bucket, &upload)
	if err != nil {
		log.Errorf("s3: Can't insert object on %s: %s",
			infoLong(&upload), err.Error())
		upload.dbUnlock(ctx)
		return nil, err
	}

	err = s3UploadRemoveLocked(ctx, bucket, &upload, false)
	if err != nil {
		// Don't fail here since object is already committed
		log.Errorf("s3: Can't remove %s: %s",
				infoLong(&upload), err.Error())
	}

	res.ETag = object.ETag

	log.Debugf("s3: Complete upload %v", res)
	return &res, nil
}

func s3Uploads(ctx context.Context, bname string) (*swys3api.S3MpuList,  *S3Error) {
	var res swys3api.S3MpuList
	var bucket *s3mgo.Bucket
	var uploads []S3Upload
	var err error

	bucket, err = FindBucket(ctx, bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return nil, &S3Error{ ErrorCode: S3ErrInternalError }
	}

	res.Bucket		= bucket.Name
	res.MaxUploads		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(ctx, bson.M{"bucket-id": bucket.ObjID,
				"state": S3StateActive,
				"lock": 0}, &uploads)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find uploads on %s: %s",
				infoLong(bucket), err.Error())
			return nil, &S3Error{ ErrorCode: S3ErrInternalError }
		}
	} else {
		for _, u := range uploads {
			res.Upload = append(res.Upload,
				swys3api.S3MpuUpload{
					UploadId:	u.UploadID,
					Key:		u.Key,
					Initiated:	u.CreationTime,
				})
		}
	}

	log.Debugf("s3: List upload %v", res)
	return &res, nil
}

func s3UploadList(ctx context.Context, bucket *s3mgo.Bucket, oname, uid string) (*swys3api.S3MpuPartList, error) {
	var res swys3api.S3MpuPartList
	var objp []*s3mgo.ObjectPart
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return nil, err
	}

	err = dbS3FindOne(ctx, bson.M{"uid": uid,
				"state": S3StateActive,
				"lock": 0}, &upload)
	if err != nil {
		return nil, err
	}

	res.Bucket		= bucket.Name
	res.Key			= oname
	res.UploadId		= uid
	res.StorageClass	= swys3api.S3StorageClassStandard
	res.MaxParts		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(ctx, bson.M{"ref-id": upload.ObjID,
				"state": S3StateActive}, &objp)
	if err != nil {
		if err == mgo.ErrNotFound {
			goto out
		}
		log.Errorf("s3: Can't find parts %s: %s",
			infoLong(&upload), err.Error())
		return nil, err
	} else {
		for _, od := range objp {
			res.Part = append(res.Part,
				swys3api.S3MpuPart{
					PartNumber:	int(od.Part),
					LastModified:	od.CreationTime,
					ETag:		od.ETag,
					Size:		od.Size,
				})
		}
	}

out:
	log.Debugf("s3: List upload %v", res)
	return &res, nil
}

func s3UploadAbort(ctx context.Context, bucket *s3mgo.Bucket, oname, uid string) error {
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return err
	}

	err = dbS3FindOne(ctx, bson.M{"uid": uid}, &upload)
	if err != nil {
		return nil
	}

	err = upload.dbLock(ctx)
	if err != nil {
		return err
	}

	err = s3UploadRemoveLocked(ctx, bucket, &upload, true)
	if err != nil {
		upload.dbUnlock(ctx)
		return err
	}

	log.Debugf("s3: Aborted %s", infoLong(&upload))
	return nil
}
