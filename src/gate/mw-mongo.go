package main

import (
	"gopkg.in/mgo.v2"
	"context"
	"time"
)

func mgoDial(conf *YAMLConfMw) (*mgo.Session, error) {
	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mongo.Addr},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mongo.Admin,
		Password:	gateSecrets[conf.Mongo.Pass],
	}

	return mgo.DialWithInfo(&ifo)
}

func InitMongo(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(mwd)
	if err != nil {
		return err
	}

	mwd.Namespace = mwd.Client

	sess, err := mgoDial(conf)
	if err != nil {
		return err
	}

	defer sess.Close()

	err = sess.DB(mwd.Namespace).UpsertUser(&mgo.User{
		Username: mwd.Client,
		Password: mwd.Secret,
		Roles: []mgo.Role{ "dbOwner" },
	})

	return err
}

func FiniMongo(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) error {
	sess, err := mgoDial(conf)
	if err != nil {
		return err
	}
	defer sess.Close()

	err = sess.DB(mwd.Namespace).DropDatabase()
	if err != nil {
		ctxlog(ctx).Errorf("can't drop database %s: %s", mwd.Namespace, err.Error())
	}

	return nil
}

func GetEnvMongo(conf *YAMLConfMw, mwd *MwareDesc) ([][2]string) {
	return append(mwGenUserPassEnvs(mwd, conf.Mongo.Addr), mkEnv(mwd, "DBNAME", mwd.Namespace))
}

var MwareMongo = MwareOps {
	Init:	InitMongo,
	Fini:	FiniMongo,
	GetEnv:	GetEnvMongo,
}
