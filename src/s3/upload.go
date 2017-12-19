package main

import (
	"gopkg.in/mgo.v2/bson"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type S3Upload struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	UploadID			string		`json:"uid" bson:"uid"`
	State				uint32		`json:"state" bson:"state"`

	S3ObjectPorps					`json:",inline" bson:",inline"`
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

func s3UploadFini(bucket *S3Bucket, upload_id string) {
}

func s3UploadList(bucket *S3Bucket) {
}

func s3UploadAbort(bucket *S3Bucket, upload_id string) error {
	return nil
}
