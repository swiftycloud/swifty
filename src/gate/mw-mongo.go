package main

import (
	"gopkg.in/mgo.v2"
	"context"
	"errors"
	"time"
	"../apis"
)

func mgoDial() (*mgo.Session, error) {
	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mware.Mongo.c.Addr()},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mware.Mongo.c.User,
		Password:	gateSecrets[conf.Mware.Mongo.c.Pass],
	}

	return mgo.DialWithInfo(&ifo)
}

func InitMongo(ctx context.Context, mwd *MwareDesc) (error) {
	err := mwareGenerateUserPassClient(ctx, mwd)
	if err != nil {
		return err
	}

	mwd.Namespace = mwd.Client

	sess, err := mgoDial()
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

func FiniMongo(ctx context.Context, mwd *MwareDesc) error {
	ctxlog(ctx).Debugf("Drop DB 1")
	sess, err := mgoDial()
	if err != nil {
		return err
	}
	defer sess.Close()

	ctxlog(ctx).Debugf("Drop DB 2")
	err = sess.DB(mwd.Namespace).DropDatabase()
	if err != nil {
		ctxlog(ctx).Errorf("can't drop database %s: %s", mwd.Namespace, err.Error())
	}

	ctxlog(ctx).Debugf("Drop DB 3")

	return nil
}

func GetEnvMongo(conf *YAMLConfMw, mwd *MwareDesc) map[string][]byte {
	e := mwd.stdEnvs(conf.Mongo.c.Addr())
	e[mwd.envName("DBNAME")] = []byte(mwd.Namespace)
	return e
}

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
}

func InfoMongo(ctx context.Context, conf *YAMLConfMw, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
	sess, err := mgoDial()
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
	LiteOK:	true,
}
