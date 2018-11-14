/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package gmgo

import (
	"time"
)

type FnStatValues struct {
	Called		uint64		`bson:"called"`
	Timeouts	uint64		`bson:"timeouts"`
	Errors		uint64		`bson:"errors"`
	LastCall	time.Time	`bson:"lastcall"`
	RunTime		time.Duration	`bson:"rtime"`
	BytesIn		uint64		`bson:"bytesin"`
	BytesOut	uint64		`bson:"bytesout"`

	/* RunCost is a value that represents the amount of
	 * resources spent for this function. It's used by
	 * billing to change the tennant.
	 */
	RunCost		uint64		`bson:"runcost"`
}

type TenStatValues struct {
	Called		uint64		`bson:"called"`
	RunCost		uint64		`bson:"runcost"`
	BytesIn		uint64		`bson:"bytesin"`
	BytesOut	uint64		`bson:"bytesout"`
}
