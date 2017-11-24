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

func InitPostgres(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) (error) {
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

	addr := strings.Split(conf.Mware.Postgres.Addr, ":")[0] + ":" + conf.Mware.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPostTimeout("http://" + addr + "/create", 120,
			&swyapi.PgRequest{Token: conf.Mware.Postgres.Token,
				User: mwd.Client, Pass: mwd.Pass, DbName: pgs.DBName}, nil, http.StatusOK)
	if err != nil {
		return err
	}

	mwd.JSettings = string(js)
	return nil
}

func FiniPostgres(conf *YAMLConf, mwd *MwareDesc) error {
	var pgs PGSetting

	err := json.Unmarshal([]byte(mwd.JSettings), &pgs)
	if err != nil {
		return fmt.Errorf("Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	addr := strings.Split(conf.Mware.Postgres.Addr, ":")[0] + ":" + conf.Mware.Postgres.AdminPort
	_, err = swy.HTTPMarshalAndPostTimeout("http://" + addr + "/drop", 120,
			&swyapi.PgRequest{Token: conf.Mware.Postgres.Token,
				User: mwd.Client, DbName: pgs.DBName}, nil, http.StatusOK)

	return err
}

func EventPostgres(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	return fmt.Errorf("No events for postgres")
}

func GetEnvPostgres(conf *YAMLConf, mwd *MwareDesc) ([]string) {
	var pgs PGSetting
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &pgs)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Mware.Postgres.Addr), mkEnv(mwd, "DBNAME", pgs.DBName))
	} else {
		log.Fatal("Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwarePostgres = MwareOps {
	Init:	InitPostgres,
	Fini:	FiniPostgres,
	Event:	EventPostgres,
	GetEnv:	GetEnvPostgres,
}
