package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"crypto/rand"
	"fmt"

	"../common/crypto"
)

type S3AccessKey struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	AccountObjID			bson.ObjectId	`bson:"account-id,omitempty"`
	IamObjID			bson.ObjectId	`bson:"iam-id,omitempty"`

	CreationTimestamp		int64		`bson:"creation-timestamp,omitempty"`
	ExpirationTimestamp		int64		`bson:"expiration-timestamp,omitempty"`

	AccessKeyID			string		`bson:"access-key-id"`
	AccessKeySecret			string		`bson:"access-key-secret"`
}

var AccessKeyLetters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
var SecretKeyLetters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")


func genKey(length int, dict []byte) (string) {
	idx := make([]byte, length)
	pass:= make([]byte, length)
	_, err := rand.Read(idx)
	if err != nil {
		return ""
	}

	for i, j := range idx {
		pass[i] = dict[int(j) % len(dict)]
	}

	return string(pass)
}

func (akey *S3AccessKey) Expired() bool {
	if akey.ExpirationTimestamp < S3TimeStampMax {
		return current_timestamp() > akey.ExpirationTimestamp
	}
	return false
}

//
// Keys operation should not report any errors,
// for security reason.
//

func getEndlessKey(account *S3Account, policy *S3Policy) (*S3AccessKey, error) {
	query := bson.M{ "account-id" : account.ObjID, "state": S3StateActive }
	if iams, err := s3LookupIam(query); err == nil {
		for _, iam := range iams {
			var akey S3AccessKey

			if !policy.isEqual(&iam.Policy) {
				continue
			}

			query = bson.M{"iam-id": iam.ObjID, "state": S3StateActive,
				"expiration-timestamp": bson.M{ "$eq": S3TimeStampMax }}
			if err = dbS3FindOne(query, &akey); err == nil {
				return &akey, nil
			}
		}
	}
	return nil, fmt.Errorf("No suitable iam found")
}

func genNewAccessKey(namespace, bucket string, lifetime uint32) (*S3AccessKey, error) {
	var timestamp_now, expired_when int64
	var akey *S3AccessKey
	var policy *S3Policy
	var iam *S3Iam
	var err error

	account, err := s3AccountInsert(namespace)
	if err != nil {
		return nil, err
	}

	if bucket != "" {
		policy = &S3Policy {
			Effect: Policy_Allow,
			Action: PolicyBucketActions,
			Resource: []string{ bucket },
		}
	} else {
		policy = PolicyRoot
	}

	timestamp_now = current_timestamp()
	if lifetime != 0 {
		expired_when = timestamp_now + int64(lifetime)
	} else {
		expired_when = S3TimeStampMax

		if akey, err = getEndlessKey(account, policy); err == nil {
			log.Debugf("s3: Found active key %s", infoLong(akey))
			return akey, nil
		}
	}

	iam, err = s3IamInsert(account, policy)
	if err != nil {
		goto out_1
	}

	akey = &S3AccessKey {
		ObjID:			bson.NewObjectId(),
		State:			S3StateNone,

		AccountObjID:		account.ObjID,
		IamObjID:		iam.ObjID,

		CreationTimestamp:	timestamp_now,
		ExpirationTimestamp:	expired_when,

		AccessKeyID:		genKey(20, AccessKeyLetters),
		AccessKeySecret:	genKey(40, SecretKeyLetters),
	}

	if akey.AccessKeyID == "" || akey.AccessKeySecret == "" {
		err = fmt.Errorf("s3: Can't generate keys")
		goto out_2
	}

	akey.AccessKeySecret, err = swycrypt.EncryptString(s3SecKey, akey.AccessKeySecret)
	if err != nil {
		goto out_2
	}

	if err = dbS3Insert(akey); err != nil {
		goto out_2
	}

	if err = dbS3SetState(akey, S3StateActive, nil); err != nil {
		goto out_3
	}

	log.Debugf("s3: Inserted %s", infoLong(akey))
	return akey, nil

out_3:
	dbS3Remove(akey)
out_2:
	s3IamDelete(iam)
out_1:
	s3AccountDelete(account)
	return nil, err
}

func (iam *S3Iam) FindBuckets() ([]S3Bucket, error) {
	var res []S3Bucket
	var err error

	account, err := iam.s3AccountLookup()
	if err != nil { return nil, err }

	err = dbS3FindAll(bson.M{"nsid": account.NamespaceID()}, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func s3DecryptAccessKeySecret(akey *S3AccessKey) string {
	sec, err := swycrypt.DecryptString(s3SecKey, akey.AccessKeySecret)
	if err != nil {
		return ""
	}
	return sec
}

func LookupAccessKey(AccessKeyId string) (*S3AccessKey, error) {
	var akey *S3AccessKey
	var err error

	if akey, err = dbLookupAccessKey(AccessKeyId); err == nil {
		if akey.Expired() {
			return nil, fmt.Errorf("Expired key")
		}
		return akey, nil
	}
	return nil, err
}

func dbLookupAccessKey(AccessKeyId string) (*S3AccessKey, error) {
	var akey S3AccessKey
	var err error

	err = dbS3FindOne(bson.M{"access-key-id": AccessKeyId, "state": S3StateActive }, &akey)
	if err == nil {
		return &akey, nil
	}

	return nil, err
}

func dbRemoveAccessKey(AccessKeyID string) (error) {
	var err error

	akey, err := dbLookupAccessKey(AccessKeyID)
	if err != nil {
		log.Debugf("s3: Can't find akey %s", AccessKeyID)
		return err
	}

	if iam, err := akey.s3IamFind(); err == nil {
		s3IamDelete(iam)
	}

	err = dbS3Remove(akey)
	if err != nil {
		log.Errorf("s3: Can't remove %s: %s",
			infoLong(akey), err.Error())
		return err
	}

	log.Debugf("s3: Removed akey %s", infoLong(akey))
	return nil
}

func gc_keys() {
	var akey S3AccessKey
	var pipe *mgo.Pipe
	var iter *mgo.Iter
	var err error

	query := bson.M{ "expiration-timestamp": bson.M{"$lt": current_timestamp()}}
	pipe = dbS3Pipe(&akey, []bson.M{{"$match": query}})

	iter = pipe.Iter()
	for iter.Next(&akey) {
		if iam, err := akey.s3IamFind(); err == nil {
			s3IamDelete(iam)
		}
		err = dbS3Remove(&akey)
		if err != nil {
			log.Errorf("s3: Can't remove %s: %s",
				infoLong(&akey), err.Error())
		} else {
			log.Debugf("s3: Removed expired akey %s",
				infoLong(&akey))
		}
	}
	iter.Close()
}
