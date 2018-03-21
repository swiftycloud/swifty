package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"../common"
	"../apis/apps"
)

const (
	DBSwiftyDB	="swifty"
	DBTenantDB	= "swifty-tenant"
	DBColLimits	= "Limits"
)

var dbSession *mgo.Session

func dbGetUserLimits(conf *YAMLConf, id string) (*swyapi.UserLimits, error) {
	c := dbSession.DB(DBTenantDB).C(DBColLimits)
	var v swyapi.UserLimits
	err := c.Find(bson.M{"id":id}).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return &v, err
}

func dbSetUserLimits(conf *YAMLConf, limits *swyapi.UserLimits) error {
	c := dbSession.DB(DBTenantDB).C(DBColLimits)
	_, err := c.Upsert(bson.M{"id":limits.Id}, limits)
	return err
}

func dbConnect(conf *YAMLConf) error {
	var err error

	dbc := swy.ParseXCreds(conf.DB)
	info := mgo.DialInfo{
		Addrs:		[]string{dbc.AddrPort()},
		Database:	DBSwiftyDB,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	admdSecrets[dbc.Pass]}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, DBTenantDB, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()

	log.Debugf("Connected to mongo:%s", DBTenantDB)
	return nil
}
