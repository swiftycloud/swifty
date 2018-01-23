package main

import (
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

func (objd *S3ObjectData)infoLong() (string) {
	return fmt.Sprintf("object-data: %s/%s/%s/%s/%d",
			objd.ObjID, objd.RefID,
			objd.BucketBID, objd.ObjectBID,
			objd.State)
}

func (objd *S3ObjectData)dbSet(state uint32, fields bson.M) (error) {
	var res S3ObjectData

	return dbS3Update(
			bson.M{"_id": objd.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": fields},
			&res)
}

func (objd *S3ObjectData)dbSetState(state uint32) (error) {
	return objd.dbSet(state, bson.M{"state": state})
}

func (objd *S3ObjectData)dbRemove() (error) {
	return dbS3RemoveCond(
			bson.M{	"_id": objd.ObjID,
				"state": S3StateInactive},
		&S3ObjectData{})
}

func s3ObjectDataFind(refID bson.ObjectId) (*S3ObjectData, error) {
	var res S3ObjectData

	err := dbS3FindOne(bson.M{"ref-id": refID, "state": S3StateActive}, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3ObjectDataAdd(refid bson.ObjectId, bucket_bid, object_bid string, data []byte) (string, error) {
	var objd S3ObjectData
	var err error

	objd.ObjID		= bson.NewObjectId()
	objd.RefID		= refid
	objd.BucketBID		= bucket_bid
	objd.ObjectBID		= object_bid
	objd.State		= S3StateNone
	objd.Size		= int64(len(data))
	objd.CreationTime	= time.Now().Format(time.RFC3339)

	if radosDisabled || objd.Size <= S3StorageSizePerObj {
		if objd.Size > S3StorageSizePerObj {
			log.Errorf("s3: Too big object to store %d", objd.Size)
			err = fmt.Errorf("s3: Object is too big")
			return "", err
		}

		objd.Data = data

		err = dbS3Insert(objd)
		if err != nil {
			log.Errorf("s3: Can't insert object data %s/%s/%s: %s",
				objd.RefID, objd.BucketBID, objd.ObjectBID, err.Error())
			return "", err
		}
	} else {
		err = radosWriteObject(objd.BucketBID, objd.ObjectBID, data, 0)
		if err != nil {
			return "", err
		}
	}

	err = objd.dbSetState(S3StateActive)
	if err != nil {
		log.Errorf("s3: Can't activate object data %s/%s/%s: %s",
			objd.RefID, objd.BucketBID, objd.ObjectBID, err.Error())
		goto out
	}

	log.Debugf("s3: Added object data %s/%s/%s", objd.RefID, objd.BucketBID, objd.ObjectBID)
	return fmt.Sprintf("%x", md5.Sum(data)), nil

out:
	err1 := dbS3Remove(objd, bson.M{"_id": objd.ObjID})
	if err1 != nil {
		log.Errorf("s3: Can't remove object data %s/%s/%s",
			objd.RefID, objd.BucketBID, objd.ObjectBID)
	}

	return "", nil
}

func s3ObjectDataDel(objd *S3ObjectData) (error) {
	var err error

	err = objd.dbSetState(S3StateInactive)
	if err != nil {
		log.Errorf("s3: Can't deactivate object data %s/%s/%s",
			objd.RefID, objd.BucketBID, objd.ObjectBID)
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
		log.Errorf("s3: Can't delete object data %s/%s/%s",
			objd.RefID, objd.BucketBID, objd.ObjectBID)
		return err
	}

	log.Debugf("s3: Deleted object data %s/%s/%s",
		objd.RefID, objd.BucketBID, objd.ObjectBID)
	return nil
}

func s3ObjectDataGet(objd *S3ObjectData) ([]byte, error) {
	var res []byte
	var err error

	if objd.Data == nil {
		res, err = radosReadObject(objd.BucketBID, objd.ObjectBID,
						uint64(objd.Size), 0)
		if err != nil {
			log.Errorf("s3: Can't read object data %s/%s/%s",
				objd.RefID, objd.BucketBID, objd.ObjectBID)
			return nil, err
		}

		return res, nil
	}

	return objd.Data, nil
}
