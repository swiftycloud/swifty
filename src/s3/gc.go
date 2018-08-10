package main

import (
	"time"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"./mgo"
)

const gcDefaultPeriod = uint32(10)

func gcInit(period uint32) error {
	if period == 0 { period = gcDefaultPeriod }

	go func() {
		for {
			ctx, done := mkContext("GC keys")
			gc_keys(ctx);
			done(ctx)
			time.Sleep(time.Duration(period) * time.Second)
		}
	}()

	return nil
}

func gcOldVersions(b *s3mgo.S3Bucket, key string, rover int64) {
	ctx, done := mkContext("GC old obj")
	defer done(ctx)

	var object s3mgo.S3Object

	query := bson.M{ "bucket-id": b.ObjID, "state": S3StateActive, "key": key, "rover": bson.M {"$lt": rover}}
	pipe := dbS3Pipe(ctx, &object, []bson.M{{"$match": query}, {"$sort": bson.M{"key": 1, "rover": -1}}})
	iter := pipe.Iter()

	for iter.Next(&object) {
		err := DropObject(ctx, b, &object)
		if err != nil && err != mgo.ErrNotFound {
			log.Errorf("Can't GC object %s:%s, rover %d: %s", b.BCookie, key, object.Rover, err.Error())
		}
	}

	iter.Close()
}
