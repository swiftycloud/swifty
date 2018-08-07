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
	S3P_HeadBucket			= 0
	S3P_ListAllMyBuckets			= 1
	S3P_ListBucket			= 2
	S3P_ListObjects			= 3

	// Read
	S3P_GetAccelerateConfiguration	= 16
	S3P_GetAnalyticsConfiguration	= 17
	S3P_GetBucketAcl			= 18
	S3P_GetBucketCORS			= 19
	S3P_GetBucketLocation		= 20
	S3P_GetBucketLogging			= 21
	S3P_GetBucketNotification		= 22
	S3P_GetBucketPolicy			= 23
	S3P_GetBucketRequestPayment		= 24
	S3P_GetBucketTagging			= 25
	S3P_GetBucketVersioning		= 26
	S3P_GetBucketWebsite			= 27
	S3P_GetInventoryConfiguration	= 28
	S3P_GetIpConfiguration		= 29
	S3P_GetLifecycleConfiguration	= 30
	S3P_GetMetricsConfiguration		= 31
	S3P_GetObject			= 32
	S3P_GetObjectAcl			= 33
	S3P_GetObjectTagging			= 34
	S3P_GetObjectTorrent			= 35
	S3P_GetObjectVersion			= 36
	S3P_GetObjectVersionAcl		= 37
	S3P_GetObjectVersionForReplication	= 38
	S3P_GetObjectVersionTagging		= 39
	S3P_GetObjectVersionTorrent		= 40
	S3P_GetReplicationConfiguration	= 41
	S3P_ListBucketByTags			= 42
	S3P_ListBucketMultipartUploads	= 43
	S3P_ListBucketVersions		= 44
	S3P_ListMultipartUploadParts		= 45

	//
	// qword bound, index 1
	//

	// Write
	S3P_AbortMultipartUpload		= 64
	S3P_CreateBucket			= 65
	S3P_DeleteBucket			= 66
	S3P_DeleteBucketWebsite		= 67
	S3P_DeleteObject			= 68
	S3P_DeleteObjectTagging		= 69
	S3P_DeleteObjectVersion		= 70
	S3P_DeleteObjectVersionTagging	= 71
	S3P_PutAccelerateConfiguration	= 72
	S3P_PutAnalyticsConfiguration	= 73
	S3P_PutBucketCORS			= 74
	S3P_PutBucketLogging			= 75
	S3P_PutBucketNotification		= 76
	S3P_PutBucketRequestPayment		= 77
	S3P_PutBucketTagging			= 78
	S3P_PutBucketVersioning		= 79
	S3P_PutBucketWebsite			= 80
	S3P_PutInventoryConfiguration	= 81
	S3P_PutIpConfiguration		= 82
	S3P_PutLifecycleConfiguration	= 83
	S3P_PutMetricsConfiguration		= 84
	S3P_PutObject			= 85
	S3P_PutObjectTagging			= 86
	S3P_PutObjectVersionTagging		= 87
	S3P_PutReplicationConfiguration	= 88
	S3P_ReplicateDelete			= 89
	S3P_ReplicateObject			= 90
	S3P_ReplicateTags			= 91
	S3P_RestoreObject			= 92

	// Permissions management":
	S3P_DeleteBucketPolicy		= 93
	S3P_ObjectOwnerOverrideToBucketOwner	= 94
	S3P_PutBucketAcl			= 95
	S3P_PutBucketPolicy			= 96
	S3P_PutObjectAcl			= 97
	S3P_PutObjectVersionAcl		= 98

	S3P_All				= 127
)

var S3PolicyAction_Map = map[string]uint32 {
	// List
	"s3:HeadBucket":			S3P_HeadBucket,
	"s3:ListAllMyBuckets":			S3P_ListAllMyBuckets,
	"s3:ListBucket":			S3P_ListBucket,
	"s3:ListObjects":			S3P_ListObjects,

	// Read
	"s3:GetAccelerateConfiguration":	S3P_GetAccelerateConfiguration,
	"s3:GetAnalyticsConfiguration":		S3P_GetAnalyticsConfiguration,
	"s3:GetBucketAcl":			S3P_GetBucketAcl,
	"s3:GetBucketCORS":			S3P_GetBucketCORS,
	"s3:GetBucketLocation":			S3P_GetBucketLocation,
	"s3:GetBucketLogging":			S3P_GetBucketLogging,
	"s3:GetBucketNotification":		S3P_GetBucketNotification,
	"s3:GetBucketPolicy":			S3P_GetBucketPolicy,
	"s3:GetBucketRequestPayment":		S3P_GetBucketRequestPayment,
	"s3:GetBucketTagging":			S3P_GetBucketTagging,
	"s3:GetBucketVersioning":		S3P_GetBucketVersioning,
	"s3:GetBucketWebsite":			S3P_GetBucketWebsite,
	"s3:GetInventoryConfiguration":		S3P_GetInventoryConfiguration,
	"s3:GetIpConfiguration":		S3P_GetIpConfiguration,
	"s3:GetLifecycleConfiguration":		S3P_GetLifecycleConfiguration,
	"s3:GetMetricsConfiguration":		S3P_GetMetricsConfiguration,
	"s3:GetObject":				S3P_GetObject,
	"s3:GetObjectAcl":			S3P_GetObjectAcl,
	"s3:GetObjectTagging":			S3P_GetObjectTagging,
	"s3:GetObjectTorrent":			S3P_GetObjectTorrent,
	"s3:GetObjectVersion":			S3P_GetObjectVersion,
	"s3:GetObjectVersionAcl":		S3P_GetObjectVersionAcl,
	"s3:GetObjectVersionForReplication":	S3P_GetObjectVersionForReplication,
	"s3:GetObjectVersionTagging":		S3P_GetObjectVersionTagging,
	"s3:GetObjectVersionTorrent":		S3P_GetObjectVersionTorrent,
	"s3:GetReplicationConfiguration":	S3P_GetReplicationConfiguration,
	"s3:ListBucketByTags":			S3P_ListBucketByTags,
	"s3:ListBucketMultipartUploads":	S3P_ListBucketMultipartUploads,
	"s3:ListBucketVersions":		S3P_ListBucketVersions,
	"s3:ListMultipartUploadParts":		S3P_ListMultipartUploadParts,

	// Write
	"s3:AbortMultipartUpload":		S3P_AbortMultipartUpload,
	"s3:CreateBucket":			S3P_CreateBucket,
	"s3:DeleteBucket":			S3P_DeleteBucket,
	"s3:DeleteBucketWebsite":		S3P_DeleteBucketWebsite,
	"s3:DeleteObject":			S3P_DeleteObject,
	"s3:DeleteObjectTagging":		S3P_DeleteObjectTagging,
	"s3:DeleteObjectVersion":		S3P_DeleteObjectVersion,
	"s3:DeleteObjectVersionTagging":	S3P_DeleteObjectVersionTagging,
	"s3:PutAccelerateConfiguration":	S3P_PutAccelerateConfiguration,
	"s3:PutAnalyticsConfiguration":		S3P_PutAnalyticsConfiguration,
	"s3:PutBucketCORS":			S3P_PutBucketCORS,
	"s3:PutBucketLogging":			S3P_PutBucketLogging,
	"s3:PutBucketNotification":		S3P_PutBucketNotification,
	"s3:PutBucketRequestPayment":		S3P_PutBucketRequestPayment,
	"s3:PutBucketTagging":			S3P_PutBucketTagging,
	"s3:PutBucketVersioning":		S3P_PutBucketVersioning,
	"s3:PutBucketWebsite":			S3P_PutBucketWebsite,
	"s3:PutInventoryConfiguration":		S3P_PutInventoryConfiguration,
	"s3:PutIpConfiguration":		S3P_PutIpConfiguration,
	"s3:PutLifecycleConfiguration":		S3P_PutLifecycleConfiguration,
	"s3:PutMetricsConfiguration":		S3P_PutMetricsConfiguration,
	"s3:PutObject":				S3P_PutObject,
	"s3:PutObjectTagging":			S3P_PutObjectTagging,
	"s3:PutObjectVersionTagging":		S3P_PutObjectVersionTagging,
	"s3:PutReplicationConfiguration":	S3P_PutReplicationConfiguration,
	"s3:ReplicateDelete":			S3P_ReplicateDelete,
	"s3:ReplicateObject":			S3P_ReplicateObject,
	"s3:ReplicateTags":			S3P_ReplicateTags,
	"s3:RestoreObject":			S3P_RestoreObject,

	// Permissions management
	"s3:DeleteBucketPolicy":		S3P_DeleteBucketPolicy,
	"s3:ObjectOwnerOverrideToBucketOwner":	S3P_ObjectOwnerOverrideToBucketOwner,
	"s3:PutBucketAcl":			S3P_PutBucketAcl,
	"s3:PutBucketPolicy":			S3P_PutBucketPolicy,
	"s3:PutObjectAcl":			S3P_PutObjectAcl,
	"s3:PutObjectVersionAcl":		S3P_PutObjectVersionAcl,

	"s3:*":					S3P_All,
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

var S3PolicyActions_AllSet = ActionBits{ 0xffffffffffffffff, 0xffffffffffffffff }
var S3PolicyActions_PerBucket = ActionBits{
		0xffffffffffffffff ^
			((1 << (S3P_ListAllMyBuckets - 0))),
		0xffffffffffffffff ^
			((1 << (S3P_CreateBucket - 64))			|
			 (1 << (S3P_DeleteBucket - 64))			|
			 (1 << (S3P_DeleteBucketPolicy - 64))		|
			 (1 << (S3P_ObjectOwnerOverrideToBucketOwner - 64))),
}
var S3PolicyActions_Web = ActionBits{
		(1 << S3P_GetObject),
		0,
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
		Action: S3PolicyActions_AllSet.toMgo(),
		Resource: []string{ Resourse_Any },
	}
}

func getBucketPolicy(bname string) *S3Policy {
	return &S3Policy {
		Effect: Policy_Allow,
		Action: S3PolicyActions_PerBucket.toMgo(),
		Resource: []string{ bname },
	}
}

func getWebPolicy(bname string) *S3Policy {
	return &S3Policy {
		Effect: Policy_Allow,
		Action: S3PolicyActions_Web.toMgo(),
		Resource: []string{ bname },
	}
}

func (policy *S3Policy) isCanned() bool {
	return policy != nil && policy.Effect == Policy_Allow && len(policy.Resource) > 0
}

func (policy *S3Policy) isRoot() bool {
	if policy.isCanned() {
		// Root key, can do everything
		if policy.Action == S3PolicyActions_AllSet.toMgo() {
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
	if len(policy.Resource) > 0 && policy.Resource[0] == Resourse_Any {
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
