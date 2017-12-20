package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"crypto/sha256"
	"encoding/hex"
	"time"
	"fmt"
)

type S3Upload struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	UploadID			string		`json:"uid" bson:"uid"`
	State				uint32		`json:"state" bson:"state"`

	S3ObjectPorps					`json:",inline" bson:",inline"`
}

func (upload *S3Upload)dbSetState(state uint32) (error) {
	var res S3Upload

	return dbS3Update(
			bson.M{"_id": upload.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"state": state},
			&res,
		)
}

// FIXME What to do if one start uploadin parts and
// same time create complete file with same name?
//
// Amazon says they are merging all parts into one
// result file so it could be read then back (and I
// suspect they are simply increment versioning or
// overwrite old files). We don't plan to merge
// the chunks instead and carry them as is.
func UploadUID(salt, key string, part, version int) string {
	h := sha256.New()
	h.Write([]byte(salt + "-" + fmt.Sprintf("%d-%d", part, version) + "-" + key))
	return hex.EncodeToString(h.Sum(nil))
}

func VerifyUploadUID(bucket *S3Bucket, object_name, upload_id string) error {
	uid := UploadUID(bucket.BackendID, object_name, 0, 0)
	if uid != upload_id {
		err := fmt.Errorf("uploadId mismatch")
		log.Errorf("s3: uploadId mismatch %s/%s", uid, upload_id)
		return err
	}
	return nil
}

func s3UploadInit(bucket *S3Bucket, object_name, acl string) (*S3Upload, error) {
	var err error

	upload := S3Upload{
		S3ObjectPorps: S3ObjectPorps {
			Name:		object_name,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		BucketObjID:	bucket.ObjID,
		UploadID:	UploadUID(bucket.BackendID, object_name, 0, 0),
		State:		S3StateActive,
	}

	err = dbS3Insert(upload)
	if err != nil {
		log.Errorf("s3: Can't insert upload %s: %s",
				upload.UploadID, err.Error())
		return nil, err
	}

	log.Debugf("s3: Inserted upload %s", upload.UploadID)
	return &upload, err
}

func s3UploadPart(namespace string, bucket *S3Bucket, object_name,
			upload_id string, part int, data []byte) (string, error) {
	var upload S3Upload
	var object *S3Object
	var etag string
	var size int64
	var err error

	err = VerifyUploadUID(bucket, object_name, upload_id)
	if err != nil {
		return "", err
	}

	err = dbS3FindOne(bson.M{"uid": upload_id,
				"state": S3StateActive},
				&upload)
	if err != nil {
		return "", err
	}

	size = int64(len(data))

	object, err = s3InsertObject(bucket, object_name,
			upload.ObjID, part, 0, size, "")
	if err != nil {
		log.Errorf("s3: Can't insert object %s part %d: %s",
				object_name, part, err.Error())
		return "", err
	}

	log.Debugf("s3: Inserted object %s", object.BackendID)

	etag, err = s3CommitObject(namespace, bucket, object, data)
	if err != nil {
		log.Errorf("s3: Can't commit object %s part %d: %s",
				object_name, part, err.Error())
		return "", err
	}

	log.Debugf("s3: Committed object %s part %d", object.BackendID, part)
	return etag, nil
}

func s3UploadFini(bucket *S3Bucket, upload_id string) {
}

func s3UploadList(bucket *S3Bucket) {
}

func s3UploadAbort(bucket *S3Bucket, object_name, upload_id string) error {
	var objects []S3Object
	var upload S3Upload
	var err error

	// If upload is finished one have to delete parts like
	// they are one object via traditional delete object
	// interface.
	//
	// First disable the upload head, if something go wrong further
	// and we won't be able to remove parts then this makes them
	// unusable and hidden.
	//

	err = VerifyUploadUID(bucket, object_name, upload_id)
	if err != nil {
		return err
	}

	err = dbS3Update(
			bson.M{"uid": upload_id,
				"state": bson.M{"$in": s3StateTransition[S3StateAbort]}},
			bson.M{"$set": bson.M{"state": S3StateAbort}},
			&upload)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable upload %s/%s: %s",
				upload_id, object_name, err.Error())
		return err
	}

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &objects)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object parts with uid %s/%s: %s",
					upload_id, object_name, err.Error())
			return err
		}
	} else {
		for _, obj := range objects {
			err = s3DeleteObjectFound(bucket, &obj)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't delete object part %s/%s: %s",
							upload_id, object_name, err.Error())
					return err
				}
			}
		}
	}

	err = dbS3Remove(upload, bson.M{"_id": upload.ObjID})
	if err != nil {
		log.Errorf("s3: Can't delete upload %s/%s: %s",
				upload_id, object_name, err.Error())
		return err
	}

	log.Debugf("s3: Deleted upload %s/%s", upload_id, object_name)
	return nil
}
