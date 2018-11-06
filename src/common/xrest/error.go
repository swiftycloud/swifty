package xrest

import (
	"strconv"
	"encoding/json"
)

const (
	GenErr		uint = 1	// Unclassified error
	BadRequest	uint = 2	// Error parsing request data
	BadResp		uint = 3	// Error generating response
)

type ReqErr struct {
	Code		uint			`json:"code"`
	Message		string			`json:"message"`
}

func (re *ReqErr)String() string {
	jdata, err := json.Marshal(re)
	if err == nil {
		return string(jdata)
	} else {
		return "ERROR: " + strconv.Itoa(int(re.Code))
	}
}
