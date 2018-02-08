package main

import (
	"../apis/apps"
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

func GateErrC(code uint) *swyapi.GateErr {
	return &swyapi.GateErr{code, gateErrMsg[code]}
}

func GateErrE(code uint, err error) *swyapi.GateErr {
	return &swyapi.GateErr{code, err.Error()}
}

func GateErrM(code uint, msg string) *swyapi.GateErr {
	return &swyapi.GateErr{code, msg}
}

func GateErrD(err error) *swyapi.GateErr {
	if err == mgo.ErrNotFound {
		return GateErrC(swy.GateNotFound)
	} else if mgo.IsDup(err) {
		return GateErrC(swy.GateDuplicate)
	} else {
		return GateErrE(swy.GateDbError, err)
	}
}
