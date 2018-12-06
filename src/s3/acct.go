/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2/bson"
	"context"
	"errors"
	"swifty/s3/mgo"
)

func rsLimited(st *s3mgo.AcctStats) error {
	if st.Lim == nil {
		return nil
	}

	if st.Lim.CntBytes != 0 && st.CntBytes > st.Lim.CntBytes {
		return errors.New("Objects total size exceeded")
	}

	return nil
}

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
	st, err := StatsAcct(ctx, bucket.NamespaceID, m)
	if err != nil {
		goto er1
	}

	err = rsLimited(st)
	if err != nil {
		m = bson.M{ "cnt-objects": -1, "cnt-bytes": -size }
		eru = StatsUnacct(ctx, bucket.NamespaceID, m)
		if eru != nil {
			goto er1
		}

		return err
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

func checkDownload(ctx context.Context, nsid string) error {
	/* XXX -- limit OutBytesTot here */
	return nil
}

func acctDownload(ctx context.Context, nsid string, size int64) {
	mn := "out-bytes"
	if ctx.(*s3Context).id == "web" {
		mn += "-web"
	}

	err := StatsAcctInt64(ctx, nsid, mn, size)
	if err != nil {
		log.Errorf("acct: Cannot account download: %s", err.Error())
	}
}
