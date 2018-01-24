package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"crypto/md5"
	"sort"
	"time"
	"fmt"

	"../apis/apps/s3"
)

type S3UploadPart struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	UploadObjID			bson.ObjectId	`bson:"upload-id,omitempty"`
	BackendID			string		`json:"bid" bson:"bid"`
	State				uint32		`json:"state" bson:"state"`

	Part				int		`json:"part" bson:"part"`
	Size				int64		`json:"size" bson:"size"`
	ETag				string		`json:"etag" bson:"etag"`
	Data				[]byte		`json:"data,omitempty" bson:"data,omitempty"`
	S3ObjectPorps					`json:",inline" bson:",inline"`
}

type S3Upload struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	BucketObjID			bson.ObjectId	`bson:"bucket-id,omitempty"`
	UploadID			string		`json:"uid" bson:"uid"`
	State				uint32		`json:"state" bson:"state"`

	S3ObjectPorps					`json:",inline" bson:",inline"`
}

func (upload *S3Upload)infoLong() (string) {
	return fmt.Sprintf("{ S3Upload: %s/%s/%s/%d/%s }",
			upload.ObjID, upload.BucketObjID,
			upload.UploadID, upload.Key)
}

func (part *S3UploadPart)infoLong() (string) {
	return fmt.Sprintf("{ S3UploadPart: %s/%s/%s/%d/%s }",
			part.ObjID, part.UploadObjID,
			part.BackendID, part.Part,
			part.Key)
}

func (part *S3UploadPart)dbRemoveF() (error) {
	err := dbS3Remove(part, bson.M{"_id": part.ObjID})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't force remove %s: %s",
			part.infoLong(), err.Error())
	}
	return err
}

func (part *S3UploadPart)dbRemove() (error) {
	err := dbS3RemoveCond(
			bson.M{	"_id": part.ObjID,
				"state": S3StateInactive},
			&S3UploadPart{})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't remove %s: %s",
			part.infoLong(), err.Error())
	}
	return err
}

func (part *S3UploadPart)dbSet(state uint32, fields bson.M) (error) {
	err := dbS3Update(
			bson.M{"_id": part.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": fields},
			&S3UploadPart{})
	if err != nil {
		log.Errorf("s3: Can't set state %d %s: %s",
			state, part.infoLong(), err.Error())
	}
	return err
}

func (part *S3UploadPart)dbSetState(state uint32) (error) {
	return part.dbSet(state, bson.M{"state": state})
}

func (upload *S3Upload)dbRemoveF() (error) {
	err := dbS3Remove(upload, bson.M{"_id": upload.ObjID})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't force remove %s: %s",
			upload.infoLong(), err.Error())
	}
	return err
}

func (upload *S3Upload)dbRemove() (error) {
	err := dbS3RemoveCond(
			bson.M{	"_id": upload.ObjID,
				"state": S3StateInactive},
			&S3Upload{})
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("s3: Can't remove %s: %s",
			upload.infoLong(), err.Error())
	}
	return err
}

func (upload *S3Upload)dbSet(state uint32, fields bson.M) (error) {
	err := dbS3Update(
			bson.M{"_id": upload.ObjID,
				"state": bson.M{"$in": s3StateTransition[state]}},
			bson.M{"$set": fields},
			&S3Upload{})
	if err != nil {
		log.Errorf("s3: Can't set state %d %s: %s",
			state, upload.infoLong(), err.Error())
	}
	return err
}

func (upload *S3Upload)dbSetState(state uint32) (error) {
	return upload.dbSet(state, bson.M{"state": state})
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

func s3UploadRemove(upload *S3Upload) (error) {
	var err error

	err = dbS3Remove(&S3UploadPart{}, bson.M{"upload-id": upload.ObjID})
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't delete upload parts %s: %s",
					upload.UploadID, err.Error())
			return err
		}
	}

	err = dbS3Remove(upload, bson.M{"_id": upload.ObjID})
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't delete upload %s: %s",
					upload.UploadID, err.Error())
			return err
		}
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
		UploadID:	bucket.UploadUID(oname),
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
			uid string, partno int, data []byte) (string, error) {
	var part S3UploadPart
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return "", err
	}

	err = dbS3FindOne(bson.M{"uid": uid}, &upload)
	if err != nil {
		return "", err
	}

	if int64(len(data)) > S3StorageSizePerObj {
		err = fmt.Errorf("upload part is too big")
		log.Errorf("s3: Can't insert upload %s object %s part %d: %s",
				upload.UploadID, oname, part.Part, err.Error())
		return "", err
	}

	part = S3UploadPart{
		S3ObjectPorps: S3ObjectPorps {
			CreationTime:	time.Now().Format(time.RFC3339),
		},
		UploadObjID:	upload.ObjID,
		BackendID:	upload.ObjectBID(oname, partno),
		Part:		partno,
		Size:		int64(len(data)),
		ETag:		md5sum(data),
		Data:		data,
	}

	err = dbS3Insert(part)
	if err != nil {
		log.Errorf("s3: Can't insert upload %s object %s part %d: %s",
				upload.UploadID, oname, part.Part, err.Error())
		return "", err
	}

	log.Debugf("s3: Inserted upload %s object %s part %d",
			upload.UploadID, oname, part.Part)
	return part.ETag, nil
}

type S3UploadByPart []S3UploadPart

func (o S3UploadByPart) Len() int           { return len(o) }
func (o S3UploadByPart) Swap(i, j int)      { o[i], o[j] = o[j], o[i] }
func (o S3UploadByPart) Less(i, j int) bool { return o[i].Part < o[j].Part }

func s3UploadFini(namespace string, bucket *S3Bucket, uid string,
			compete *swys3api.S3MpuFiniParts) (*swys3api.S3MpuFini, error) {
	var res swys3api.S3MpuFini
	var parts []S3UploadPart
	var upload S3Upload
	var size int64
	var partno int
	var data []byte
	var err error

	err = dbS3FindOne(bson.M{"uid": uid}, &upload)
	if err != nil {
		return nil, err
	}

	res.Bucket	= bucket.Name
	res.Key		= upload.Key

	h := md5.New()

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &parts)
	if err != nil {
		if err == mgo.ErrNotFound {
			res.ETag = fmt.Sprintf("%x", md5.Sum(nil))
			goto out
		}
		log.Errorf("s3: Can't find upload %s: %s",
				uid, err.Error())
		return nil, err
	} else {
		sort.Sort(S3UploadByPart(parts))
		partno = 0

		for _, part := range parts {
			if partno >= part.Part {
				err = fmt.Errorf("upload %s unexpected part %d",
						uid, part.Part)
				log.Errorf("s3: Upload part sequence failed: %s",
						err.Error())
				return nil, err
			}
			partno = part.Part
			size += part.Size

			h.Write(part.Data)
			data = append(data, part.Data ...)
		}
	}

	_, err = s3AddObject(namespace, bucket, upload.Key, upload.Acl, size, data)
	if err != nil {
		log.Errorf("s3: Can't insert object %d upload %s: %s",
				upload.Key, uid, err.Error())
		return nil, err
	}

	err = s3UploadRemove(&upload)
	if err != nil {
		// Don't fail here since object is already committed
		log.Errorf("s3: Can't remove upload %s: %s",
				uid, err.Error())
	}

	res.ETag = fmt.Sprintf("%x", md5.Sum(nil))
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
	var parts []S3UploadPart
	var upload S3Upload
	var err error

	err = VerifyUploadUID(bucket, oname, uid)
	if err != nil {
		return nil, err
	}

	err = dbS3FindOne(bson.M{"uid": uid}, &upload)
	if err != nil {
		return nil, err
	}

	res.Bucket		= bucket.Name
	res.Key			= oname
	res.UploadId		= uid
	res.StorageClass	= swys3api.S3StorageClassStandard
	res.MaxParts		= 1000
	res.IsTruncated		= false

	err = dbS3FindAll(bson.M{"upload-id": upload.ObjID}, &parts)
	if err != nil {
		if err == mgo.ErrNotFound {
			goto out
		}
		log.Errorf("s3: Can't find upload %s/%s: %s",
				uid, oname, err.Error())
		return nil, err
	} else {
		for _, part := range parts {
			res.Part = append(res.Part,
				swys3api.S3MpuPart{
					PartNumber:	int(part.Part),
					LastModified:	part.CreationTime,
					ETag:		part.ETag,
					Size:		part.Size,
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

	err = s3UploadRemove(&upload)
	if err != nil {
		return nil
	}

	log.Debugf("s3: Deleted upload %s/%s", uid, oname)
	return nil
}
