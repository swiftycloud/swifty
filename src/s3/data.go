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
	MTime				int64		`json:"mtime,omitempty" bson:"mtime,omitempty"`
	State				uint32		`json:"state" bson:"state"`

	RefID				bson.ObjectId	`bson:"ref-id,omitempty"`
	BackendID			string		`json:"bid" bson:"bid"`
	BucketBID			string		`json:"bucket-bid,omitempty" bson:"bucket-bid,omitempty"`
	ObjectBID			string		`json:"object-bid,omitempty" bson:"object-bid,omitempty"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`
	Size				int64		`json:"size" bson:"size"`
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
		var part S3UploadPart

		log.Debugf("s3: Detected stale object data %s", infoLong(&objd))

		update := bson.M{ "$set": bson.M{ "state": S3StateInactive } }
		query := bson.M{ "_id": objd.RefID }

		err = dbS3FindOne(query, &object)
		if err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object on data %s: %s",
					infoLong(&objd), err.Error())
				return err
			}

			err = dbS3FindOne(query, &part)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't find part on data %s: %s",
						infoLong(&objd), err.Error())
					return err
				}
			} else {
				if err = dbS3Update(nil, update, true, &part); err != nil {
					log.Errorf("s3: Can't deactivate part on data %s: %s",
						infoLong(&part), infoLong(&objd), err.Error())
					return err
				}

				log.Debugf("s3: Deactivated part on data %s: %s",
					infoLong(&part), infoLong(&objd), err.Error())
			}
		} else {
			if err = dbS3Update(nil, update, true, &object); err != nil {
				log.Errorf("s3: Can't deactivate object on data %s: %s",
					infoLong(&object), infoLong(&objd), err.Error())
				return err
			}

			log.Debugf("s3: Deactivated object on data %s: %s",
				infoLong(&object), infoLong(&objd), err.Error())
		}

		if objd.Data == nil {
			err = radosDeleteObject(objd.BucketBID, objd.ObjectBID)
			if err != nil {
				log.Errorf("s3: %s/%s backend object data may stale",
					objd.BucketBID, objd.ObjectBID)
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

func s3ObjectDataFind(refID bson.ObjectId) (*S3ObjectData, error) {
	var res S3ObjectData

	err := dbS3FindOne(bson.M{"ref-id": refID, "state": S3StateActive}, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3ObjectDataAdd(refid bson.ObjectId, bucket_bid, object_bid string, data []byte) (*S3ObjectData, string, error) {
	var objd *S3ObjectData
	var err error

	objd = &S3ObjectData {
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		RefID:		refid,
		BackendID:	ObjdBID(bucket_bid, object_bid),
		BucketBID:	bucket_bid,
		ObjectBID:	object_bid,
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
		err = radosWriteObject(objd.BucketBID, objd.ObjectBID, data, 0)
		if err != nil {
			goto out
		}
	}

	if err = dbS3SetState(objd, S3StateActive, nil); err != nil {
		if objd.Data == nil {
			radosDeleteObject(objd.BucketBID, objd.ObjectBID)
		}
		goto out
	}

	log.Debugf("s3: Added %s", infoLong(objd))
	return objd, fmt.Sprintf("%x", md5.Sum(data)), nil

out:
	dbS3Remove(objd)
	return nil, "", nil
}

func s3ObjectDataDel(objd *S3ObjectData) (error) {
	var err error

	err = dbS3SetState(objd, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if objd.Data == nil {
		err = radosDeleteObject(objd.BucketBID, objd.ObjectBID)
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

func s3ObjectDataGet(objd *S3ObjectData) ([]byte, error) {
	var res []byte
	var err error

	if objd.Data == nil {
		res, err = radosReadObject(objd.BucketBID, objd.ObjectBID,
						uint64(objd.Size), 0)
		if err != nil {
			return nil, err
		}

		return res, nil
	}

	return objd.Data, nil
}
