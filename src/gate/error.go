package main

import (
	"../common/xrest"
	"../common"
	"gopkg.in/mgo.v2"
)

var gateErrMsg = map[uint]string {
	swy.GateGenErr:		"Unknown error",
	swy.GateBadRequest:	"Error parsing request parameters",
	swy.GateBadResp:	"Error writing responce",
	swy.GateDbError:	"Database request failed",
	swy.GateDuplicate:	"ID already exists",
	swy.GateNotFound:	"ID not found",
	swy.GateFsError:	"Files access failed",
	swy.GateNotAvail:	"Operation not (yet) available",
}

func GateErrC(code uint) *xrest.ReqErr {
	return &xrest.ReqErr{code, gateErrMsg[code]}
}

func GateErrE(code uint, err error) *xrest.ReqErr {
	return &xrest.ReqErr{code, err.Error()}
}

func GateErrM(code uint, msg string) *xrest.ReqErr {
	return &xrest.ReqErr{code, msg}
}

func GateErrD(err error) *xrest.ReqErr {
	if err == mgo.ErrNotFound {
		return GateErrC(swy.GateNotFound)
	} else if mgo.IsDup(err) {
		return GateErrC(swy.GateDuplicate)
	} else {
		return GateErrE(swy.GateDbError, err)
	}
}
