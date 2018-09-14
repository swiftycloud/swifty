package main

import (
	"strconv"
	"../common"
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

// Object part key in backend and index in DB for lookup
func (upload *S3Upload)UCookie(oname string, part int) string {
	return xh.Sha256sum([]byte(upload.UploadID + oname + strconv.Itoa(part)))
}
