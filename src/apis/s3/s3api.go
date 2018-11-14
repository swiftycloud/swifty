/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swys3api

import (
	"encoding/xml"
)

const (
	SwyS3_AdminToken	= "X-SwyS3-Token"
	SwyS3_AccessKey		= "X-SwyS3-AccessKey"
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
	S3BucketAclCannedPrivate		= "private"
	S3BucketAclCannedPublicRead		= "public-read"
	S3BucketAclCannedPublicReadWrite	= "public-read-write"
	S3BucketAclCannedAuthenticatedRead	= "authenticated-read"
)

const (
	S3PermRead				= "READ"
	S3PermWrite				= "WRITE"
	S3PermReadACP				= "READ_ACP"
	S3PermWriteACP				= "WRITE_ACP"
	S3PermFull				= "FULL_CONTROL"
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

type CopyObjectResult struct {
	ETag			string				`xml:"ETag,omitempy"`
	LastModified		string				`xml:"LastModified,omitempy"`
}

type S3Prefix struct {
	Prefix			string				`xml:"Prefix"`
}

type S3Datapoint struct {
	Timestamp		string				`xml:"Timestamp,omitempy"`
	SampleCount		float64				`xml:"SampleCount,omitempy"`
	Average			float64				`xml:"Average,omitempy"`
	//Sum			float64				`xml:"Sum,omitempy"`
	//Minimum		float64				`xml:"Minimum,omitempy"`
	//Maximum		float64				`xml:"Maximum,omitempy"`
	Unit			string				`xml:"Unit,omitempy"`
	//ExtendedStatistics	map[string]float64		`xml:"ExtendedStatistics,omitempy"`
}

type S3Datapoints struct {
	Points			[]S3Datapoint			`xml:"member,omitempy"`
}

type S3WebErrDoc struct {
	Key			string				`xml:"Key"`
}

type S3WebIndex struct {
	Suff			string				`xml:"Suffix"`
}

type S3WebsiteConfig struct {
	ErrDoc			S3WebErrDoc			`xml:"ErrorDocument"`
	IndexDoc		S3WebIndex			`xml:"IndexDocument"`
}

type S3GetMetricStatisticsResult struct {
	Label			string				`xml:"Label,omitempy"`
	Datapoints		S3Datapoints			`xml:"Datapoints,omitempy"`
}

type S3GetMetricStatisticsOutput struct {
	XMLName			xml.Name			`xml:"GetMetricStatisticsResponse"`
	Result			S3GetMetricStatisticsResult	`xml:"GetMetricStatisticsResult"`
}

type S3Bucket struct {
	XMLName			xml.Name			`xml:"ListBucketResult"`
	Name			string				`xml:"Name,omitempy"`
	Prefix			string				`xml:"Prefix,omitempy"`
	KeyCount		int64				`xml:"KeyCount,omitempy"`
	MaxKeys			int64				`xml:"MaxKeys,omitempy"`
	IsTruncated		bool				`xml:"IsTruncated,omitempy"`
	Contents		[]S3Object			`xml:"Contents,omitempy"`
	CommonPrefixes		[]S3Prefix			`xml:"CommonPrefixes,omitempy"`
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

type S3CommonPrefixEntry struct {
	Prefix			string				`xml:"Prefix,omitempy"`
}


type S3CommonPrefixes struct {
	Prefixes		[]S3CommonPrefixEntry		`xml:"CommonPrefixes,omitempy"`
}

type S3MpuUpload struct {
	UploadId		string				`xml:"UploadId,omitempy"`
	Key			string				`xml:"Key,omitempy"`
	Initiated		string				`xml:"Initiated,omitempy"`
	StorageClass		string				`xml:"StorageClass,omitempy"`
	Initiator		S3Owner				`xml:"Initiator,omitempy"`
	Owner			S3Owner				`xml:"Owner,omitempy"`
}

type S3MpuList struct {
	XMLName			xml.Name			`xml:"ListMultipartUploadsResult"`
	Bucket			string				`xml:"Bucket,omitempy"`
	KeyMarker		string				`xml:"KeyMarker,omitempy"`
	UploadIdMarker		string				`xml:"UploadIdMarker,omitempy"`
	NextKeyMarker		string				`xml:"NextKeyMarker,omitempy"`
	NextUploadIdMarker	string				`xml:"NextUploadIdMarker,omitempy"`
	EncodingType		string				`xml:"Encoding-Type,omitempy"`
	MaxUploads		int64				`xml:"MaxUploads,omitempy"`
	IsTruncated		bool				`xml:"IsTruncated,omitempy"`
	Upload			[]S3MpuUpload			`xml:"Upload,omitempy"`
	Prefix			string				`xml:"Prefix,omitempy"`
	CommonPrefixes		[]S3CommonPrefixes
	Delimiter		string				`xml:"Delimiter,omitempy"`
}

type S3MpuPart struct {
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
