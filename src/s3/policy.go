package main

import (
	"reflect"
	"fmt"
)

// Effect element
const (
	Policy_Allow		= "Allow"
	Policy_Deny		= "Deny"
)

const (
	S3PolicyAction_AbortMultipartUpload		= uint64(1 <<  0)
	S3PolicyAction_DeleteObject			= uint64(1 <<  1)
	S3PolicyAction_DeleteObjectTagging		= uint64(1 <<  2)
	S3PolicyAction_DeleteObjectVersion		= uint64(1 <<  3)
	S3PolicyAction_DeleteObjectVersionTagging	= uint64(1 <<  4)
	S3PolicyAction_GetObject			= uint64(1 <<  5)
	S3PolicyAction_GetObjectAcl			= uint64(1 <<  6)
	S3PolicyAction_GetObjectTagging			= uint64(1 <<  7)
	S3PolicyAction_GetObjectTorrent			= uint64(1 <<  8)
	S3PolicyAction_GetObjectVersion			= uint64(1 <<  9)
	S3PolicyAction_GetObjectVersionAcl		= uint64(1 << 10)
	S3PolicyAction_GetObjectVersionTagging		= uint64(1 << 11)
	S3PolicyAction_GetObjectVersionTorrent		= uint64(1 << 12)
	S3PolicyAction_ListMultipartUploadParts		= uint64(1 << 13)
	S3PolicyAction_PutObject			= uint64(1 << 14)
	S3PolicyAction_PutObjectAcl			= uint64(1 << 15)
	S3PolicyAction_PutObjectTagging			= uint64(1 << 16)
	S3PolicyAction_PutObjectVersionAcl		= uint64(1 << 17)
	S3PolicyAction_PutObjectVersionTagging		= uint64(1 << 18)
	S3PolicyAction_RestoreObject			= uint64(1 << 19)

	S3PolicyAction_CreateBucket			= uint64(1 << 32)
	S3PolicyAction_DeleteBucket			= uint64(1 << 33)
	S3PolicyAction_ListBucket			= uint64(1 << 34)
	S3PolicyAction_ListBucketVersions		= uint64(1 << 35)
	S3PolicyAction_ListAllMyBuckets			= uint64(1 << 36)
	S3PolicyAction_ListBucketMultipartUploads	= uint64(1 << 37)

	S3PolicyAction_All				= uint64((1 << 63) - 1)
	S3PolicyAction_Mask				= uint64((1 << 63) - 1)
)

var S3PolicyAction_Map = map[string]uint64 {
	"s3:AbortMultipartUpload":	S3PolicyAction_AbortMultipartUpload,
	"s3:DeleteObject":		S3PolicyAction_DeleteObject,
	"s3:DeleteObjectTagging":	S3PolicyAction_DeleteObjectTagging,
	"s3:DeleteObjectVersion":	S3PolicyAction_DeleteObjectVersion,
	"s3:DeleteObjectVersionTagging":S3PolicyAction_DeleteObjectVersionTagging,
	"s3:GetObject":			S3PolicyAction_GetObject,
	"s3:GetObjectAcl":		S3PolicyAction_GetObjectAcl,
	"s3:GetObjectTagging":		S3PolicyAction_GetObjectTagging,
	"s3:GetObjectTorrent":		S3PolicyAction_GetObjectTorrent,
	"s3:GetObjectVersion":		S3PolicyAction_GetObjectVersion,
	"s3:GetObjectVersionAcl":	S3PolicyAction_GetObjectVersionAcl,
	"s3:GetObjectVersionTagging":	S3PolicyAction_GetObjectVersionTagging,
	"s3:GetObjectVersionTorrent":	S3PolicyAction_GetObjectVersionTorrent,
	"s3:ListMultipartUploadParts":	S3PolicyAction_ListMultipartUploadParts,
	"s3:PutObject":			S3PolicyAction_PutObject,
	"s3:PutObjectAcl":		S3PolicyAction_PutObjectAcl,
	"s3:PutObjectTagging":		S3PolicyAction_PutObjectTagging,
	"s3:PutObjectVersionAcl":	S3PolicyAction_PutObjectVersionAcl,
	"s3:PutObjectVersionTagging":	S3PolicyAction_PutObjectVersionTagging,
	"s3:RestoreObject":		S3PolicyAction_RestoreObject,

	"s3:CreateBucket":		S3PolicyAction_CreateBucket,
	"s3:DeleteBucket":		S3PolicyAction_DeleteBucket,
	"s3:ListBucket":		S3PolicyAction_ListBucket,
	"s3:ListBucketVersions":	S3PolicyAction_ListBucketVersions,
	"s3:ListAllMyBuckets":		S3PolicyAction_ListAllMyBuckets,
	"s3:ListBucketMultipartUploads":S3PolicyAction_ListBucketMultipartUploads,

	"s3:*":				S3PolicyAction_All,
}

// Most permissive mode
const (
	Resourse_Any				= "*"
)

type S3Policy struct {
	Effect				string		`bson:"effect,omitempty"`
	Action				[]uint64	`bson:"action,omitempty"`
	Resource			[]string	`bson:"resource,omitempty"`
}

func (policy *S3Policy) infoLong() string {
	if policy != nil {
		if len(policy.Action) > 0 && len(policy.Resource) > 0 {
			return fmt.Sprintf("%x/%s",
				policy.Action[0],
				policy.Resource[0])
		}
	}
	return "nil"
}

// If changing modify isRoot as well
var PolicyRoot = &S3Policy {
	Effect: Policy_Allow,
	Action: []uint64{ S3PolicyAction_All },
	Resource: []string{ Resourse_Any },
}

var PolicyBucketActions = []uint64 {
	S3PolicyAction_AbortMultipartUpload		|
	S3PolicyAction_DeleteObject			|
	S3PolicyAction_DeleteObjectTagging		|
	S3PolicyAction_DeleteObjectVersion		|
	S3PolicyAction_DeleteObjectVersionTagging	|
	S3PolicyAction_GetObject			|
	S3PolicyAction_GetObjectAcl			|
	S3PolicyAction_GetObjectTagging			|
	S3PolicyAction_GetObjectTorrent			|
	S3PolicyAction_GetObjectVersion			|
	S3PolicyAction_GetObjectVersionAcl		|
	S3PolicyAction_GetObjectVersionTagging		|
	S3PolicyAction_GetObjectVersionTorrent		|
	S3PolicyAction_ListMultipartUploadParts		|
	S3PolicyAction_PutObject			|
	S3PolicyAction_PutObjectAcl			|
	S3PolicyAction_PutObjectTagging			|
	S3PolicyAction_PutObjectVersionAcl		|
	S3PolicyAction_PutObjectVersionTagging		|
	S3PolicyAction_RestoreObject			|
	S3PolicyAction_ListBucket			|
	S3PolicyAction_ListBucketVersions		|
	S3PolicyAction_ListBucketMultipartUploads,
}

func (policy *S3Policy) isCanned() bool {
	if policy != nil {
		if policy.Effect == Policy_Allow {
			return len(policy.Action) > 0 &&
				len(policy.Resource) > 0
		}
	}
	return false
}

func (policy *S3Policy) isRoot() bool {
	if policy.isCanned() {
		// Root key, can do everything
		if policy.Action[0] == S3PolicyAction_All {
			if policy.Resource[0] == Resourse_Any {
				return true
			}
		}
	}
	return false
}

func (policy *S3Policy) isEqual(dst *S3Policy) bool {
	return reflect.DeepEqual(policy, dst)
}

func (policy *S3Policy) mayAccess(resource string) bool {
	if !policy.isRoot() {
		for _, x := range policy.Resource {
			if x == resource {
				return true
			}
		}
		return false
	}
	return true
}
