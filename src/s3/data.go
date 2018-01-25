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
	RefID				bson.ObjectId	`bson:"ref-id,omitempty"`
	BucketBID			string		`json:"bucket-bid,omitempty" bson:"bucket-bid,omitempty"`
	ObjectBID			string		`json:"object-bid,omitempty" bson:"object-bid,omitempty"`
	CreationTime			string		`json:"creation-time,omitempty" bson:"creation-time,omitempty"`
	State				uint32		`json:"state" bson:"state"`
	Size				int64		`json:"size" bson:"size"`
	Data				[]byte		`bson:"data,omitempty"`
}

func (objd *S3ObjectData)dbRemoveF() (error) {
	var err error

	err = dbS3Remove(objd, bson.M{"_id": objd.ObjID})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't force remove %s: %s",
			infoLong(objd), err.Error())
	}
	return err
}

func (objd *S3ObjectData)dbRemove() (error) {
	var err error

	err = dbS3RemoveCond(
			bson.M{	"_id": objd.ObjID,
				"state": S3StateInactive},
			&S3ObjectData{})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't remove %s: %s",
			infoLong(objd), err.Error())
	}
	return err
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
		RefID:		refid,
		BucketBID:	bucket_bid,
		ObjectBID:	object_bid,
		State:		S3StateNone,
		Size:		int64(len(data)),
		CreationTime:	time.Now().Format(time.RFC3339),
	}

	if radosDisabled || objd.Size <= S3StorageSizePerObj {
		if objd.Size > S3StorageSizePerObj {
			log.Errorf("s3: Too big %s", infoLong(objd))
			err = fmt.Errorf("s3: Object is too big")
			return nil, "", err
		}

		objd.Data = data

		if err = dbS3Insert(objd); err != nil {
			goto out
		}
	} else {
		err = radosWriteObject(objd.BucketBID, objd.ObjectBID, data, 0)
		if err != nil {
			goto out
		}
	}

	if err = dbS3SetState(objd, S3StateActive, nil); err != nil {
		if objd.Data != nil {
			radosDeleteObject(objd.BucketBID, objd.ObjectBID)
		}
		goto out
	}

	log.Debugf("s3: Added %s", infoLong(objd))
	return objd, fmt.Sprintf("%x", md5.Sum(data)), nil

out:
	objd.dbRemoveF()
	return nil, "", nil
}

func s3ObjectDataDel(objd *S3ObjectData) (error) {
	var err error

	err = dbS3SetState(objd, S3StateInactive, nil)
	if err != nil {
		return err
	}

	if objd.Data != nil {
		err = radosDeleteObject(objd.BucketBID, objd.ObjectBID)
		if err != nil {
			return err
		}
	}

	err = objd.dbRemove()
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
