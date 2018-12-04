/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2/bson"
	"context"
	"swifty/s3/mgo"
)

func bucketAcct(ctx context.Context, b *s3mgo.Bucket, upd bson.M) error {
	return dbS3Update(ctx, bson.M{ "state": S3StateActive }, bson.M{ "$inc": upd }, true, b)
}

func commitObj(ctx context.Context, bucket *s3mgo.Bucket, size int64) (error) {
	m := bson.M{ "ref": -1 }
	err := bucketAcct(ctx, bucket, m)
	if err != nil {
		log.Errorf("s3: Can't commit %d bytes %s: %s",
			size, infoLong(bucket), err.Error())
	}
	return err
}

func acctObj(ctx context.Context, bucket *s3mgo.Bucket, size int64) (error) {
	var eru error

	m := bson.M{ "cnt-objects": 1, "cnt-bytes": size }
	err := StatsAcct(ctx, bucket.NamespaceID, m)
	if err != nil {
		goto er1
	}

	m = bson.M{ "cnt-objects": 1, "cnt-bytes": size, "ref": 1, "rover": int64(1) }
	err = bucketAcct(ctx, bucket, m)
	if err != nil {
		goto er2
	}

	return nil

er2:
	m = bson.M{ "cnt-objects": -1, "cnt-bytes": -size }
	eru = StatsUnacct(ctx, bucket.NamespaceID, m)
er1:
	log.Errorf("s3: Can't +account %d bytes %s: %s (unacct %v)", size, infoLong(bucket), err.Error(), eru)
	requestFsck()
	return err
}

func unacctObj(ctx context.Context, bucket *s3mgo.Bucket, size int64, dropref bool) (error) {
	m := bson.M{ "cnt-objects": -1, "cnt-bytes": -size }
	if dropref {
		m["ref"] = -1
	}
	err := bucketAcct(ctx, bucket, m)
	if err != nil {
		goto er1
	}

	m = bson.M{ "cnt-objects": -1, "cnt-bytes": -size }
	err = StatsUnacct(ctx, bucket.NamespaceID, m)
	if err != nil {
		goto er2
	}

	return nil

er2:
	;
er1:
	log.Errorf("s3: Can't -account %d bytes %s: %s", size, infoLong(bucket), err.Error())
	requestFsck()
	return err
}
