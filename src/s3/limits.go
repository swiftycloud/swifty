package main

const (
	// Maximum of objects in a bucket
	S3StorageMaxObjects		= int64(10000)

	// Maximum size of one object
	S3StorageMaxBytes		= int64(100 << 20)

	// Maximum size of an object to keep data inside
	// MongoDB itself
	S3StorageSizePerObj		= int64(16 << 20)

	// Maximum ACL per bucket/object
	S3BucketMaxACL			= int(100)
)

// Bucket and object names
const (
	S3BucketName_Letter = `[a-zA-Z0-9\-]`
	S3ObjectName_Letter = `[a-zA-Z0-9\/\!\-\_\.\*\'\(\)]`
)
