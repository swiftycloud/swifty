package main

import (
	"fmt"
	"strings"
	"encoding/json"
	"net/http"
	"../apis/apps"
	"../common"
)

type PGSetting struct {
	DBName	string	`json:"database"`
}

func InitPostgres(conf *YAMLConfMw, mwd *MwareDesc, mware *swyapi.MwareItem) (error) {
	pgs := PGSetting{}

	err := mwareGenerateClient(mwd)
	if err != nil {
		return err
	}

	pgs.DBName = mwd.Client

	js, err := json.Marshal(&pgs)
	if err != nil {
		return err
	}

	addr := strings.Split(conf.Postgres.Addr, ":")[0] + ":" + conf.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPostTimeout("http://" + addr + "/create", 120,
			&swyapi.PgRequest{Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, Pass: mwd.Pass, DbName: pgs.DBName}, nil, http.StatusOK)
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
	_, err = swy.HTTPMarshalAndPostTimeout("http://" + addr + "/drop", 120,
			&swyapi.PgRequest{Token: gateSecrets[conf.Postgres.Token],
				User: mwd.Client, DbName: pgs.DBName}, nil, http.StatusOK)

	return err
}

func GetEnvPostgres(conf *YAMLConfMw, mwd *MwareDesc) ([]string) {
	var pgs PGSetting
	var envs []string
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
