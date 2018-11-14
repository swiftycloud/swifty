/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2/bson"
	"swifty/apis"
)

type TenantCache struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	Cookie		string			`bson:"cookie"`
	Tenant		string			`bson:"tenant"`

	Packages	map[string][]*swyapi.Package	`bson:"packages,omitempty"`
	PkgStats	map[string]*PackagesStats	`bson:"pkg_stats,omitempty"`
}

func init() {
	addSysctl("ten_cache_flush", func() string { return "Set any value here" },
		func(_ string) error {
			ctx, done := mkContext("::tcache-flush")
			defer done(ctx)
			dbTCacheFlushAll(ctx)
			return nil
		})
}
