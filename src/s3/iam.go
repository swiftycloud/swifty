package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"fmt"
)

type S3Account struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	Namespace			string		`bson:"namespace,omitempty"`
	Ref				int64		`bson:"ref"`

	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
	Email				string		`bson:"email,omitempty"`
}

type S3Iam struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`

	Policy				S3Policy	`bson:"policy,omitempty"`
	CreationTime			string		`bson:"creation-time,omitempty"`
	User				string		`bson:"user,omitempty"`
}

func s3AccountInsert(namespace string) (*S3Account, error) {
	var account S3Account
	var err error

	if namespace == "" {
		return nil, fmt.Errorf("s3: Empty namespace")
	}

	timestamp := current_timestamp()
	insert := bson.M{
		"mtime":		timestamp,
		"state":		S3StateActive,

		"namespace":		namespace,
		"ref":			0,

		"creation-time":	time.Now().Format(time.RFC3339),
		"user":			"user" + genKey(8, AccessKeyLetters),
		"email":		"email" + genKey(8, AccessKeyLetters) + "@fake.mail",
	}
	query := bson.M{ "namespace": namespace, "state": S3StateActive }
	update := bson.M{ "$setOnInsert": insert }

	log.Debugf("s3: Upserting namespace %s", namespace)
	if err = dbS3Upsert(query, update, &account); err != nil {
		return nil, err
	}

	log.Debugf("s3: Upserted %s", infoLong(&account))
	return &account, nil
}

func (account *S3Account) RefAdd(ref int64) (error) {
	m := bson.M{ "ref": ref }
	return dbS3Update(bson.M{ "state": S3StateActive },
		bson.M{ "$inc": m }, true, account)
}

func (account *S3Account) RefInc() (error) {
	return account.RefAdd(1)
}

func (account *S3Account) RefDec() (error) {
	return account.RefAdd(-1)
}

func (iam *S3Iam) s3AccountLookup() (*S3Account, error) {
	var account S3Account
	var err error

	query := bson.M{ "_id": iam.AccountObjID, "state": S3StateActive }
	err = dbS3FindOne(query, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func s3AccountFind(namespace string) (*S3Account, error) {
	var account S3Account
	var err error

	if namespace == "" {
		return nil, fmt.Errorf("s3: Empty namespace")
	}

	query := bson.M{ "namespace": namespace, "state": S3StateActive }
	err = dbS3FindOne(query, &account)
	if err != nil {
		return nil, err
	}

	return &account, nil
}

func s3AccountDelete(account *S3Account) (error) {
	err := dbS3SetState(account, S3StateInactive, bson.M{"ref": 0})
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	dbS3Remove(account)
	log.Debugf("s3: Deleted %s", infoLong(account))
	return nil
}

func s3IamInsert(account *S3Account, policy *S3Policy) (*S3Iam, error) {
	var err error

	iam := &S3Iam {
		ObjID:		bson.NewObjectId(),
		State:		S3StateNone,

		AccountObjID:	account.ObjID,

		CreationTime:	time.Now().Format(time.RFC3339),
		User:		"user" + genKey(8, AccessKeyLetters),
	}

	if policy != nil { iam.Policy = *policy }

	if err = account.RefInc(); err != nil {
		return nil, err
	}

	if err = dbS3Insert(iam); err != nil {
		account.RefDec()
		return nil, err
	}

	if err = dbS3SetState(iam, S3StateActive, nil); err != nil {
		account.RefDec()
		dbS3Remove(iam)
		return nil, err
	}

	log.Debugf("s3: Inserted %s", infoLong(iam))
	return iam, nil
}

func s3IamDelete(iam *S3Iam) (error) {
	var account S3Account
	var err error

	err = dbS3SetState(iam, S3StateInactive, nil)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}
		return err
	}

	query := bson.M{ "_id": iam.AccountObjID, "state": S3StateActive }
	err = dbS3FindOne(query, &account)
	if err != nil {
		if err != mgo.ErrNotFound {
			return err
		}
	} else {
		if err = account.RefDec(); err != nil {
			return err
		}
	}

	dbS3Remove(iam)
	log.Debugf("s3: Deleted %s", infoLong(iam))
	return nil
}

func s3LookupIam(query bson.M) ([]S3Iam, error) {
	var res []S3Iam

	err := dbS3FindAll(query, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (akey *S3AccessKey) s3IamFind() (*S3Iam, error) {
	query := bson.M{"_id": akey.IamObjID, "state": S3StateActive }
	iams, err := s3LookupIam(query)
	if err != nil {
		return nil, err
	}
	return &iams[0], nil
}

func (account *S3Account) NamespaceID() string {
	return sha256sum([]byte(account.Namespace))
}
