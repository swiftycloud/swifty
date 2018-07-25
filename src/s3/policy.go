package main

import (
	"encoding/binary"
	"reflect"
	"fmt"
)

// Effect element
const (
	Policy_Allow		= "Allow"
	Policy_Deny		= "Deny"
)

const (
	//
	// qword bound, index 0
	//

	// List
	S3PolicyAction_HeadBucket			= 0
	S3PolicyAction_ListAllMyBuckets			= 1
	S3PolicyAction_ListBucket			= 2
	S3PolicyAction_ListObjects			= 3

	// Read
	S3PolicyAction_GetAccelerateConfiguration	= 16
	S3PolicyAction_GetAnalyticsConfiguration	= 17
	S3PolicyAction_GetBucketAcl			= 18
	S3PolicyAction_GetBucketCORS			= 19
	S3PolicyAction_GetBucketLocation		= 20
	S3PolicyAction_GetBucketLogging			= 21
	S3PolicyAction_GetBucketNotification		= 22
	S3PolicyAction_GetBucketPolicy			= 23
	S3PolicyAction_GetBucketRequestPayment		= 24
	S3PolicyAction_GetBucketTagging			= 25
	S3PolicyAction_GetBucketVersioning		= 26
	S3PolicyAction_GetBucketWebsite			= 27
	S3PolicyAction_GetInventoryConfiguration	= 28
	S3PolicyAction_GetIpConfiguration		= 29
	S3PolicyAction_GetLifecycleConfiguration	= 30
	S3PolicyAction_GetMetricsConfiguration		= 31
	S3PolicyAction_GetObject			= 32
	S3PolicyAction_GetObjectAcl			= 33
	S3PolicyAction_GetObjectTagging			= 34
	S3PolicyAction_GetObjectTorrent			= 35
	S3PolicyAction_GetObjectVersion			= 36
	S3PolicyAction_GetObjectVersionAcl		= 37
	S3PolicyAction_GetObjectVersionForReplication	= 38
	S3PolicyAction_GetObjectVersionTagging		= 39
	S3PolicyAction_GetObjectVersionTorrent		= 40
	S3PolicyAction_GetReplicationConfiguration	= 41
	S3PolicyAction_ListBucketByTags			= 42
	S3PolicyAction_ListBucketMultipartUploads	= 43
	S3PolicyAction_ListBucketVersions		= 44
	S3PolicyAction_ListMultipartUploadParts		= 45

	//
	// qword bound, index 1
	//

	// Write
	S3PolicyAction_AbortMultipartUpload		= 64
	S3PolicyAction_CreateBucket			= 65
	S3PolicyAction_DeleteBucket			= 66
	S3PolicyAction_DeleteBucketWebsite		= 67
	S3PolicyAction_DeleteObject			= 68
	S3PolicyAction_DeleteObjectTagging		= 69
	S3PolicyAction_DeleteObjectVersion		= 70
	S3PolicyAction_DeleteObjectVersionTagging	= 71
	S3PolicyAction_PutAccelerateConfiguration	= 72
	S3PolicyAction_PutAnalyticsConfiguration	= 73
	S3PolicyAction_PutBucketCORS			= 74
	S3PolicyAction_PutBucketLogging			= 75
	S3PolicyAction_PutBucketNotification		= 76
	S3PolicyAction_PutBucketRequestPayment		= 77
	S3PolicyAction_PutBucketTagging			= 78
	S3PolicyAction_PutBucketVersioning		= 79
	S3PolicyAction_PutBucketWebsite			= 80
	S3PolicyAction_PutInventoryConfiguration	= 81
	S3PolicyAction_PutIpConfiguration		= 82
	S3PolicyAction_PutLifecycleConfiguration	= 83
	S3PolicyAction_PutMetricsConfiguration		= 84
	S3PolicyAction_PutObject			= 85
	S3PolicyAction_PutObjectTagging			= 86
	S3PolicyAction_PutObjectVersionTagging		= 87
	S3PolicyAction_PutReplicationConfiguration	= 88
	S3PolicyAction_ReplicateDelete			= 89
	S3PolicyAction_ReplicateObject			= 90
	S3PolicyAction_ReplicateTags			= 91
	S3PolicyAction_RestoreObject			= 92

	// Permissions management":
	S3PolicyAction_DeleteBucketPolicy		= 93
	S3PolicyAction_ObjectOwnerOverrideToBucketOwner	= 94
	S3PolicyAction_PutBucketAcl			= 95
	S3PolicyAction_PutBucketPolicy			= 96
	S3PolicyAction_PutObjectAcl			= 97
	S3PolicyAction_PutObjectVersionAcl		= 98

	S3PolicyAction_All				= 127
)

var S3PolicyAction_Map = map[string]uint32 {
	// List
	"s3:HeadBucket":			S3PolicyAction_HeadBucket,
	"s3:ListAllMyBuckets":			S3PolicyAction_ListAllMyBuckets,
	"s3:ListBucket":			S3PolicyAction_ListBucket,
	"s3:ListObjects":			S3PolicyAction_ListObjects,

	// Read
	"s3:GetAccelerateConfiguration":	S3PolicyAction_GetAccelerateConfiguration,
	"s3:GetAnalyticsConfiguration":		S3PolicyAction_GetAnalyticsConfiguration,
	"s3:GetBucketAcl":			S3PolicyAction_GetBucketAcl,
	"s3:GetBucketCORS":			S3PolicyAction_GetBucketCORS,
	"s3:GetBucketLocation":			S3PolicyAction_GetBucketLocation,
	"s3:GetBucketLogging":			S3PolicyAction_GetBucketLogging,
	"s3:GetBucketNotification":		S3PolicyAction_GetBucketNotification,
	"s3:GetBucketPolicy":			S3PolicyAction_GetBucketPolicy,
	"s3:GetBucketRequestPayment":		S3PolicyAction_GetBucketRequestPayment,
	"s3:GetBucketTagging":			S3PolicyAction_GetBucketTagging,
	"s3:GetBucketVersioning":		S3PolicyAction_GetBucketVersioning,
	"s3:GetBucketWebsite":			S3PolicyAction_GetBucketWebsite,
	"s3:GetInventoryConfiguration":		S3PolicyAction_GetInventoryConfiguration,
	"s3:GetIpConfiguration":		S3PolicyAction_GetIpConfiguration,
	"s3:GetLifecycleConfiguration":		S3PolicyAction_GetLifecycleConfiguration,
	"s3:GetMetricsConfiguration":		S3PolicyAction_GetMetricsConfiguration,
	"s3:GetObject":				S3PolicyAction_GetObject,
	"s3:GetObjectAcl":			S3PolicyAction_GetObjectAcl,
	"s3:GetObjectTagging":			S3PolicyAction_GetObjectTagging,
	"s3:GetObjectTorrent":			S3PolicyAction_GetObjectTorrent,
	"s3:GetObjectVersion":			S3PolicyAction_GetObjectVersion,
	"s3:GetObjectVersionAcl":		S3PolicyAction_GetObjectVersionAcl,
	"s3:GetObjectVersionForReplication":	S3PolicyAction_GetObjectVersionForReplication,
	"s3:GetObjectVersionTagging":		S3PolicyAction_GetObjectVersionTagging,
	"s3:GetObjectVersionTorrent":		S3PolicyAction_GetObjectVersionTorrent,
	"s3:GetReplicationConfiguration":	S3PolicyAction_GetReplicationConfiguration,
	"s3:ListBucketByTags":			S3PolicyAction_ListBucketByTags,
	"s3:ListBucketMultipartUploads":	S3PolicyAction_ListBucketMultipartUploads,
	"s3:ListBucketVersions":		S3PolicyAction_ListBucketVersions,
	"s3:ListMultipartUploadParts":		S3PolicyAction_ListMultipartUploadParts,

	// Write
	"s3:AbortMultipartUpload":		S3PolicyAction_AbortMultipartUpload,
	"s3:CreateBucket":			S3PolicyAction_CreateBucket,
	"s3:DeleteBucket":			S3PolicyAction_DeleteBucket,
	"s3:DeleteBucketWebsite":		S3PolicyAction_DeleteBucketWebsite,
	"s3:DeleteObject":			S3PolicyAction_DeleteObject,
	"s3:DeleteObjectTagging":		S3PolicyAction_DeleteObjectTagging,
	"s3:DeleteObjectVersion":		S3PolicyAction_DeleteObjectVersion,
	"s3:DeleteObjectVersionTagging":	S3PolicyAction_DeleteObjectVersionTagging,
	"s3:PutAccelerateConfiguration":	S3PolicyAction_PutAccelerateConfiguration,
	"s3:PutAnalyticsConfiguration":		S3PolicyAction_PutAnalyticsConfiguration,
	"s3:PutBucketCORS":			S3PolicyAction_PutBucketCORS,
	"s3:PutBucketLogging":			S3PolicyAction_PutBucketLogging,
	"s3:PutBucketNotification":		S3PolicyAction_PutBucketNotification,
	"s3:PutBucketRequestPayment":		S3PolicyAction_PutBucketRequestPayment,
	"s3:PutBucketTagging":			S3PolicyAction_PutBucketTagging,
	"s3:PutBucketVersioning":		S3PolicyAction_PutBucketVersioning,
	"s3:PutBucketWebsite":			S3PolicyAction_PutBucketWebsite,
	"s3:PutInventoryConfiguration":		S3PolicyAction_PutInventoryConfiguration,
	"s3:PutIpConfiguration":		S3PolicyAction_PutIpConfiguration,
	"s3:PutLifecycleConfiguration":		S3PolicyAction_PutLifecycleConfiguration,
	"s3:PutMetricsConfiguration":		S3PolicyAction_PutMetricsConfiguration,
	"s3:PutObject":				S3PolicyAction_PutObject,
	"s3:PutObjectTagging":			S3PolicyAction_PutObjectTagging,
	"s3:PutObjectVersionTagging":		S3PolicyAction_PutObjectVersionTagging,
	"s3:PutReplicationConfiguration":	S3PolicyAction_PutReplicationConfiguration,
	"s3:ReplicateDelete":			S3PolicyAction_ReplicateDelete,
	"s3:ReplicateObject":			S3PolicyAction_ReplicateObject,
	"s3:ReplicateTags":			S3PolicyAction_ReplicateTags,
	"s3:RestoreObject":			S3PolicyAction_RestoreObject,

	// Permissions management
	"s3:DeleteBucketPolicy":		S3PolicyAction_DeleteBucketPolicy,
	"s3:ObjectOwnerOverrideToBucketOwner":	S3PolicyAction_ObjectOwnerOverrideToBucketOwner,
	"s3:PutBucketAcl":			S3PolicyAction_PutBucketAcl,
	"s3:PutBucketPolicy":			S3PolicyAction_PutBucketPolicy,
	"s3:PutObjectAcl":			S3PolicyAction_PutObjectAcl,
	"s3:PutObjectVersionAcl":		S3PolicyAction_PutObjectVersionAcl,

	"s3:*":					S3PolicyAction_All,
}

type ActionBits		[2]uint64
type ActionBitsMgo	[16]byte

func (v ActionBits) toMgo() ActionBitsMgo {
	var b ActionBitsMgo

	binary.LittleEndian.PutUint64(b[0:], v[0])
	binary.LittleEndian.PutUint64(b[8:], v[1])

	return b
}

func (v ActionBitsMgo) toSwy() ActionBits {
	var b ActionBits

	b[0] = binary.LittleEndian.Uint64(v[0:])
	b[1] = binary.LittleEndian.Uint64(v[8:])

	return b
}

var S3PolicyAction_AllSet = ActionBits{ 0xffffffffffffffff, 0xffffffffffffffff }
var S3PolicyAction_PerBucket = ActionBits{
		0xffffffffffffffff ^
			((1 << (S3PolicyAction_ListAllMyBuckets - 0))),
		0xffffffffffffffff ^
			((1 << (S3PolicyAction_CreateBucket - 64))			|
			 (1 << (S3PolicyAction_DeleteBucket - 64))			|
			 (1 << (S3PolicyAction_DeleteBucketPolicy - 64))		|
			 (1 << (S3PolicyAction_ObjectOwnerOverrideToBucketOwner - 64))),
}

// Most permissive mode
const (
	Resourse_Any				= "*"
)

type S3Policy struct {
	Effect		string		`bson:"effect,omitempty"`
	Action		ActionBitsMgo	`bson:"action,omitempty"`
	Resource	[]string	`bson:"resource,omitempty"`
}

func (policy *S3Policy) infoLong() string {
	if policy != nil {
		if len(policy.Resource) > 0 {
			return fmt.Sprintf("% x/%s",
				policy.Action.toSwy(),
				policy.Resource[0])
		}
	}
	return "nil"
}

func getRootPolicy() *S3Policy {
	// If changing modify isRoot as well
	return &S3Policy {
		Effect: Policy_Allow,
		Action: S3PolicyAction_AllSet.toMgo(),
		Resource: []string{ Resourse_Any },
	}
}

func getBucketPolicy(bname string) *S3Policy {
	return &S3Policy {
		Effect: Policy_Allow,
		Action: S3PolicyAction_PerBucket.toMgo(),
		Resource: []string{ bname },
	}
}

func (policy *S3Policy) isCanned() bool {
	return policy != nil && policy.Effect == Policy_Allow && len(policy.Resource) > 0
}

func (policy *S3Policy) isRoot() bool {
	if policy.isCanned() {
		// Root key, can do everything
		if policy.Action == S3PolicyAction_AllSet.toMgo() {
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
	if policy.isRoot() {
		return true
	}

	for _, x := range policy.Resource {
		if x == resource {
			return true
		}
	}

	return false
}

func (policy *S3Policy) allowed(action int) bool {
	bits := policy.Action.toSwy()
	if action < 64 {
		return bits[0] & (1 << uint(action)) != 0
	} else {
		return bits[1] & (1 << uint((action - 64))) != 0
	}
}
