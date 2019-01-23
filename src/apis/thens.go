/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

type Then struct {
	Call		*ThenCall		`json:"call,omitempty"`
}

/*
 * This "then" makes gate call another function with the given
 * args and passing the return value to its body.
 *
 * Then @sync field specifies whether the execution of this fn
 * should be done synchronously and (!) the result of it is what
 * gate would return to the caller.
 */
type ThenCall struct {
	Name		string			`json:"name"`
	Args		map[string]string	`json:"args"`
	Sync		bool			`json:"sync"`
}
