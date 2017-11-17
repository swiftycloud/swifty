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

func mgoDial(conf *YAMLConf) (*mgo.Session, error) {
	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mware.MGO.Addr},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mware.MGO.Admin,
		Password:	conf.Mware.MGO.Pass,
	}

	return mgo.DialWithInfo(&ifo)
}

func InitMongo(conf *YAMLConf, mwd *MwareDesc, mware *swyapi.MwareItem) ([]byte, error) {
	mgs := MGOSetting{}

	err := mwareGenerateClient(mwd)
	if err != nil {
		return nil, err
	}

	mgs.DBName = mwd.Client

	sess, err := mgoDial(conf)
	if err != nil {
		return nil, err
	}

	defer sess.Close()

	err = sess.DB(mgs.DBName).UpsertUser(&mgo.User{
		Username: mwd.Client,
		Password: mwd.Pass,
		Roles: []mgo.Role{ "dbOwner" },
	})

	if err != nil {
		return nil, err
	}

	return json.Marshal(&mgs)
}

func FiniMongo(conf *YAMLConf, mwd *MwareDesc) error {
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

func EventMongo(conf *YAMLConf, source *FnEventDesc, mwd *MwareDesc, on bool) (error) {
	return fmt.Errorf("No events for mongo")
}

func GetEnvMongo(conf *YAMLConf, mwd *MwareDesc) ([]string) {
	var mgs DBSettings
	var envs []string
	var err error

	err = json.Unmarshal([]byte(mwd.JSettings), &mgs)
	if err == nil {
		envs = append(mwGenEnvs(mwd, conf.Mware.MGO.Addr), mkEnv(mwd, "DBNAME", mgs.DBName))
	} else {
		log.Fatal("rabbit: Can't unmarshal DB entry %s", mwd.JSettings)
	}

	return envs
}

var MwareMongo = MwareOps {
	Init:	InitMongo,
	Fini:	FiniMongo,
	Event:	EventMongo,
	GetEnv:	GetEnvMongo,
}
