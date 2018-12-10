/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"swifty/common/xrest/sysctl"
)

const (
	// Maximum of objects in a bucket
	S3StorageMaxObjects		= int64(10000)

	// Maximum size of all objects in bucket
	S3StorageMaxBytes		= int64(1 << 30)

	// Maximum ACL per bucket/object
	S3BucketMaxACL			= int(100)
)

var (
	// Default objects count for listing
	S3StorageDefaultListObjects	= int64(1000)

	// How many bytes we heep directly in part
	S3InlineDataSize		= int64(1 << 20)

	// Maximum size of object chunk
	S3MaxChunkSize			= int64(16 << 20)
)

func init() {
	sysctl.AddInt64Sysctl("default_list_objects", &S3StorageDefaultListObjects)
	sysctl.AddInt64Sysctl("inline_data_size", &S3InlineDataSize)
	sysctl.AddInt64Sysctl("max_chunk_size", &S3MaxChunkSize)
}

// Bucket and object names
const (
	S3BucketName_Letter = `[a-zA-Z0-9\-]`
	S3ObjectName_Letter = `[a-zA-Z0-9\/\!\-\_\.\*\'\(\)]`
)
