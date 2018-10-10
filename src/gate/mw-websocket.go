package main

import (
	"context"
	"swifty/common"
	"swifty/apis"
)

func InitWebSocket(ctx context.Context, mwd *MwareDesc) (error) {
	var err error

	mwd.Secret, err = xh.GenRandId(32)
	if err != nil {
		return err
	}

	return nil
}

func FiniWebSocket(ctx context.Context, mwd *MwareDesc) error {
	return nil
}

func GetEnvWebSocket(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	return map[string][]byte{mwd.envName("SENDKEY"): []byte(mwd.Secret)}
}

func InfoWebSocket(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
	url := conf.Daemon.WSGate
	if url == "" {
		url = conf.Daemon.Addr
	}
	url += "/websockets/" + mwd.Cookie

	ifo.URL = &url
	return nil
}

var MwareWebSocket = MwareOps {
	Init:	InitWebSocket,
	Fini:	FiniWebSocket,
	GetEnv:	GetEnvWebSocket,
	Info:	InfoWebSocket,
	Devel:	true,
}
