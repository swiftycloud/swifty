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

func BIDFromNames(namespace, bucket string) string {
	return sha256sum([]byte(namespace + bucket))
}

// Bucket pool name and index in DB for lookup
func (iam *S3Iam)BucketBID(bname string) string {
	return BIDFromNames(iam.Namespace, bname)
}

// Object key in backend and index in DB for lookup
func (bucket *S3Bucket)ObjectBID(oname string, version int) string {
	return bucket.BackendID + "-" + strconv.Itoa(version) + "-" + oname
}