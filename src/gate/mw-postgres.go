package main

import (
	"strings"
	"context"
	"../common/http"
	"../apis/apps"
)

func InitPostgres(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
	if err != nil {
		return err
	}

	/* Postgres needs lower case in all user/db names and
	 * should start with letter */
	mwd.Client = "p" + strings.ToLower(mwd.Client[:30])
	mwd.Namespace = mwd.Client

	addr := conf.Postgres.c.AddrP(conf.Postgres.AdminPort)
	_, err = swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + "/create",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.c.Pass],
				User: mwd.Client, Pass: mwd.Secret, DbName: mwd.Namespace,
			})
	return err
}

func FiniPostgres(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	addr := conf.Postgres.c.AddrP(conf.Postgres.AdminPort)
	_, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + "/drop",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.c.Pass],
				User: mwd.Client, DbName: mwd.Namespace,
			})

	return err
}

func GetEnvPostgres(conf *YAMLConfMw, mwd *MwareDesc) map[string][]byte {
	e := mwd.stdEnvs(conf.Postgres.c.Addr())
	e[mwd.envName("DBNAME")] = []byte(mwd.Namespace)
	return e
}

var MwarePostgres = MwareOps {
	Init:	InitPostgres,
	Fini:	FiniPostgres,
	GetEnv:	GetEnvPostgres,
	Devel:  true,
}
