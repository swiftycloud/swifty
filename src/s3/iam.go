package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"context"
	"time"
	"fmt"
	"../common"
	"./mgo"
)

func s3AccountInsert(ctx context.Context, namespace, user string) (*s3mgo.S3Account, error) {
	var account s3mgo.S3Account
	var err error

	if namespace == "" {
		return nil, fmt.Errorf("s3: Empty namespace")
	}

	id := bson.NewObjectId()
	timestamp := current_timestamp()
	insert := bson.M{
		"_id":			id,
		"mtime":		timestamp,
		"state":		S3StateActive,

		"aws-id":		swy.Sha256sum([]byte(id.String())),
		"namespace":		namespace,

		"creation-time":	time.Now().Format(time.RFC3339),
		"user":			s3mgo.AccountUser(namespace, user),
		"email":		user + "@mail",
	}
	query := bson.M{ "namespace": namespace, "state": S3StateActive }
	update := bson.M{ "$setOnInsert": insert }

	log.Debugf("s3: Upserting namespace %s", namespace)
	if err = dbS3Upsert(ctx, query, update, &account); err != nil {
		return nil, err
	}

	log.Debugf("s3: Upserted %s", infoLong(&account))
	return &account, nil
}

func s3AccountLookup(ctx context.Context) (*s3mgo.S3Account, error) {
	var account s3mgo.S3Account
	var err error

	iam := ctxIam(ctx)
	query := bson.M{ "_id": iam.AccountObjID, "state": S3StateActive }
	err = dbS3FindOne(ctx, query, &account)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Errorf("s3: Can't find account %s: %s",
				infoLong(iam), err.Error())
		}
		return nil, err
	}

	return &account, nil
}

func s3FindFullAccessIam(ctx context.Context, namespace string) (*s3mgo.S3Iam, error) {
	var account s3mgo.S3Account
	var iams []s3mgo.S3Iam
	var query bson.M
	var err error

	if namespace == "" {
		return nil, fmt.Errorf("s3: Empty namespace")
	}

	query = bson.M{ "namespace": namespace, "state": S3StateActive }
	err = dbS3FindOne(ctx, query, &account)
	if err != nil {
		return nil, err
	}

	query = bson.M{ "account-id" : account.ObjID, "state": S3StateActive }
	err = dbS3FindAll(ctx, query, &iams)
	if err == nil {
		for _, iam := range iams {
			if isRoot(&iam.Policy) {
				return &iam, nil
			}
		}
		err = fmt.Errorf("No root iam found")
	}
	return nil, err
}

func s3AccountDelete(ctx context.Context, account *s3mgo.S3Account) (error) {
	err := dbS3SetState(ctx, account, S3StateInactive, nil)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	// FIXME Delete related iams/keys/buckets

	dbS3Remove(ctx, account)
	log.Debugf("s3: Deleted %s", infoLong(account))
	return nil
}

func s3IamNew(ctx context.Context, account *s3mgo.S3Account, policy *s3mgo.S3Policy) (*s3mgo.S3Iam, error) {
	var iam *s3mgo.S3Iam
	var err error

	id := bson.NewObjectId()
	iam = &s3mgo.S3Iam {
		ObjID:		id,
		MTime:		current_timestamp(),
		State:		S3StateActive,
		AwsID:		swy.Sha256sum([]byte(id.String())),
		AccountObjID:	account.ObjID,
		Policy:		*policy,
		CreationTime:	time.Now().Format(time.RFC3339),
		User:		account.IamUser(id.Hex()),
	}

	log.Debugf("s3: Upserting iam %s", iam.User)
	if err = dbS3Insert(ctx, iam); err != nil {
		return nil, err
	}

	log.Debugf("s3: Upserted %s", infoLong(&iam))
	return iam, nil
}

func s3IamDelete(ctx context.Context, iam *s3mgo.S3Iam) (error) {
	if err := dbS3SetState(ctx, iam, S3StateInactive, nil); err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	dbS3Remove(ctx, iam)
	log.Debugf("s3: Deleted %s", infoLong(iam))
	return nil
}

func s3LookupIam(ctx context.Context, id bson.ObjectId) (*s3mgo.S3Iam, error) {
	var res s3mgo.S3Iam

	err := dbS3FindOne(ctx, bson.M{"_id": id, "state": S3StateActive }, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func s3IamFind(ctx context.Context, akey *s3mgo.S3AccessKey) (*s3mgo.S3Iam, error) {
	return s3LookupIam(ctx, akey.IamObjID)
}
