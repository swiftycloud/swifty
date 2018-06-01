package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"crypto/md5"
	"time"
	"fmt"
)

type S3ObjectData struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	RefID				bson.ObjectId	`bson:"ref-id,omitempty"`
	BCookie				string		`bson:"bcookie,omitempty"`
	OCookie				string		`bson:"ocookie,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	Size				int64		`bson:"size"`
	Data				[]byte		`bson:"data,omitempty"`
}

func s3RepairObjectData() error {
	var objds []S3ObjectData
	var err error

	log.Debugf("s3: Running object data consistency test")

	err = dbS3FindAllInactive(&objds)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairObjectData failed: %s", err.Error())
		return err
	}

	for _, objd := range objds {
		var object S3Object
		var bucket S3Bucket

		log.Debugf("s3: Detected stale object data %s", infoLong(&objd))

		query_ref := bson.M{ "_id": objd.RefID }

		err = dbS3FindOne(query_ref, &object)
		if err != nil {
			var part S3UploadPart

			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object on data %s: %s",
					infoLong(&objd), err.Error())
				return err
			}

			err = dbS3FindOne(query_ref, &part)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't find part on data %s: %s",
						infoLong(&objd), err.Error())
					return err
				}
			} else {
				if err = dbS3Remove(&part); err != nil {
					if err != mgo.ErrNotFound {
						log.Errorf("s3: Can't remove part on data %s: %s",
							infoLong(&objd), err.Error())
						return err
					}
				}
				log.Debugf("s3: Removed part on data %s: %s",
					infoLong(&part), infoLong(&objd), err.Error())
			}
		} else {
			query_bucket := bson.M{ "_id": object.BucketObjID }
			err = dbS3FindOne(query_bucket, &bucket)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't find bucket on object %s: %s",
						infoLong(&object), err.Error())
					return err
				}
			} else {
				err = s3DirtifyBucket(bucket.ObjID)
				if err != nil {
					if err != mgo.ErrNotFound {
						log.Errorf("s3: Can't dirtify bucket on object %s: %s",
						infoLong(&bucket), err.Error())
						return err
					}
				}
			}

			if err = dbS3Remove(&object); err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't remove object on data %s: %s",
						infoLong(&objd), err.Error())
					return err
				}
			}
			log.Debugf("s3: Removed object on data %s: %s", infoLong(&objd), err.Error())

		}

		if objd.Data == nil {
			err = radosDeleteObject(objd.BCookie, objd.OCookie)
			if err != nil {
				log.Errorf("s3: %s/%s backend object data may stale",
					objd.BCookie, objd.OCookie)
			}
		}

		err = dbS3Remove(&objd)
		if err != nil {
			log.Debugf("s3: Can't remove object data %s", infoLong(&objd))
			return err
		}

		log.Debugf("s3: Removed stale object data %s", infoLong(&objd))
	}

	log.Debugf("s3: Object data consistency passed")
	return nil
}

func s3DeactivateObjectData(refID bson.ObjectId) error {
	update := bson.M{ "$set": bson.M{ "state": S3StateInactive } }
	query  := bson.M{ "ref-id": refID }

	return dbS3Update(query, update, false, &S3ObjectData{})
}

func s3ObjectDataFind(refID bson.ObjectId) (*S3ObjectData, error) {
	var res S3ObjectData

	err := dbS3FindOne(bson.M{"ref-id": refID, "state": S3StateActive}, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3ObjectDataAdd(iam *S3Iam, refid bson.ObjectId, bucket_bid, object_bid string, data []byte) (*S3ObjectData, string, error) {
	var objd *S3ObjectData
	var err error

	objd = &S3ObjectData {
		ObjID:		bson.NewObjectId(),
		IamObjID:	iam.ObjID,
		State:		S3StateNone,

		RefID:		refid,
		BCookie:	bucket_bid,
		OCookie:	object_bid,
		Size:		int64(len(data)),
		CreationTime:	time.Now().Format(time.RFC3339),
	}

	if err = dbS3Insert(objd); err != nil {
		goto out
	}

	if radosDisabled || objd.Size <= S3StorageSizePerObj {
		if objd.Size > S3StorageSizePerObj {
			log.Errorf("s3: Too big %s", infoLong(objd))
			err = fmt.Errorf("s3: Object is too big")
			goto out
		}

		update := bson.M{ "$set": bson.M{ "data": data }}
		err = dbS3Update(nil, update, true, objd)
		if err != nil {
			goto out
		}
	} else {
		err = radosWriteObject(bucket_bid, object_bid, data, 0)
		if err != nil {
			goto out
		}
	}

	if err = dbS3SetState(objd, S3StateActive, nil); err != nil {
		if objd.Data == nil {
			radosDeleteObject(bucket_bid, object_bid)
		}
		goto out
	}

	log.Debugf("s3: Added %s", infoLong(objd))
	return objd, fmt.Sprintf("%x", md5.Sum(data)), nil

out:
	dbS3Remove(objd)
	return nil, "", nil
}

func s3ObjectDataDel(bucket *S3Bucket, ocookie string, objd *S3ObjectData) (error) {
	var err error

	err = dbS3SetState(objd, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if objd.Data == nil {
		err = radosDeleteObject(bucket.BCookie, ocookie)
		if err != nil {
			return err
		}
	}

	err = dbS3RemoveOnState(objd, S3StateInactive, nil)
	if err != nil {
		return err
	}

	log.Debugf("s3: Deleted %s", infoLong(objd))
	return nil
}

func s3ObjectDataGet(bucket *S3Bucket, ocookie string, objd *S3ObjectData) ([]byte, error) {
	var res []byte
	var err error

	if objd.Data == nil {
		res, err = radosReadObject(bucket.BCookie, ocookie, uint64(objd.Size), 0)
		if err != nil {
			return nil, err
		}

		return res, nil
	}

	return objd.Data, nil
}
