/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

import (
	"time"
)

type TracerHello struct {
	ID	string
}

type TracerEvent struct {
	Ts	time.Time
	RqID	uint64
	Type	string
	Data	map[string]interface{}
}
