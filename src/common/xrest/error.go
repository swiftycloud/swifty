package xrest

import (
	"strconv"
	"encoding/json"
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
