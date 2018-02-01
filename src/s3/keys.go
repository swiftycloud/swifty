package main

import (
	"gopkg.in/mgo.v2/bson"
	"fmt"
	"crypto/rand"
	"../common/crypto"
)

type S3AccessKey struct {
	ObjID				bson.ObjectId	`bson:"_id,omitempty"`
	MTime				int64		`bson:"mtime,omitempty"`
	State				uint32		`bson:"state"`

	IamID				bson.ObjectId	`bson:"iam-id,omitempty"`
	AccessKeyID			string		`bson:"access-key-id"`
	AccessKeySecret			string		`bson:"access-key-secret"`
	Status				uint32		`bson:"status,omitempty"`
	Bucket				string		`bson:"bucket,omitempty"`
}

const (
	S3KeyStatusInActive		= 0
	S3KeyStatusActivePlain		= 1
	S3KeyStatusActive		= 2
)

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

func (key *S3AccessKey)CheckBucketAccess(bname string) error {
	if key.Bucket == "" || key.Bucket == bname {
		return nil
	}

	return fmt.Errorf("Access to bucket %s prohibited", bname)
}

//
// Keys operation should not report any errors,
// for security reason.
//

func genNewAccessKey(namespace, bucket string) (*S3AccessKey, error) {
	akey := S3AccessKey {
		ObjID:			bson.NewObjectId(),
		State:			S3StateActive,

		AccessKeyID:		genKey(20, AccessKeyLetters),
		AccessKeySecret:	genKey(40, SecretKeyLetters),
		Status:			S3KeyStatusActive,
		Bucket:			bucket,
	}

	if akey.AccessKeyID == "" ||
		akey.AccessKeySecret == "" {
		return nil, fmt.Errorf("s3: Can't generate keys")
	}

	// No user/email for a while, autogenerated
	iam, err := s3IamGet(namespace, "", "")
	if err != nil {
		return nil, err
	}

	akey.IamID = iam.ObjID

	// The secret key will be encoded here
	err = dbInsertAccessKey(&akey)
	if err != nil {
		return nil, err
	}

	if S3ModeDevel {
		log.Debugf("genNewAccessKey: akey %v", akey)
	}
	return &akey, nil
}

func (iam *S3Iam)FindBuckets(akey *S3AccessKey) ([]S3Bucket, error) {
	var res []S3Bucket
	var err error

	if akey.Bucket != "" {
		var b S3Bucket
		err = dbS3FindOne(bson.M{"nsid": iam.NamespaceID, "name": akey.Bucket}, &b)
		res = []S3Bucket{b}
	} else {
		err = dbS3FindAll(bson.M{"nsid": iam.NamespaceID()}, &res)
	}

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

	err = dbS3Insert(&akey_encoded)
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

	err = dbS3Remove(akey)
	if err != nil {
		log.Debugf("dbRemoveAccessKey: Can't remove akey %v: %s",
				akey, err.Error())
		return err
	}

	log.Debugf("dbRemoveAccessKey: akey %v", akey)
	return nil
}
