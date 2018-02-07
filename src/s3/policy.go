package main

// Effect element
const (
	Policy_Allow		= "Allow"
	Policy_Deny		= "Deny"
)

// Permissions for object operations
const (
	PermS3_AbortMultipartUpload		= "s3:AbortMultipartUpload"
	PermS3_DeleteObject			= "s3:DeleteObject"
	PermS3_DeleteObjectTagging		= "s3:DeleteObjectTagging"
	PermS3_DeleteObjectVersion		= "s3:DeleteObjectVersion"
	PermS3_DeleteObjectVersionTagging	= "s3:DeleteObjectVersionTagging"
	PermS3_GetObject			= "s3:GetObject"
	PermS3_GetObjectAcl			= "s3:GetObjectAcl"
	PermS3_GetObjectTagging			= "s3:GetObjectTagging"
	PermS3_GetObjectTorrent			= "s3:GetObjectTorrent"
	PermS3_GetObjectVersion			= "s3:GetObjectVersion"
	PermS3_GetObjectVersionAcl		= "s3:GetObjectVersionAcl"
	PermS3_GetObjectVersionTagging		= "s3:GetObjectVersionTagging"
	PermS3_GetObjectVersionTorrent		= "s3:GetObjectVersionTorrent"
	PermS3_ListMultipartUploadParts		= "s3:ListMultipartUploadParts"
	PermS3_PutObject			= "s3:PutObject"
	PermS3_PutObjectAcl			= "s3:PutObjectAcl"
	PermS3_PutObjectTagging			= "s3:PutObjectTagging"
	PermS3_PutObjectVersionAcl		= "s3:PutObjectVersionAcl"
	PermS3_PutObjectVersionTagging		= "s3:PutObjectVersionTagging"
	PermS3_RestoreObject			= "s3:RestoreObject"
)

// Permissions related to bucket operations
const (
	PermS3_CreateBucket			= "s3:CreateBucket"
	PermS3_DeleteBucket			= "s3:DeleteBucket"
	PermS3_ListBucket			= "s3:ListBucket"
	PermS3_ListBucketVersions		= "s3:ListBucketVersions"
	PermS3_ListAllMyBuckets			= "s3:ListAllMyBuckets"
	PermS3_ListBucketMultipartUploads	= "s3:ListBucketMultipartUploads"
)

// Most permissive mode
const (
	PermS3_Any				= "s3:*"
	Resourse_Any				= "*"
)

type S3Policy struct {
	Effect				string		`bson:"effect,omitempty"`
	Action				[]string	`bson:"action,omitempty"`
	Resource			[]string	`bson:"resource,omitempty"`
}

var PolicyBucketActions = []string {
	PermS3_AbortMultipartUpload,
	PermS3_DeleteObject,
	PermS3_DeleteObjectTagging,
	PermS3_DeleteObjectVersion,
	PermS3_DeleteObjectVersionTagging,
	PermS3_GetObject,
	PermS3_GetObjectAcl,
	PermS3_GetObjectTagging,
	PermS3_GetObjectTorrent,
	PermS3_GetObjectVersion,
	PermS3_GetObjectVersionAcl,
	PermS3_GetObjectVersionTagging,
	PermS3_GetObjectVersionTorrent,
	PermS3_ListMultipartUploadParts,
	PermS3_PutObject,
	PermS3_PutObjectAcl,
	PermS3_PutObjectTagging,
	PermS3_PutObjectVersionAcl,
	PermS3_PutObjectVersionTagging,
	PermS3_RestoreObject,
	PermS3_ListBucket,
	PermS3_ListBucketVersions,
	PermS3_ListBucketMultipartUploads,
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
		if policy.Action[0] == PermS3_Any {
			if policy.Resource[0] == Resourse_Any {
				return true
			}
		}
	}
	return false
}

func (policy *S3Policy) Match(resource string) bool {
	if policy.isCanned() {
		for _, x := range policy.Resource {
			if x != resource {
				return false
			}
		}
	}
	return false
}
