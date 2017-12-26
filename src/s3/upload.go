package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"crypto/sha256"
	"crypto/md5"
	"encoding/hex"
	"time"
	"fmt"

	"../apis/apps/s3"
)

type S3Upload struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	UploadID			string		`json:"uid" bson:"uid"`
	State				uint32		`json:"state" bson:"state"`

	// These three are filled upon
	// parts completion
	Parts				int64		`json:"parts" bson:"parts"`
	Size				int64		`json:"size" bson:"size"`
	ETag				string		`json:"etag" bson:"etag"`

	S3ObjectPorps					`json:",inline" bson:",inline"`
}

func (upload *S3Upload)dbSet(query bson.M, change bson.M) (error) {
	var res S3Upload
	return dbS3Update(query, change, &res)
}

func (upload *S3Upload)dbSetState(state uint32) (error) {
	return upload.dbSet(
			bson.M{"_id": upload.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"state": state})
}

func (upload *S3Upload)dbSetStateComplete(state uint32, etag string, parts, size int64) (error) {
	return upload.dbSet(
			bson.M{"_id": upload.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"state": state,
				"parts": parts,
				"size": size,
				"etag": etag})
}

// FIXME What to do if one start uploadin parts and
// same time create complete file with same name?
//
// Amazon says they are merging all parts into one
// result file so it could be read then back (and I
// suspect they are simply increment versioning or
// overwrite old files). We don't plan to merge
// the chunks instead and carry them as is.
func UploadUID(salt, oname string, part, version int) string {
	h := sha256.New()
	h.Write([]byte(salt + "-" + fmt.Sprintf("%d-%d", part, version) + "-" + oname))
	return hex.EncodeToString(h.Sum(nil))
}

func VerifyUploadUID(bucket *S3Bucket, oname, uid string) error {
	genuid := UploadUID(bucket.BackendID, oname, 0, 0)
	if genuid != uid {
		err := fmt.Errorf("uploadId mismatch")
		log.Errorf("s3: uploadId mismatch %s/%s", genuid, uid)
		return err
	}
	return nil
}

func s3UploadInit(bucket *S3Bucket, oname, acl string) (*S3Upload, error) {
	var err error

	upload := S3Upload{
		S3ObjectPorps: S3ObjectPorps {
			Key:		oname,
			Acl:		acl,
			CreationTime:	time.Now().Format(time.RFC3339),
		},

		BucketObjID:	bucket.ObjID,
		UploadID:	UploadUID(bucket.BackendID, oname, 0, 0),
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

func s3UploadPart(namespace string, bucket *S3Bucket, oname,
			uid string, part int, data []byte) (string, error) {
	var upload S3Upload
	var object *S3Object
	var etag string
	var size int64
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return "", err
	}

	err = dbS3FindOne(bson.M{"uid": uid,
				"state": S3StateActive},
				&upload)
	if err != nil {
		return "", err
	}

	size = int64(len(data))

	object, err = s3InsertObject(bucket, oname,
			upload.ObjID, part, 0, size, "")
	if err != nil {
		log.Errorf("s3: Can't insert object %s part %d: %s",
				oname, part, err.Error())
		return "", err
	}

	log.Debugf("s3: Inserted object %s", object.BackendID)

	etag, err = s3CommitObject(namespace, bucket, object, data)
	if err != nil {
		log.Errorf("s3: Can't commit object %s part %d: %s",
				oname, part, err.Error())
		return "", err
	}

	log.Debugf("s3: Committed object %s part %d", object.BackendID, part)
	return etag, nil
}

func s3UploadFini(bucket *S3Bucket, uid string, compete *swys3api.S3MpuFiniParts) (*swys3api.S3MpuFini, error) {
	var res swys3api.S3MpuFini
	var objects []S3Object
	var parts, size int64
	var upload S3Upload
	var err error

	err = dbS3FindOne(bson.M{"uid": uid,
				"state": S3StateActive},
				&upload)
	if err != nil {
		return nil, err
	}

	res.Bucket	= bucket.Name

	h := md5.New()

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &objects)
	if err != nil {
		if err == mgo.ErrNotFound {
			res.ETag = fmt.Sprintf("%x", md5.Sum(nil))
			goto out
		}
		log.Errorf("s3: Can't find upload %s: %s",
				uid, err.Error())
		return nil, err
	} else {
		for _, obj := range objects {
			var data []byte

			parts++
			size += obj.Size

			data, err = s3ReadObjectData(bucket, &obj)
			if err != nil {
				return nil, err
			}

			h.Write(data)
		}
	}

	res.ETag = fmt.Sprintf("%x", md5.Sum(nil))
	err = upload.dbSetStateComplete(S3StateInactive,
			res.ETag, parts, size)
	if err != nil {
		return nil, err
	}
out:
	log.Debugf("s3: Complete upload %v", res)
	return &res, nil
}

func s3Uploads(iam *S3Iam, akey *S3AccessKey, bname string) (*swys3api.S3MpuList, error) {
	var res swys3api.S3MpuList
	var bucket *S3Bucket
	var uploads []S3Upload
	var err error

	bucket, err = iam.FindBucket(akey, bname)
	if err != nil {
		log.Errorf("s3: Can't find bucket %s: %s", bname, err.Error())
		return nil, err
	}

	res.Bucket		= bucket.Name
	res.MaxUploads		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(bson.M{"bucket-id": bucket.ObjID,
				"state": S3StateActive}, &uploads)
	if err != nil {
		if err == mgo.ErrNotFound {
			goto out
		}
		log.Errorf("s3: Can't find uploads on bucket %s: %s",
				bucket.Name, err.Error())
		return nil, err
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

out:
	log.Debugf("s3: List upload %v", res)
	return &res, nil
}

func s3UploadList(bucket *S3Bucket, oname, uid string) (*swys3api.S3MpuPartList, error) {
	var res swys3api.S3MpuPartList
	var objects []S3Object
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return nil, err
	}

	err = dbS3FindOne(bson.M{"uid": uid,
				"state": S3StateActive},
				&upload)
	if err != nil {
		return nil, err
	}

	res.Bucket		= bucket.Name
	res.Key			= oname
	res.UploadId		= uid
	res.StorageClass	= swys3api.S3StorageClassStandard
	res.MaxParts		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &objects)
	if err != nil {
		if err == mgo.ErrNotFound {
			goto out
		}
		log.Errorf("s3: Can't find upload %s/%s: %s",
				uid, oname, err.Error())
		return nil, err
	} else {
		for _, obj := range objects {
			res.Part = append(res.Part,
				swys3api.S3MpuPart{
					PartNumber:	obj.Part,
					LastModified:	obj.CreationTime,
					ETag:		obj.ETag,
					Size:		obj.Size,
				})
		}
	}

out:
	log.Debugf("s3: List upload %v", res)
	return &res, nil
}

func s3UploadAbort(bucket *S3Bucket, oname, uid string) error {
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

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return err
	}

	err = dbS3Update(
			bson.M{"uid": uid,
				"state": bson.M{"$in": s3StateTransition[S3StateAbort]}},
			bson.M{"$set": bson.M{"state": S3StateAbort}},
			&upload)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: Can't disable upload %s/%s: %s",
				uid, oname, err.Error())
		return err
	}

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &objects)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find object parts with uid %s/%s: %s",
					uid, oname, err.Error())
			return err
		}
	} else {
		for _, obj := range objects {
			err = s3DeleteObjectFound(bucket, &obj)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't delete object part %s/%s: %s",
							uid, oname, err.Error())
					return err
				}
			}
		}
	}

	err = dbS3Remove(upload, bson.M{"_id": upload.ObjID})
	if err != nil {
		log.Errorf("s3: Can't delete upload %s/%s: %s",
				uid, oname, err.Error())
		return err
	}

	log.Debugf("s3: Deleted upload %s/%s", uid, oname)
	return nil
}
