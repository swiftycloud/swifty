package swyapi

import (
	"time"
)

type SwdFunctionBuild struct {
	Sources		string		`json:"sources"`
}

/*
 * This type is not seen by wdog itself, instead, it's described
 * by each wdog runner by smth like "Request"
 */
type SwdFunctionRun struct {
	Args		map[string]string	`json:"args,omitempty"`
	Body		interface{}		`json:"body,omitempty"`
	Claims		map[string]interface{}	`json:"claims,omitempty"` // JWT
	Method		string			`json:"method,omitempty"`
	Path		*string			`json:"path,omitempty"`
}

type SwdFunctionRunResult struct {
	Return		string		`json:"return"`
	Code		int		`json:"code"`
	Stdout		string		`json:"stdout"`
	Stderr		string		`json:"stderr"`
	Time		uint		`json:"time"` /* usec */
}

func (r *SwdFunctionRunResult)FnTime() time.Duration {
	return time.Duration(r.Time) * time.Microsecond
}
