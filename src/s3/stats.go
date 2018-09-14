package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"./mgo"
)

func StatsAcct(ctx context.Context, nsid string, upd bson.M) error {
	err := dbS3Upsert(ctx, bson.M{ "nsid": nsid }, bson.M{ "$inc": upd }, &s3mgo.S3AcctStats{} )
	if err != nil {
		log.Errorf("s3: Can't +account %v to %s: %s",
				upd, nsid, err.Error())
	}
	return nil
}

func StatsAcctInt64(ctx context.Context, nsid string, metric string, value int64) error {
	return StatsAcct(ctx, nsid, bson.M{ metric: value })
}
