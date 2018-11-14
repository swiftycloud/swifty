/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2/bson"
	"context"
	"github.com/streadway/amqp"
	"encoding/json"
	"strings"
	"fmt"
	"swifty/s3/mgo"
	"swifty/common"
	"swifty/apis/s3"
)

func notifyFindBucket(ctx context.Context, params *swys3api.Subscribe) (*s3mgo.Bucket, error) {
	var bucket s3mgo.Bucket

	cookie := s3mgo.BCookie(params.Namespace, params.Bucket)
	err := dbS3FindOne(ctx, bson.M{ "bcookie": cookie, "state": S3StateActive }, &bucket)
	if err != nil {
		return nil, err
	}

	return &bucket, nil
}

func s3Subscribe(ctx context.Context, params *swys3api.Subscribe) error {
	bucket, err := notifyFindBucket(ctx, params)
	if err != nil {
		return err
	}

	ops := bson.M{}
	for _, op := range strings.Split(params.Ops, ",") {
		ops["notify." + op] = 1
	}
	update := bson.M{
		"$set": bson.M{ "notify.queue": params.Queue },
		"$inc": ops,
	}

	query := bson.M{ "state": S3StateActive }
	return dbS3Update(ctx, query, update, false, bucket)
}

func s3Unsubscribe(ctx context.Context, params *swys3api.Subscribe) error {
	bucket, err := notifyFindBucket(ctx, params)
	if err != nil {
		return err
	}

	ops := bson.M{}
	for _, op := range strings.Split(params.Ops, ",") {
		ops["notify." + op] = -1
	}
	update := bson.M{"$inc": ops}

	query := bson.M{ "state": S3StateActive }
	return dbS3Update(ctx, query, update, false, bucket)
}

var nChan *amqp.Channel

func s3Notify(ctx context.Context, bucket *s3mgo.Bucket, object *s3mgo.Object, op string) {
	account, err := s3AccountLookup(ctx)
	if err != nil { return }

	data, err := json.Marshal(&swys3api.Event{
			Namespace: account.Namespace,
			Bucket: bucket.Name,
			Object: object.Key,
			Op: op,
		})

	// XXX Throttling

	err = nChan.Publish("", bucket.BasicNotify.Queue, false, false, amqp.Publishing{
			ContentType: "application/json",
			Body: data,
		})
	if err != nil {
		log.Error("Failed to send notification: %s", err.Error())
	}
}

func notifyInit(conf *YAMLConfNotify) error {
	if conf.Rabbit == "" {
		return nil
	}

	xc := xh.ParseXCreds(conf.Rabbit)
	pwd, err := s3Secrets.Get(xc.Pass)
	if err != nil {
		return fmt.Errorf("No notify queue password: %s", err.Error())
	}

	xc.Pass = pwd

	log.Debugf("Turn on AMQP notifications via %s", xc.Domn)

	nConn, err := amqp.Dial("amqp://" + xc.URL())
	if err != nil {
		return fmt.Errorf("Can't dial amqp: %s", err.Error())
	}

	nChan, err = nConn.Channel()
	if err != nil {
		nConn.Close()
		return fmt.Errorf("Can't get channel: %s", err.Error())
	}

	return nil
}
