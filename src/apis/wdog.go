package swyapi

import (
	"time"
)

type WdogFunctionBuild struct {
	Sources		string		`json:"sources"`
	Suff		string		`json:"suff,omitempty"`
}

func (r *WdogFunctionRunResult)FnTime() time.Duration {
	return time.Duration(r.Time) * time.Microsecond
}
