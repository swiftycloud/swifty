package swyapi

import (
	"time"
)

type SwdFunctionBuild struct {
	Sources		string		`json:"sources"`
}

func (r *SwdFunctionRunResult)FnTime() time.Duration {
	return time.Duration(r.Time) * time.Microsecond
}
