/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

type Then struct {
	Call		*ThenCall		`json:"call,omitempty"`
}

type ThenCall struct {
	Name		string			`json:"name"`
	Args		map[string]string	`json:"args"`
}
