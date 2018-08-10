package main

import (
	"gopkg.in/mgo.v2/bson"
	"context"
	"github.com/streadway/amqp"
	"encoding/json"
	"strings"
	"fmt"
	"./mgo"
	"../common"
	"../apis/apps/s3"
)

func notifyFindBucket(ctx context.Context, params *swys3api.S3Subscribe) (*s3mgo.S3Bucket, error) {
	var bucket s3mgo.S3Bucket

	cookie := s3mgo.BCookie(params.Namespace, params.Bucket)
	err := dbS3FindOne(ctx, bson.M{ "bcookie": cookie, "state": S3StateActive }, &bucket)
	if err != nil {
		return nil, err
	}

	return &bucket, nil
}

func s3Subscribe(ctx context.Context, params *swys3api.S3Subscribe) error {
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

func s3Unsubscribe(ctx context.Context, params *swys3api.S3Subscribe) error {
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

func s3Notify(ctx context.Context, iam *s3mgo.S3Iam, bucket *s3mgo.S3Bucket, object *s3mgo.S3Object, op string) {
	account, err := s3AccountLookup(ctx, iam)
	if err != nil { return }

	data, err := json.Marshal(&swys3api.S3Event{
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

	xc := swy.ParseXCreds(conf.Rabbit)
	xc.Pass = s3Secrets[xc.Pass]

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
