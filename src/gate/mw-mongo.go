package main

import (
	"gopkg.in/mgo.v2"
	"context"
	"errors"
	"time"
	"swifty/apis"
)

func mgoDial() (*mgo.Session, error) {
	if conf.Mware.Mongo == nil {
		return nil, errors.New("Not configured")
	}

	ifo := mgo.DialInfo {
		Addrs:		[]string{conf.Mware.Mongo.c.Addr()},
		Database:	"admin",
		Timeout:	60*time.Second,
		Username:	conf.Mware.Mongo.c.User,
		Password:	conf.Mware.Mongo.c.Pass,
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
	sess, err := mgoDial()
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

func GetEnvMongo(ctx context.Context, mwd *MwareDesc) map[string][]byte {
	e := mwd.stdEnvs(conf.Mware.Mongo.c.Addr())
	e[mwd.envName("DBNAME")] = []byte(mwd.Namespace)
	return e
}

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
}

func InfoMongo(ctx context.Context, mwd *MwareDesc, ifo *swyapi.MwareInfo) error {
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

func TInfoMongo(ctx context.Context) *swyapi.MwareTypeInfo {
	return &swyapi.MwareTypeInfo{
		Envs: stdEnvNames("mongo", "DBNAME"),
	}
}

var MwareMongo = MwareOps {
	Init:	InitMongo,
	Fini:	FiniMongo,
	GetEnv:	GetEnvMongo,
	Info:	InfoMongo,
	TInfo:	TInfoMongo,
	LiteOK:	true,
}
