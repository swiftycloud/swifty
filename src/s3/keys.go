package main

import (
	"gopkg.in/mgo.v2/bson"
	"fmt"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"../common/crypto"
)

type S3AccessKey struct {
	ObjID				bson.ObjectId	`json:"_id,omitempty" bson:"_id,omitempty"`
	AccessKeyID			string		`json:"access-key-id" bson:"access-key-id"`
	AccessKeySecret			string		`json:"access-key-secret" bson:"access-key-secret"`
	Status				uint32		`json:"status,omitempty" bson:"status,omitempty"`
	Namespace			string		`json:"namespace,omitempty" bson:"namespace,omitempty"`
}

const (
	S3KeyStatusInActive		= 0
	S3KeyStatusActivePlain		= 1
	S3KeyStatusActive		= 2
)

// use swifty-s3
// db.S3AccessKeys.insert({"_id":ObjectId("5a16ccdbb3e8ee4bdf83da35"),"key-id":ObjectId("5a16ccd7b3e8ee4bdf83da34"),"access-key-id":"6DLA43X797XL2I42IJ33","access-key-secret":"AJwz9vZpdnz6T5TqEDQOEFos6wxxCnW0qwLQeDcB"})

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

//
// Keys operation should not report any errors,
// for security reason.
//

func genNewAccessKey(namespace string) (*S3AccessKey, error) {
	akey := S3AccessKey {
		ObjID:			bson.NewObjectId(),
		AccessKeyID:		genKey(20, AccessKeyLetters),
		AccessKeySecret:	genKey(40, SecretKeyLetters),
		Status:			S3KeyStatusActive,
		Namespace:		namespace,
	}

	if akey.AccessKeyID == "" ||
		akey.AccessKeySecret == "" ||
		akey.Namespace == "" {
		return nil, fmt.Errorf("s3: Can't generate keys")
	}

	// The secret key will be encoded here
	err := dbInsertAccessKey(&akey)
	if err != nil {
		return nil, err
	}

	if S3ModeDevel {
		log.Debugf("genNewAccessKey: akey %v", akey)
	}
	return &akey, nil
}

func BIDFromNames(namespace, bucket string) string {
	/*
	 * BID stands for backend-id and is a unique identifier
	 * in the storage. For CEPH case this is pool ID and
	 * since all users live in a plain pool namespace, it
	 * should be unique across users and their buckets.
	 */
	h := sha256.New()
	h.Write([]byte(namespace + "::" + bucket))
	return hex.EncodeToString(h.Sum(nil))
}

func (akey *S3AccessKey)BucketBID(bname string) string {
	return BIDFromNames(akey.Namespace, bname)
}

func (akey *S3AccessKey)NamespaceID() string {
	h := sha256.New()
	h.Write([]byte(akey.Namespace))
	return hex.EncodeToString(h.Sum(nil))
}

func (akey *S3AccessKey)FindBuckets() ([]S3Bucket, error) {
	var res []S3Bucket

	query := bson.M{"nsid": akey.NamespaceID()}

	err := dbS3FindAll(query, &res)
	if err != nil {
		return nil, err
	}

	return res, nil
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

		sec, err = swycrypt.DecryptString(s3SecKey, akey.AccessKeySecret)
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

func dbInsertAccessKey(akey *S3AccessKey) (error) {
	AccessKeySecret, err := swycrypt.EncryptString(s3SecKey, akey.AccessKeySecret)
	if err != nil {
		return err
	}

	akey_encoded := *akey
	akey_encoded.AccessKeySecret = AccessKeySecret

	err = dbSession.DB(dbName).C(DBColS3AccessKeys).Insert(&akey_encoded)
	if err != nil {
		log.Errorf("dbInsertAccessKey: Can't insert akey %v: %s",
				akey, err.Error())
		return err
	}

	log.Debugf("dbInsertAccessKey: akey %v", akey)
	return nil
}

func dbRemoveAccessKey(AccessKeyID string) (error) {
	var err error

	akey, err := dbLookupAccessKey(AccessKeyID)
	if err != nil {
		log.Debugf("dbRemoveAccessKey: Can't find for %v", AccessKeyID)
		return err
	}

	id := bson.M{"_id": akey.ObjID}
	err = dbSession.DB(dbName).C(DBColS3AccessKeys).Remove(id)
	if err != nil {
		log.Debugf("dbRemoveAccessKey: Can't remove akey %v: %s",
				akey, err.Error())
		return err
	}

	log.Debugf("dbRemoveAccessKey: akey %v", akey)
	return nil
}
