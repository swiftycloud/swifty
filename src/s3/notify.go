package main

import (
	"gopkg.in/mgo.v2/bson"
	"github.com/streadway/amqp"
	"encoding/json"
	"strings"
	"fmt"
	"../apis/apps/s3"
)

const (
	S3NotifyPut =		1
)

var eventNames = []string {
	1: "put",
}

func genOpsMask(ops string) (uint64, error) {
	var ret uint64

	opss := strings.Split(ops, ",")
out:
	for _, op := range opss {
		for n, opn := range eventNames {
			if op == opn {
				ret |= (uint64(1) << uint(n))
				continue out
			}
		}

		return 0, fmt.Errorf("Unknwon op %s", op)
	}

	return ret, nil
}

func s3Subscribe(params *swys3ctl.S3Subscribe) error {
	var ops uint64
	ops, err := genOpsMask(params.Ops)
	if err != nil {
		return err
	}

	var res S3Bucket
	return dbS3Update(
			bson.M {"bid": BIDFromNames(params.Namespace, params.Bucket),
				"state": S3StateActive,
			},
			bson.M {"$set": bson.M {
					"notify": bson.M {
						"events": ops,
						"queue": params.Queue,
					},
				},
			}, &res)
}

func s3Unsubscribe(params *swys3ctl.S3Subscribe) error {
	var res S3Bucket
	return dbS3Update(
			bson.M {"bid": BIDFromNames(params.Namespace, params.Bucket),
				"state": S3StateActive,
			},
			bson.M {"$unset": bson.M { "notify": "" }, }, &res)
}

var nChan *amqp.Channel

func s3Notify(namespace string, bucket *S3Bucket, object *S3Object, op uint) {
	if bucket.BasicNotify.Events & (uint64(1) << op) == 0 {
		return
	}

	data, err := json.Marshal(&swys3ctl.S3Event{
			Namespace: namespace,
			Bucket: bucket.Name,
			Object: object.Name,
			Op: eventNames[op],
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
	if conf.Rabbit == nil {
		return nil
	}

	log.Debugf("Turn on AMQP notifications via %s", conf.Rabbit.Target)

	nConn, err := amqp.Dial("amqp://" + conf.Rabbit.User + ":" + s3Secrets[conf.Rabbit.Pass] + "@" + conf.Rabbit.Target)
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
