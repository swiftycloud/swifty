package main

import (
	"../common/xrest"
	"gopkg.in/mgo.v2"
	"../apis"
)

var gateErrMsg = map[uint]string {
	swyapi.GateGenErr:	"Unknown error",
	swyapi.GateBadRequest:	"Error parsing request parameters",
	swyapi.GateBadResp:	"Error writing responce",
	swyapi.GateDbError:	"Database request failed",
	swyapi.GateDuplicate:	"ID already exists",
	swyapi.GateNotFound:	"ID not found",
	swyapi.GateFsError:	"Files access failed",
	swyapi.GateNotAvail:	"Operation not (yet) available",
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
		return GateErrC(swyapi.GateNotFound)
	} else if mgo.IsDup(err) {
		return GateErrC(swyapi.GateDuplicate)
	} else {
		return GateErrE(swyapi.GateDbError, err)
	}
}
