package swys3api

import (
	"encoding/xml"
)

// All entries exported by Amazon S3
//
// AnalyticsConfiguration
// BucketLoggingStatus
// CompleteMultipartUploadResult		+
// CopyObjectResult
// Delete
// DeleteResult
// Error					+
// InitiateMultipartUploadResult		+
// InventoryConfiguration
// LifecycleConfiguration
// ListAllMyBucketsResult			+
// ListBucketResult				+
// ListInventoryConfigurationsResult
// ListMultipartUploadsResult
// ListPartsResult				+
// ListVersionsResult
// LocationConstraint
// MetricsConfiguration
// ReplicationConfiguration
// RequestPaymentConfiguration
// Tagging
// WebsiteConfiguration

const (
	S3StorageClassStandard			= "STANDARD"
	S3StorageClassStandardIa		= "STANDARD_IA"
	S3StorageClassReducedRedundancy		= "REDUCED_REDUNDANCY"
	S3StorageClassGlacier			= "GLACIER"
)

const (
	S3ObjectAclPrivate			= "private"
	S3ObjectAclPublicRead			= "public-read"
	S3ObjectAclPublicReadWrite		= "public-read-write"
	S3ObjectAclAuthenticatedRead		= "authenticated-read"
	S3ObjectAclAwsExecRead			= "aws-exec-read"
	S3ObjectAclBucketOwnerRead		= "bucket-owner-read"
	S3ObjectAclBucketOwnerFullControl	= "bucket-owner-full-control"
)

const (
	S3BucketAclPrivate			= "private"
	S3BucketAclPublicRead			= "public-read"
	S3BucketAclPublicReadWrite		= "public-read-write"
	S3BucketAclAuthenticatedRead		= "authenticated-read"
)

type S3Error struct {
	XMLName			xml.Name			`xml:"Error"`
	Code			string				`xml:"Code,omitempy"`
	Message			string				`xml:"Message,omitempy"`
	Resource		string				`xml:"Resource,omitempy"`
	RequestID		string				`xml:"RequestId,omitempy"`
}

type S3Owner struct {
	DisplayName		string				`xml:"DisplayName,omitempy"`
	ID			string				`xml:"ID,omitempy"`
}

type S3Object struct {
	Key			string				`xml:"Key,omitempy"`
	Size			int64				`xml:"Size,omitempy"`
	Owner			S3Owner				`xml:"Owner,omitempy"`
	LastModified		string				`xml:"LastModified,omitempy"`
	ETag			string				`xml:"ETag,omitempy"`
	StorageClass		string				`xml:"StorageClass,omitempy"`
}

type S3Bucket struct {
	XMLName			xml.Name			`xml:"ListBucketResult"`
	Name			string				`xml:"Name,omitempy"`
	Prefix			string				`xml:"Prefix,omitempy"`
	KeyCount		int64				`xml:"KeyCount,omitempy"`
	MaxKeys			int64				`xml:"MaxKeys,omitempy"`
	IsTruncated		bool				`xml:"IsTruncated,omitempy"`
	Contents		[]S3Object			`xml:"Contents,omitempy"`
	CommonPrefixes		string				`xml:"CommonPrefixes,omitempy"`
	Delimiter		string				`xml:"Delimiter,omitempy"`
	EncodingType		string				`xml:"Encoding-Type,omitempy"`
	ContinuationToken	string				`xml:"ContinuationToken,omitempy"`
	NextContinuationToken	string				`xml:"NextContinuationToken,omitempy"`
	StartAfter		string				`xml:"StartAfter,omitempy"`
}

type S3BucketListEntry struct {
	Name			string				`xml:"Name,omitempy"`
	CreationDate		string				`xml:"CreationDate,omitempy"`
}

type S3BucketListEntries struct {
	Bucket			[]S3BucketListEntry		`xml:"Bucket"`
}

type S3BucketList struct {
	XMLName			xml.Name			`xml:"ListAllMyBucketsResult"`
	Buckets			S3BucketListEntries		`xml:"Buckets,omitempy"`
	Owner			S3Owner				`xml:"Owner,omitempy"`
}

type S3MpuInit struct {
	XMLName			xml.Name			`xml:"InitiateMultipartUploadResult"`
	Bucket			string				`xml:"Bucket,omitempy"`
	Key			string				`xml:"Key,omitempy"`
	UploadId		string				`xml:"UploadId,omitempy"`
}

type S3MpuPart struct {
	XMLName			xml.Name			`xml:"ListPartsResultPart"`
	PartNumber		int				`xml:"PartNumber"`
	LastModified		string				`xml:"LastModified"`
	ETag			string				`xml:"ETag"`
	Size			int64				`xml:"Size"`
}

type S3MpuPartList struct {
	XMLName			xml.Name			`xml:"ListPartsResult"`
	Bucket			string				`xml:"Bucket"`
	EncodingType		string				`xml:"Encoding-Type"`
	Key			string				`xml:"Key"`
	UploadId		string				`xml:"UploadId"`
	Initiator		S3Owner				`xml:"Initiator"`
	Owner			S3Owner				`xml:"Owner"`
	StorageClass		string				`xml:"StorageClass"`
	PartNumberMarker	int				`xml:"PartNumberMarker"`
	NextPartNumberMarker	int				`xml:"NextPartNumberMarker"`
	MaxParts		int				`xml:"MaxParts"`
	IsTruncated		bool				`xml:"IsTruncated"`
	Part			[]S3MpuPart			`xml:"Part"`
}

type S3MpuFiniPart struct {
	PartNumber		int				`xml:"PartNumber"`
	ETag			string				`xml:"ETag"`
}

type S3MpuFiniParts struct {
	XMLName			xml.Name			`xml:"CompleteMultipartUpload"`
	Part			[]S3MpuFiniPart			`xml:"Part"`
}

type S3MpuFini struct {
	XMLName			xml.Name			`xml:"CompleteMultipartUploadResult"`
	Location		string				`xml:"Location"`
	Bucket			string				`xml:"Bucket"`
	Key			string				`xml:"Key"`
	ETag			string				`xml:"ETag"`
}
