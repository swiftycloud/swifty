package main

import (
	"gopkg.in/mgo.v2/bson"
	"fmt"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

type S3AccessKey struct {
	ObjID				bson.ObjectId	`json:"_id,omitempty" bson:"_id,omitempty"`
	AccessKeyID			string		`json:"access-key-id" bson:"access-key-id"`
	AccessKeySecret			string		`json:"access-key-secret" bson:"access-key-secret"`
	Kind				uint32		`json:"kind,omitempty" bson:"kind,omitempty"`
	Status				uint32		`json:"status,omitempty" bson:"status,omitempty"`
	Namespace			string		`json:"namespace,omitempty" bson:"namespace,omitempty"`
}

const (
	S3KeyKindUserAccessKey		= 1
	S3KeyKindAdminAccessKey		= 2

	S3KeyStatusInActive		= 0
	S3KeyStatusActivePlain		= 1
	S3KeyStatusActive		= 2
)

// use swifty-s3
// db.S3Keys.insert({"_id":ObjectId("5a16ccd7b3e8ee4bdf83da34"),"key-id":ObjectId("5a16ccdbb3e8ee4bdf83da35"),"kind":1,"status":1})
// db.S3AccessKeys.insert({"_id":ObjectId("5a16ccdbb3e8ee4bdf83da35"),"key-id":ObjectId("5a16ccd7b3e8ee4bdf83da34"),"access-key-id":"6DLA43X797XL2I42IJ33","access-key-secret":"AJwz9vZpdnz6T5TqEDQOEFos6wxxCnW0qwLQeDcB"})

var AccessKeyLetters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
var SecretKeyLetters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")

func genKey(length int, dict []byte) (string, error) {
	idx := make([]byte, length)
	pass:= make([]byte, length)
	_, err := rand.Read(idx)
	if err != nil {
		return "", err
	}

	for i, j := range idx {
		pass[i] = dict[int(j) % len(dict)]
	}

	return string(pass), nil
}

func genAccessKeyPair() (string, string) {
	akey, _ := genKey(20, AccessKeyLetters)
	skey, _ := genKey(40, SecretKeyLetters)

	return akey, skey
}

//
// Keys operation should not report any errors,
// for security reason.
//

func (akey *S3AccessKey)BucketBID(bucket_name string) string {
	/*
	 * BID stands for backend-id and is a unique identifier
	 * in the storage. For CEPH case this is pool ID and
	 * since all users live in a plain pool namespace, it
	 * should be unique across users and their buckets.
	 */
	h := sha256.New()
	h.Write([]byte(akey.Namespace + "::" + bucket_name))
	return hex.EncodeToString(h.Sum(nil))
}

func (akey *S3AccessKey)FindDefaultBucket() (string, error) {
	var res S3Bucket

	regex := "^" + akey.Namespace + ".+"
	query := bson.M{"bid": bson.M{"$regex": bson.RegEx{regex, ""}}}

	err := dbS3FindOne(query, &res)
	if err != nil {
		return "", err
	}

	return res.Name, nil
}

func dbLookupAccessKey(AccessKeyId string) (*S3AccessKey, error) {
	var akey S3AccessKey
	var err error

	c := dbSession.DB(dbName).C(DBColS3AccessKeys)
	err = c.Find(bson.M{"access-key-id": AccessKeyId}).One(&akey)
	if err != nil {
		return nil, fmt.Errorf("Can't find access key '%s': %s", AccessKeyId, err.Error())
	}

	if akey.Status == S3KeyStatusActive {
		var sec string

		sec, err = DecryptString([]byte(s3Secrets[conf.SecKey]), akey.AccessKeySecret)
		if err != nil {
			return nil, err
		}

		akey.AccessKeySecret = sec
		return &akey, nil
	}

	if S3ModeDevel && (akey.Status == S3KeyStatusActivePlain) {
		return &akey, nil
	}

	return nil, fmt.Errorf("Access key %s is not active", AccessKeyId)
}

func dbInsertAccessKey(AccessKeyID, AccessKeySecret string, Kind uint32) (*S3AccessKey, error) {
	var err error

	status := uint32(S3KeyStatusActive)
	if S3ModeDevel {
		status = S3KeyStatusActivePlain
	} else {
		AccessKeySecret, err = EncryptString([]byte(s3Secrets[conf.SecKey]), AccessKeySecret)
		if err != nil {
			return nil, err
		}
	}

	akey := S3AccessKey {
		ObjID:			bson.NewObjectId(),
		AccessKeyID:		AccessKeyID,
		AccessKeySecret:	AccessKeySecret,
		Status:			status,
		Kind:			Kind,
	}

	err = dbSession.DB(dbName).C(DBColS3AccessKeys).Insert(&akey)
	if err != nil {
		log.Errorf("dbInsertAccessKey: Can't insert akey %v: %s",
				akey, err.Error())
		return nil, err
	}

	log.Debugf("dbInsertAccessKey: akey %v", akey)
	return &akey, nil
}

func dbRemoveAccessKey(AccessKeyID string) (error) {
	var err error

	akey, err := dbLookupAccessKey(AccessKeyID)
	if akey == nil || err != nil {
		log.Debugf("dbRemoveAccessKey: Can't find for %v", AccessKeyID)
		return nil
	}

	err = dbSession.DB(dbName).C(DBColS3AccessKeys).Remove(akey)
	if err != nil {
		log.Debugf("dbRemoveAccessKey: Can't remove akey %v: %s",
				akey, err.Error())
		return err
	}

	log.Debugf("dbRemoveAccessKey: akey %v", akey)
	return nil
}
