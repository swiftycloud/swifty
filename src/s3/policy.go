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
	PermS3_Any				= "*"
)
