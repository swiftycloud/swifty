package main

import (
	"strconv"
)

//
// Name map convention in backend DB and a storage
// -----------------------------------------------
//
// On top level there is an "Account" which is bound to unique
// "Namespace" so every registered user has own namespace prefix
// identified names. The "account" may carry several IAMs which
// share "Namespace" but have different credentials.
//
// The "Namespace" consists of unique "Buckets", where each "Bucket"
// consists of "Objects" and "Object Parts" for multipart uploads.
//
// The "Objects" may be "versioned" or not depending on bucket settings.
// The "Object Parts" are never versioned and overwriten on demand.
//
// Basic scheme is the following
//
// Account -> IAM -> Namespace -> Bucket -> Object (version) | ObjectPart (part number)
//
// Buckets, objects, uploads are kept in separate collections but each
// collection is global so naming should be unique inside each.
//
// The backend storage carries buckets as "pools" thus their name should be
// unique, objects should take care of version in naming and object parts
// to consider part numbers.
//
// Cleanup rules
// -------------
//
// BD backend may fail in various operations, so to provide DB consistency
// sometime we need to run a "cleanup" action which would walk over collections
// and get rid of stale records.
//
// - S3Object
//	State::S3StateInactive|S3StateNone, CreationTime::<= some current time delta
// - S3Bucket
//	State::S3StateInactive|S3StateNone, CreationTime::<= some current time delta
// - S3ObjectData (FIXME)
//	RefID doesn't belong any of S3Object, S3UploadPart
// - S3UploadPart (FIXME)
// - S3Upload (FIXME)

// To distingush iam users as an index
func AccountUser(namespace, user string) string {
	return namespace + ":" + user
}

func (account *S3Account) IamUser(user string) string {
	return account.User + ":" + user
}

// Bucket grouping by namespace in DB for lookup
func (account *S3Account) NamespaceID() string {
	return sha256sum([]byte(account.Namespace))
}

// Bucket pool name and index in DB for lookup
func BCookie(namespace, bucket string) string {
	return sha256sum([]byte(namespace + bucket))
}

func (account *S3Account)BCookie(bname string) string {
	return BCookie(account.Namespace, bname)
}

// UploadID for DB lookup
func (bucket *S3Bucket)UploadUID(oname string) string {
	return sha256sum([]byte(bucket.BCookie + oname))
}

// Object key in backend and index in DB for lookup
func (bucket *S3Bucket)ObjectBID(oname string, version int) string {
	if version != 1 {
		log.Errorf("@verioning is not yet supported")
		version = 1
	}
	return sha256sum([]byte(bucket.BCookie + oname + strconv.Itoa(version)))
}

// Object part key in backend and index in DB for lookup
func (upload *S3Upload)ObjectBID(oname string, part int) string {
	return sha256sum([]byte(upload.UploadID + oname + strconv.Itoa(part)))
}

// Object data mapped to object/bucket or part/upload pairs
func ObjdBID(bucket_bid, object_bid string) string {
	return sha256sum([]byte(bucket_bid + object_bid))
}
