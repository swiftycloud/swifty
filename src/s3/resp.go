package main

type S3RespError struct {
	Code			string				`xml:"Code,omitempy"`
	Message			string				`xml:"Message,omitempy"`
	Resource		string				`xml:"Resource,omitempy"`
	RequestID		string				`xml:"RequestId,omitempy"`
}

type S3ObjectOwner struct {
	DisplayName		string				`xml:"DisplayName,omitempy"`
	ID			string				`xml:"ID,omitempy"`
}

const (
	S3StorageClassStandard			= "STANDARD"
	S3StorageClassStandardIa		= "STANDARD_IA"
	S3StorageClassReducedRedundancy		= "REDUCED_REDUNDANCY"
	S3StorageClassGlacier			= "GLACIER"
)

type S3ObjectEntry struct {
	Key			string				`xml:"Key,omitempy"`
	Size			int64				`xml:"Size,omitempy"`
	Owner			S3ObjectOwner			`xml:"Owner,omitempy"`
	LastModified		string				`xml:"LastModified,omitempy"`
	ETag			string				`xml:"ETag,omitempy"`
	StorageClass		string				`xml:"StorageClass,omitempy"`
}

type S3BucketList struct {
	Name			string				`xml:"Name,omitempy"`
	Prefix			string				`xml:"Prefix,omitempy"`
	KeyCount		int64				`xml:"KeyCount,omitempy"`
	MaxKeys			int64				`xml:"MaxKeys,omitempy"`
	IsTruncated		bool				`xml:"IsTruncated,omitempy"`
	Contents		[]S3ObjectEntry			`xml:"Contents,omitempy"`
	CommonPrefixes		string				`xml:"CommonPrefixes,omitempy"`
	Delimiter		string				`xml:"Delimiter,omitempy"`
	EncodingType		string				`xml:"Encoding-Type,omitempy"`
	ContinuationToken	string				`xml:"ContinuationToken,omitempy"`
	NextContinuationToken	string				`xml:"NextContinuationToken,omitempy"`
	StartAfter		string				`xml:"StartAfter,omitempy"`
}
