package main

import (
	"strings"
	"context"
	"../common"
	"../common/http"
	"../apis/apps"
)

func InitPostgres(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(mwd)
	if err != nil {
		return err
	}

	/* Postgres needs lower case in all user/db names and
	 * should start with letter */
	mwd.Client = "p" + strings.ToLower(mwd.Client[:30])
	mwd.Namespace = mwd.Client

	addr := swy.MakeAdminURL(conf.Postgres.Addr, conf.Postgres.AdminPort)
	_, err = swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + "/create",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, Pass: mwd.Secret, DbName: mwd.Namespace,
			})
	return err
}

func FiniPostgres(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	addr := swy.MakeAdminURL(conf.Postgres.Addr, conf.Postgres.AdminPort)
	_, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + "/drop",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, DbName: mwd.Namespace,
			})

	return err
}

func GetEnvPostgres(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	return append(mwGenUserPassEnvs(mwd, conf.Postgres.Addr), mkEnv(mwd, "DBNAME", mwd.Namespace))
}

var MwarePostgres = MwareOps {
	Init:	InitPostgres,
	Fini:	FiniPostgres,
	GetEnv:	GetEnvPostgres,
	Devel:  true,
}
