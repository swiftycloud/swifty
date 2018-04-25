package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
)

/* FIXME -- set up public IP address/port for this FN */

func urlEventStart(ctx context.Context, ed *FnEventDesc) error {
	return dbFuncUpdate(bson.M{"cookie": ed.FnId, "url": false},
		bson.M{"$set": bson.M{"url": true}})
}

func urlEventStop(ctx context.Context, ed *FnEventDesc) error {
	return dbFuncUpdate(bson.M{"cookie": ed.FnId},
		bson.M{"$set": bson.M{"url": false}})
}
