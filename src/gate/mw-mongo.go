package main

import (
	"gopkg.in/mgo.v2"
	"context"
	"errors"
	"time"
	"../apis/apps"
)

func mgoDial(conf *YAMLConfMw) (*mgo.Session, error) {
	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mongo.c.Addr()},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mongo.c.User,
		Password:	gateSecrets[conf.Mongo.c.Pass],
	}

	return mgo.DialWithInfo(&ifo)
}

func InitMongo(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
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
	return append(mwGenUserPassEnvs(mwd, conf.Mongo.c.Addr()), mkEnv(mwd, "DBNAME", mwd.Namespace))
}

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
}

func InfoMongo(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
	sess, err := mgoDial(conf)
	if err != nil {
		return err
	}
	defer sess.Close()

	st := MgoStat{}
	err = sess.DB(mwd.Namespace).Run("dbStats", &st)
	if err != nil {
		ctxlog(ctx).Errorf("can't get dbStats for %s: %s", mwd.Namespace, err.Error())
		return errors.New("Error getting DB stats")
	}

	ifo.SetDU(st.ISize + st.SSize)

	return nil
}

var MwareMongo = MwareOps {
	Init:	InitMongo,
	Fini:	FiniMongo,
	GetEnv:	GetEnvMongo,
	Info:	InfoMongo,
}
