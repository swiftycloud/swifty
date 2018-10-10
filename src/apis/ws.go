package swyapi

type WsMwReq struct {
	Action	string	`json:"action"`
	MType	*int	`json:"msg_type,omitempty"`
	Msg	[]byte	`json:"msg_payload,omitempty"`
	CId	string	`json:"conn_id,omitempty"`
}
