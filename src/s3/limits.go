/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

const (
	// Maximum of objects in a bucket
	S3StorageMaxObjects		= int64(10000)

	// Default objects count for listing
	S3StorageDefaultListObjects	= int64(1000)

	// Maximum size of all objects in bucket
	S3StorageMaxBytes		= int64(1 << 30)

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
