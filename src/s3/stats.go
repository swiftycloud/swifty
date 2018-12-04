/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"swifty/s3/mgo"
)

func StatsAcct(ctx context.Context, nsid string, upd bson.M) error {
	return dbS3Upsert(ctx, bson.M{ "nsid": nsid }, bson.M{ "$inc": upd }, &s3mgo.AcctStats{} )
}

func StatsUnacct(ctx context.Context, nsid string, upd bson.M) error {
	return dbS3Update(ctx, bson.M{ "nsid": nsid }, bson.M{ "$inc": upd }, false, &s3mgo.AcctStats {})
}

func StatsAcctInt64(ctx context.Context, nsid string, metric string, value int64) error {
	return StatsAcct(ctx, nsid, bson.M{ metric: value })
}

func StatsFindFor(ctx context.Context, act *s3mgo.Account) (*s3mgo.AcctStats, error) {
	var st s3mgo.AcctStats

	err := dbS3FindOne(ctx, bson.M{"nsid": act.NamespaceID()}, &st)
	if err != nil && !dbNF(err) {
		return nil, err
	}

	return &st, nil
}
