package s3mgo

import (
	"../../common"
	"strconv"
)

// To distingush iam users as an index
func AccountUser(namespace, user string) string {
	return namespace + ":" + user
}

func (account *S3Account) IamUser(user string) string {
	return account.User + ":" + user
}

// Bucket grouping by namespace in DB for lookup
func (account *S3Account) NamespaceID() string {
	return swy.Sha256sum([]byte(account.Namespace))
}

// Bucket pool name and index in DB for lookup
func BCookie(namespace, bucket string) string {
	return swy.Sha256sum([]byte(namespace + bucket))
}

func (account *S3Account)BCookie(bname string) string {
	return BCookie(account.Namespace, bname)
}

// UploadID for DB lookup
func (bucket *S3Bucket)UploadUID(oname string) string {
	return swy.Sha256sum([]byte(bucket.BCookie + oname))
}

// Object key in backend and index in DB for lookup
func (bucket *S3Bucket)OCookie(oname string, version int) string {
	if version != 1 {
		version = 1
	}
	return swy.Sha256sum([]byte(bucket.BCookie + oname + strconv.Itoa(version)))
}
