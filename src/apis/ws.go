/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package swyapi

type WsMwReq struct {
	MType	int	`json:"msg_type"`
	Msg	[]byte	`json:"msg_payload"`
}
