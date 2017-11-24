package main

import (
	"gopkg.in/mgo.v2"
	"fmt"
	"time"
	"encoding/json"
	"../apis/apps"
)

type MGOSetting struct {
	DBName	string	`json:"database"`
}

func mgoDial(conf *YAMLConfMw) (*mgo.Session, error) {
	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mongo.Addr},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mongo.Admin,
		Password:	conf.Mongo.Pass,
	}

	return mgo.DialWithInfo(&ifo)
}

func InitMongo(conf *YAMLConfMw, mwd *MwareDesc, mware *swyapi.MwareItem) (error) {
	mgs := MGOSetting{}

	err := mwareGenerateClient(mwd)
	if err != nil {
		return err
	}

	mgs.DBName = mwd.Client

	sess, err := mgoDial(conf)
	if err != nil {
		return err
	}

	defer sess.Close()

	err = sess.DB(mgs.DBName).UpsertUser(&mgo.User{
		Username: mwd.Client,
		Password: mwd.Pass,
		Roles: []mgo.Role{ "dbOwner" },
	})

	if err != nil {
		return err
	}

	js, err := json.Marshal(&mgs)
	if err != nil {
		return err
	}

	mwd.JSettings = string(js)

	return nil
}

func FiniMongo(conf *YAMLConfMw, mwd *MwareDesc) error {
	var mgs MGOSetting

	err := json.Unmarshal([]byte(mwd.JSettings), &mgs)
	if err != nil {
		return fmt.Errorf("Can't unmarshal data %s: %s",
					mwd.JSettings, err.Error())
	}

	sess, err := mgoDial(conf)
	if err != nil {
		return err
	}
	defer sess.Close()

	err = sess.DB(mgs.DBName).DropDatabase()
	if err != nil {
		log.Errorf("can't drop database %s: %s", mgs.DBName, err.Error())
	}

	return nil
}

func GetEnvMongo(conf *YAMLConfMw, mwd *MwareDesc) ([]string) {
	var mgs DBSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &mgs)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Mongo.Addr), mkEnv(mwd, "DBNAME", mgs.DBName))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwareMongo = MwareOps {
	Init:	InitMongo,
	Fini:	FiniMongo,
	GetEnv:	GetEnvMongo,
}
