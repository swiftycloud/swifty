package main

import (
	"strings"
	"../common"
	"../apis/apps"
)

func InitPostgres(conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(mwd)
	if err != nil {
		return err
	}

	/* Postgres needs lower case in all user/db names and
	 * should start with letter */
	mwd.Client = "p" + strings.ToLower(mwd.Client[:30])
	mwd.Namespace = mwd.Client

	addr := strings.Split(conf.Postgres.Addr, ":")[0] + ":" + conf.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPost(
			&swy.RestReq{
				Address: "http://" + addr + "/create",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, Pass: mwd.Secret, DbName: mwd.Namespace,
			})
	return err
}

func FiniPostgres(conf *YAMLConfMw, mwd *MwareDesc) error {
	addr := strings.Split(conf.Postgres.Addr, ":")[0] + ":" + conf.Postgres.AdminPort
	_, err := swy.HTTPMarshalAndPost(
			&swy.RestReq{
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
}
