package main

import (
	"fmt"
	"strings"
	"encoding/json"
	"../common"
	"../apis/apps"
)

type PGSetting struct {
	DBName	string	`json:"database"`
}

func InitPostgres(conf *YAMLConfMw, mwd *MwareDesc) (error) {
	pgs := PGSetting{}

	err := mwareGenerateClient(mwd)
	if err != nil {
		return err
	}

	/* Postgres needs lower case in all user/db names and
	 * should start with letter */
	mwd.Client = "p" + strings.ToLower(mwd.Client[:30])
	pgs.DBName = mwd.Client

	js, err := json.Marshal(&pgs)
	if err != nil {
		return err
	}

	mwd.JSettings = string(js)

	addr := strings.Split(conf.Postgres.Addr, ":")[0] + ":" + conf.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPost(
			&swy.RestReq{
				Address: "http://" + addr + "/create",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, Pass: mwd.Pass, DbName: pgs.DBName,
			})
	if err != nil {
		return err
	}

	mwd.JSettings = string(js)
	return nil
}

func FiniPostgres(conf *YAMLConfMw, mwd *MwareDesc) error {
	var pgs PGSetting

	err := json.Unmarshal([]byte(mwd.JSettings), &pgs)
	if err != nil {
		return fmt.Errorf("Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	addr := strings.Split(conf.Postgres.Addr, ":")[0] + ":" + conf.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPost(
			&swy.RestReq{
				Address: "http://" + addr + "/drop",
				Timeout: 120,
			},
			&swyapi.PgRequest{
				Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, DbName: pgs.DBName,
			})

	return err
}

func GetEnvPostgres(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	var pgs PGSetting
	var envs [][2]string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &pgs)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Postgres.Addr), mkEnv(mwd, "DBNAME", pgs.DBName))
	} else {
		log.Fatal("Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwarePostgres = MwareOps {
	Init:	InitPostgres,
	Fini:	FiniPostgres,
	GetEnv:	GetEnvPostgres,
}
