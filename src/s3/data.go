/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"crypto/md5"
	"context"
	"errors"
	"time"
	"fmt"
	"io"
	"swifty/s3/mgo"
)

func s3ReadChunks(ctx context.Context, part *s3mgo.ObjectPart) ([]byte, error) {
	var res []byte

	if len(part.Chunks) == 0 {
		return radosReadObject(part.BCookie, part.OCookie, uint64(part.Size), 0)
	}

	for _, cid := range part.Chunks {
		var ch s3mgo.DataChunk

		err := dbS3FindOne(ctx, bson.M{"_id": cid}, &ch)
		if err != nil {
			return nil, err
		}

		res = append(res, ch.Bytes...)
	}

	return res, nil
}

func s3IterChunks(ctx context.Context, part *s3mgo.ObjectPart, fn IterChunksFn) error {
	if len(part.Chunks) == 0 {
		return errors.New("Rados cannot iter chunks yet")
	}

	for _, cid := range part.Chunks {
		var ch s3mgo.DataChunk

		err := dbS3FindOne(ctx, bson.M{"_id": cid}, &ch)
		if err != nil {
			return err
		}

		err = fn(&ch)
		if err != nil {
			return err
		}
	}

	return nil
}

type chunkReader interface {
	Size() int64
	Next(sz int64)([]byte, error)
}

type ioChunkReader struct {
	sz	int64
	r	io.Reader

	read	int64
}

func (cr *ioChunkReader)Size() int64 { return cr.sz }

func (cr *ioChunkReader)Next(sz int64) ([]byte, error) {
	ret := make([]byte, sz)
	ln, err := cr.r.Read(ret)
	if ln != 0 {
		cr.read += int64(ln)
		return ret[:ln], nil
	}

	if err == io.EOF {
		err = nil
	}

	return nil, err
}

func s3WriteChunks(ctx context.Context, part *s3mgo.ObjectPart, data chunkReader) (string, error) {
	var err error

	if !radosDisabled && part.Size > S3StorageSizePerObj {
		return radosWriteObject(part.BCookie, part.OCookie, data, 0)
	}

	hasher := md5.New()

	for {
		chd, err := data.Next(S3StorageSizePerObj)
		if err != nil {
			goto out
		}
		if chd == nil {
			break
		}

		chunk := &s3mgo.DataChunk {
			ObjID:	bson.NewObjectId(),
			Bytes:	chd,
		}

		hasher.Write(chunk.Bytes)

		err = dbS3Insert(ctx, chunk)
		if err != nil {
			goto out
		}

		part.Chunks = append(part.Chunks, chunk.ObjID)
	}

	err = dbS3Update(ctx, bson.M{"_id": part.ObjID},
			bson.M{ "$set": bson.M{ "chunks": part.Chunks }},
			false, &s3mgo.ObjectPart{})
	if err != nil {
		goto out
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil

out:
	if len(part.Chunks) != 0 {
		s3DeleteChunks(ctx, part)
	}
	return "", err
}

func s3DeleteChunks(ctx context.Context, part *s3mgo.ObjectPart) error {
	var err error

	if len(part.Chunks) == 0 {
		err = radosDeleteObject(part.BCookie, part.OCookie)
	} else {
		for _, ch := range part.Chunks {
			er := dbS3Remove(ctx, &s3mgo.DataChunk{ObjID: ch})
			if err != nil {
				err = er
			}
		}
	}
	if err != nil {
		log.Errorf("s3: %s/%s backend object data may stale",
			part.BCookie, part.OCookie)
	}

	return err
}

func s3RepairObjectData(ctx context.Context) error {
	var objps []s3mgo.ObjectPart
	var err error

	log.Debugf("s3: Running object data consistency test")

	err = dbS3FindAllInactive(ctx, &objps)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		log.Errorf("s3: s3RepairObjectData failed: %s", err.Error())
		return err
	}

	for _, objp := range objps {
		var object s3mgo.Object
		var bucket s3mgo.Bucket

		log.Debugf("s3: Detected stale object data %s", infoLong(&objp))

		query_ref := bson.M{ "_id": objp.RefID }

		err = dbS3FindOne(ctx, query_ref, &object)
		if err != nil {
			if err != mgo.ErrNotFound {
				log.Errorf("s3: Can't find object on data %s: %s",
					infoLong(&objp), err.Error())
				return err
			}
		} else {
			query_bucket := bson.M{ "_id": object.BucketObjID }
			err = dbS3FindOne(ctx, query_bucket, &bucket)
			if err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't find bucket on object %s: %s",
						infoLong(&object), err.Error())
					return err
				}
			} else {
				err = s3DirtifyBucket(ctx, bucket.ObjID)
				if err != nil {
					if err != mgo.ErrNotFound {
						log.Errorf("s3: Can't dirtify bucket on object %s: %s",
						infoLong(&bucket), err.Error())
						return err
					}
				}
			}

			if err = dbS3Remove(ctx, &object); err != nil {
				if err != mgo.ErrNotFound {
					log.Errorf("s3: Can't remove object on data %s: %s",
						infoLong(&objp), err.Error())
					return err
				}
			}
			log.Debugf("s3: Removed object on data %s: %s", infoLong(&objp), err.Error())

		}

		s3DeleteChunks(ctx, &objp)

		err = dbS3Remove(ctx, &objp)
		if err != nil {
			log.Debugf("s3: Can't remove object data %s", infoLong(&objp))
			return err
		}

		log.Debugf("s3: Removed stale object data %s", infoLong(&objp))
	}

	log.Debugf("s3: Object data consistency passed")
	return nil
}

func s3DeactivateObjectData(ctx context.Context, refID bson.ObjectId) error {
	update := bson.M{ "$set": bson.M{ "state": S3StateInactive } }
	query  := bson.M{ "ref-id": refID }

	return dbS3Update(ctx, query, update, false, &s3mgo.ObjectPart{})
}

func s3ObjectPartFind(ctx context.Context, refID bson.ObjectId) ([]*s3mgo.ObjectPart, error) {
	var res []*s3mgo.ObjectPart

	err := dbS3FindAllFields(ctx, bson.M{"ref-id": refID, "state": S3StateActive}, bson.M{"data": 0}, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3ObjectPartFindFull(ctx context.Context, refID bson.ObjectId) ([]*s3mgo.ObjectPart, error) {
	var res []*s3mgo.ObjectPart

	err := dbS3FindAllSorted(ctx, bson.M{"ref-id": refID, "state": S3StateActive}, "part",  &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3ObjectPartsIter(ctx context.Context, refID bson.ObjectId, fn IterPartsFn) error {
	var p s3mgo.ObjectPart

	iter := dbS3IterAllSorted(ctx, bson.M{"ref-id": refID, "state": S3StateActive}, "part",  &p)
	defer iter.Close()

	for iter.Next(&p) {
		err := fn(&p)
		if err != nil {
			return err
		}
	}

	return iter.Err()
}

func s3ObjectPartAdd(ctx context.Context, refid bson.ObjectId, bucket_bid, object_bid string, part int,
		data chunkReader) (*s3mgo.ObjectPart, error) {
	var objp *s3mgo.ObjectPart
	var err error
	var csum string

	objp = &s3mgo.ObjectPart {
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		RefID:		refid,
		BCookie:	bucket_bid,
		OCookie:	object_bid,
		Size:		data.Size(),
		Part:		uint(part),
		CreationTime:	time.Now().Format(time.RFC3339),
	}

	if err = dbS3Insert(ctx, objp); err != nil {
		goto out
	}

	csum, err = s3WriteChunks(ctx, objp, data)
	if err != nil {
		goto out
	}

	if err = dbS3SetState2(ctx, objp, S3StateActive, bson.M{"etag": csum}); err != nil {
		s3DeleteChunks(ctx, objp)
		goto out
	}

	log.Debugf("s3: Added %s", infoLong(objp))
	return objp, nil

out:
	dbS3Remove(ctx, objp)
	return nil, err
}

func s3ObjectPartDel(ctx context.Context, bucket *s3mgo.Bucket, ocookie string, objp []*s3mgo.ObjectPart) (error) {
	for _, od := range objp {
		err := s3ObjectPartDelOne(ctx, bucket, ocookie, od)
		if err != nil {
			return err
		}
	}

	return nil
}

func s3ObjectPartDelOne(ctx context.Context, bucket *s3mgo.Bucket, ocookie string, objp *s3mgo.ObjectPart) (error) {
	var err error

	err = dbS3SetState(ctx, objp, S3StateInactive, nil)
	if err != nil {
		return err
	}

	err = s3DeleteChunks(ctx, objp)
	if err != nil {
		return err
	}

	err = dbS3RemoveOnState(ctx, objp, S3StateInactive, nil)
	if err != nil {
		return err
	}

	return nil
}

func s3ObjectPartRead(ctx context.Context, bucket *s3mgo.Bucket, ocookie string, objp []*s3mgo.ObjectPart) ([]byte, error) {
	var res []byte

	for _, od := range objp {
		x, err := s3ReadChunks(ctx, od)
		if err != nil {
			return nil, err
		}

		res = append(res, x...)
	}

	return res, nil
}

func s3ObjectPartsResum(ctx context.Context, upload *S3Upload) (int64, string, error) {
	var objp *s3mgo.ObjectPart
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var size int64

	hasher := md5.New()

	pipe = dbS3Pipe(ctx, objp,
		[]bson.M{{"$match": bson.M{"ref-id": upload.ObjID}},
			{"$sort": bson.M{"part": 1} }})
	iter = pipe.Iter()
	for iter.Next(&objp) {
		if objp.State != S3StateActive {
			continue
		}
		if len(objp.Chunks) == 0 {
			/* XXX Too bad :( */
			return 0, "", fmt.Errorf("Can't finish upload")
		}

		for _, cid := range objp.Chunks {
			var ch s3mgo.DataChunk

			err := dbS3FindOne(ctx, bson.M{"_id": cid}, &ch)
			if err != nil {
				return 0, "", err
			}

			hasher.Write(ch.Bytes)
		}
		size += objp.Size
	}
	if err := iter.Close(); err != nil {
		log.Errorf("s3: Can't close iter on %s: %s",
			infoLong(&upload), err.Error())
		return 0, "", err
	}

	return size, fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
