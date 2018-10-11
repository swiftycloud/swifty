package swyapi

type WsMwReq struct {
	MType	int	`json:"msg_type"`
	Msg	[]byte	`json:"msg_payload"`
}
