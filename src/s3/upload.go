package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"time"
	"fmt"

	"../apis/apps/s3"
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

	S3ObjectPorps					`bson:",inline"`
}

func s3RepairUploadsInactive() error {
	var uploads []S3Upload
	var err error

	log.Debugf("s3: Processing inactive uploads")

	if err = dbS3FindAllInactive(&uploads); err != nil {
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
		if err = dbS3Update(query, update, false, &S3ObjectData{}); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't deactivate parts on upload %s: %s",
					infoLong(&upload), err.Error())
				return err
			}
		}

		err = dbS3Remove(&upload)
		if err != nil {
			log.Debugf("s3: Can't remove upload %s", infoLong(&upload))
			return err
		}

		log.Debugf("s3: Removed stale upload %s", infoLong(&upload))
	}

	return nil
}

func s3RepairPartsInactive() error {
	var objd []*S3ObjectData
	var err error

	log.Debugf("s3: Processing inactive datas")

	if err = dbS3FindAllInactive(&objd); err != nil {
		log.Debugf("Found zero inactives: %s", err.Error())
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairPartsInactive failed: %s", err.Error())
		return err
	}

	log.Debugf("Found %d inactives", len(objd))

	for _, od := range objd {
		log.Debugf("s3: Detected stale part %s", infoLong(&od))

		if err = s3DeactivateObjectData(od.ObjID); err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't deactivate data on part %s: %s",
					infoLong(&od), err.Error())
				return err
			}
		}

		err = dbS3Remove(&od)
		if err != nil {
			log.Debugf("s3: Can't remove part %s", infoLong(&od))
			return err
		}

		log.Debugf("s3: Removed stale part %s", infoLong(&od))
	}

	return nil
}

func s3RepairUpload() error {
	var err error

	log.Debugf("s3: Running uploads consistency test")

	if err = s3RepairUploadsInactive(); err != nil {
		return err
	}

	if err = s3RepairPartsInactive(); err != nil {
		return err
	}

	log.Debugf("s3: Uploads consistency passed")
	return nil
}

func (upload *S3Upload)dbLock() (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0, "ref": 0 }
	update := bson.M{ "$inc": bson.M{ "lock": 1 } }
	err := dbS3Update(query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't lock %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbUnlock() (error) {
	query := bson.M{ "state": S3StateActive, "lock": 1, "ref": 0 }
	update := bson.M{ "$inc": bson.M{ "lock": -1 } }
	err := dbS3Update(query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't unclock %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbRefInc() (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0 }
	update := bson.M{ "$inc": bson.M{ "ref": 1 } }
	err := dbS3Update(query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't +ref %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func (upload *S3Upload)dbRefDec() (error) {
	query := bson.M{ "state": S3StateActive, "lock": 0 }
	update := bson.M{ "$inc": bson.M{ "ref": -1 } }
	err := dbS3Update(query, update, true, upload)
	if err != nil {
		log.Errorf("s3: Can't -ref %s: %s",
			infoLong(upload), err.Error())
	}
	return err
}

func VerifyUploadUID(bucket *S3Bucket, oname, uid string) error {
	genuid := bucket.UploadUID(oname)
	if genuid != uid {
		err := fmt.Errorf("uploadId mismatch")
		log.Errorf("s3: uploadId mismatch %s/%s", genuid, uid)
		return err
	}
	return nil
}

func s3UploadRemoveLocked(bucket *S3Bucket, upload *S3Upload) (error) {
	var objd []*S3ObjectData
	var err error

	err = dbS3SetState(upload, S3StateInactive, nil)
	if err != nil {
		return err
	}

	err = dbS3FindAll(bson.M{"ref-id": upload.ObjID}, &objd)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find parts %s: %s",
				infoLong(upload), err.Error())
			return err
		}
	} else {
		for _, od := range objd {
			err = s3ObjectDataDelOne(bucket, od.OCookie, od)
			if err != nil {
				return err
			}
		}
	}

	err = dbS3RemoveOnState(upload, S3StateInactive, bson.M{ "ref": 0 })
	if err != nil {
		return err
	}

	log.Debugf("s3: Removed %s", infoLong(upload))
	return nil
}

func s3UploadInit(iam *S3Iam, bucket *S3Bucket, oname, acl string) (*S3Upload, error) {
	var err error

	upload := &S3Upload{
		ObjID:		bson.NewObjectId(),
		IamObjID:	iam.ObjID,
		State:		S3StateActive,

		S3ObjectPorps: S3ObjectPorps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		BucketObjID:	bucket.ObjID,
		UploadID:	bucket.UploadUID(oname),
	}

	if err = dbS3Insert(upload); err != nil {
		return nil, err
	}

	log.Debugf("s3: Inserted upload %s", upload.UploadID)
	return upload, err
}

func s3UploadPart(iam *S3Iam, bucket *S3Bucket, oname,
			uid string, partno int, data []byte) (string, error) {
	var objd *S3ObjectData
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return "", err
	}

	query := bson.M{"uid": uid, "state": S3StateActive}
	err = dbS3FindOne(query, &upload)
	if err != nil {
		return "", err
	}

	err = upload.dbRefInc()
	if err != nil {
		return "", err
	}

	objd, err = s3ObjectDataAdd(iam, upload.ObjID, bucket.BCookie, upload.UCookie(oname, partno), partno, data)
	if err != nil {
		upload.dbRefDec()
		log.Errorf("s3: Can't store data %s: %s", infoLong(objd), err.Error())
		return "", err
	}

	ioSize.Observe(float64(objd.Size) / KiB)

	upload.dbRefDec()

	log.Debugf("s3: Inserted %s", infoLong(objd))
	return fmt.Sprintf("%x", objd.ETag), nil
}

func s3UploadFini(iam *S3Iam, bucket *S3Bucket, uid string,
			compete *swys3api.S3MpuFiniParts) (*swys3api.S3MpuFini, error) {
	var res swys3api.S3MpuFini
	var objd *S3ObjectData
	var object *S3Object
	var upload S3Upload
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var data []byte
	var err error

	query := bson.M{"uid": uid, "state": S3StateActive}
	err = dbS3FindOne(query, &upload)
	if err != nil {
		return nil, err
	}

	err = upload.dbLock()
	if err != nil {
		return nil, err
	}

	/* FIXME -- migrate data, not read and write back */
	pipe = dbS3Pipe(objd,
		[]bson.M{{"$match": bson.M{"ref-id": upload.ObjID}},
			{"$sort": bson.M{"part": 1} }})
	iter = pipe.Iter()
	for iter.Next(&objd) {
		if objd.State != S3StateActive { continue }
		data = append(data, objd.Data ...)
	}
	if err = iter.Close(); err != nil {
		log.Errorf("s3: Can't close iter on %s: %s",
			infoLong(&upload), err.Error())
		upload.dbUnlock()
		return nil, err
	}

	object, err = s3AddObject(iam, bucket, upload.Key, upload.Acl, data)
	if err != nil {
		log.Errorf("s3: Can't insert object on %s: %s",
			infoLong(&upload), err.Error())
		upload.dbUnlock()
		return nil, err
	}

	err = s3UploadRemoveLocked(bucket, &upload)
	if err != nil {
		// Don't fail here since object is already committed
		log.Errorf("s3: Can't remove %s: %s",
				infoLong(&upload), err.Error())
	}

	res.ETag = object.ETag

	log.Debugf("s3: Complete upload %v", res)
	return &res, nil
}

func s3Uploads(iam *S3Iam, bname string) (*swys3api.S3MpuList,  *S3Error) {
	var res swys3api.S3MpuList
	var bucket *S3Bucket
	var uploads []S3Upload
	var err error

	bucket, err = iam.FindBucket(bname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil, &S3Error{ ErrorCode: S3ErrNoSuchBucket }
		}
		return nil, &S3Error{ ErrorCode: S3ErrInternalError }
	}

	res.Bucket		= bucket.Name
	res.MaxUploads		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(bson.M{"bucket-id": bucket.ObjID,
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

func s3UploadList(bucket *S3Bucket, oname, uid string) (*swys3api.S3MpuPartList, error) {
	var res swys3api.S3MpuPartList
	var objd []*S3ObjectData
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return nil, err
	}

	err = dbS3FindOne(bson.M{"uid": uid,
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

	err = dbS3FindAll(bson.M{"ref-id": upload.ObjID,
				"state": S3StateActive}, &objd)
	if err != nil {
		if err == mgo.ErrNotFound {
			goto out
		}
		log.Errorf("s3: Can't find parts %s: %s",
			infoLong(&upload), err.Error())
		return nil, err
	} else {
		for _, od := range objd {
			res.Part = append(res.Part,
				swys3api.S3MpuPart{
					PartNumber:	int(od.Part),
					LastModified:	od.CreationTime,
					ETag:		fmt.Sprintf("%x", od.ETag),
					Size:		od.Size,
				})
		}
	}

out:
	log.Debugf("s3: List upload %v", res)
	return &res, nil
}

func s3UploadAbort(bucket *S3Bucket, oname, uid string) error {
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return err
	}

	err = dbS3FindOne(bson.M{"uid": uid}, &upload)
	if err != nil {
		return nil
	}

	err = upload.dbLock()
	if err != nil {
		return err
	}

	err = s3UploadRemoveLocked(bucket, &upload)
	if err != nil {
		upload.dbUnlock()
		return err
	}

	log.Debugf("s3: Aborted %s", infoLong(&upload))
	return nil
}
