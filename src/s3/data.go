package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"crypto/md5"
	"time"
	"fmt"
)

type S3ObjectPart struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`

	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	RefID				bson.ObjectId	`bson:"ref-id,omitempty"`
	BCookie				string		`bson:"bcookie,omitempty"`
	OCookie				string		`bson:"ocookie,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	Size				int64		`bson:"size"`
	Part				uint		`bson:"part"`
	ETag				string		`bson:"etag"`
	Data				[]byte		`bson:"data,omitempty"`
}

func s3RepairObjectData() error {
	var objps []S3ObjectPart
	var err error

	log.Debugf("s3: Running object data consistency test")

	err = dbS3FindAllInactive(&objps)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairObjectData failed: %s", err.Error())
		return err
	}

	for _, objp := range objps {
		var object S3Object
		var bucket S3Bucket

		log.Debugf("s3: Detected stale object data %s", infoLong(&objp))

		query_ref := bson.M{ "_id": objp.RefID }

		err = dbS3FindOne(query_ref, &object)
		if err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object on data %s: %s",
					infoLong(&objp), err.Error())
				return err
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
						infoLong(&objp), err.Error())
					return err
				}
			}
			log.Debugf("s3: Removed object on data %s: %s", infoLong(&objp), err.Error())

		}

		if objp.Data == nil {
			err = radosDeleteObject(objp.BCookie, objp.OCookie)
			if err != nil {
				log.Errorf("s3: %s/%s backend object data may stale",
					objp.BCookie, objp.OCookie)
			}
		}

		err = dbS3Remove(&objp)
		if err != nil {
			log.Debugf("s3: Can't remove object data %s", infoLong(&objp))
			return err
		}

		log.Debugf("s3: Removed stale object data %s", infoLong(&objp))
	}

	log.Debugf("s3: Object data consistency passed")
	return nil
}

func s3DeactivateObjectData(refID bson.ObjectId) error {
	update := bson.M{ "$set": bson.M{ "state": S3StateInactive } }
	query  := bson.M{ "ref-id": refID }

	return dbS3Update(query, update, false, &S3ObjectPart{})
}

func s3ObjectPartFind(refID bson.ObjectId) ([]*S3ObjectPart, error) {
	var res []*S3ObjectPart

	err := dbS3FindAllFields(bson.M{"ref-id": refID, "state": S3StateActive}, bson.M{"data": 0}, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3ObjectPartFindFull(refID bson.ObjectId) ([]*S3ObjectPart, error) {
	var res []*S3ObjectPart

	err := dbS3FindAllSorted(bson.M{"ref-id": refID, "state": S3StateActive}, "part",  &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3ObjectPartAdd(iam *S3Iam, refid bson.ObjectId, bucket_bid, object_bid string, part int, data []byte) (*S3ObjectPart, error) {
	var objp *S3ObjectPart
	var err error

	objp = &S3ObjectPart {
		ObjID:		bson.NewObjectId(),
		IamObjID:	iam.ObjID,
		State:		S3StateNone,

		RefID:		refid,
		BCookie:	bucket_bid,
		OCookie:	object_bid,
		Size:		int64(len(data)),
		Part:		uint(part),
		ETag:		fmt.Sprintf("%x", md5.Sum(data)),
		CreationTime:	time.Now().Format(time.RFC3339),
	}

	if err = dbS3Insert(objp); err != nil {
		goto out
	}

	if radosDisabled || objp.Size <= S3StorageSizePerObj {
		if objp.Size > S3StorageSizePerObj {
			log.Errorf("s3: Too big %s", infoLong(objp))
			err = fmt.Errorf("s3: Object is too big")
			goto out
		}

		update := bson.M{ "$set": bson.M{ "data": data }}
		err = dbS3Update(nil, update, true, objp)
		if err != nil {
			goto out
		}
	} else {
		err = radosWriteObject(bucket_bid, object_bid, data, 0)
		if err != nil {
			goto out
		}
	}

	if err = dbS3SetState(objp, S3StateActive, nil); err != nil {
		if objp.Data == nil {
			radosDeleteObject(bucket_bid, object_bid)
		}
		goto out
	}

	log.Debugf("s3: Added %s", infoLong(objp))
	return objp, nil

out:
	dbS3Remove(objp)
	return nil, err
}

func s3ObjectPartDel(bucket *S3Bucket, ocookie string, objp []*S3ObjectPart) (error) {
	for _, od := range objp {
		err := s3ObjectPartDelOne(bucket, ocookie, od)
		if err != nil {
			return err
		}
	}

	return nil
}

func s3ObjectPartDelOne(bucket *S3Bucket, ocookie string, objp *S3ObjectPart) (error) {
	var err error

	err = dbS3SetState(objp, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if objp.Data == nil {
		err = radosDeleteObject(bucket.BCookie, ocookie)
		if err != nil {
			return err
		}
	}

	err = dbS3RemoveOnState(objp, S3StateInactive, nil)
	if err != nil {
		return err
	}

	log.Debugf("s3: Deleted %s", infoLong(objp))
	return nil
}

func s3ObjectPartGet(bucket *S3Bucket, ocookie string, objp []*S3ObjectPart) ([]byte, error) {
	var res []byte

	for _, od := range objp {
		x, err := s3ObjectPartGetOne(bucket, ocookie, od)
		if err != nil {
			return nil, err
		}

		res = append(res, x...)
	}

	return res, nil
}

func s3ObjectPartGetOne(bucket *S3Bucket, ocookie string, objp *S3ObjectPart) ([]byte, error) {
	var res []byte
	var err error

	if objp.Data == nil {
		res, err = radosReadObject(bucket.BCookie, ocookie, uint64(objp.Size), 0)
		if err != nil {
			return nil, err
		}

		return res, nil
	}

	return objp.Data, nil
}

func s3ObjectPartsResum(upload *S3Upload) (int64, string, error) {
	var objp *S3ObjectPart
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var size int64

	hasher := md5.New()

	pipe = dbS3Pipe(objp,
		[]bson.M{{"$match": bson.M{"ref-id": upload.ObjID}},
			{"$sort": bson.M{"part": 1} }})
	iter = pipe.Iter()
	for iter.Next(&objp) {
		if objp.State != S3StateActive {
			continue
		}
		if objp.Data == nil {
			/* XXX Too bad :( */
			return 0, "", fmt.Errorf("Can't finish upload")
		}

		hasher.Write(objp.Data)
		size += objp.Size
	}
	if err := iter.Close(); err != nil {
		log.Errorf("s3: Can't close iter on %s: %s",
			infoLong(&upload), err.Error())
		return 0, "", err
	}

	return size, fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
