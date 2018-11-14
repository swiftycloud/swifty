/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

import (
	"time"
)

type WdogFunctionBuild struct {
	Sources		string		`json:"sources"`
	Suff		string		`json:"suff,omitempty"`
	Packages	string		`json:"packages,omitempty"`
}

func (r *WdogFunctionRunResult)FnTime() time.Duration {
	return time.Duration(r.Time) * time.Microsecond
}
