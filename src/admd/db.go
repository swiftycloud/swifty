package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
	"../common"
	"../apis"
)

const (
	DBSwiftyDB	="swifty"
	DBTenantDB	= "swifty-tenant"
	DBColLimits	= "Limits"
	DBColPlans	= "Plans"
)

var session *mgo.Session

func dbGetUserLimits(ses *mgo.Session, conf *YAMLConf, id string) (*swyapi.UserLimits, error) {
	c := ses.DB(DBTenantDB).C(DBColLimits)
	var v swyapi.UserLimits
	err := c.Find(bson.M{"uid":id}).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return &v, err
}

func dbGetPlanLimits(ses *mgo.Session, conf *YAMLConf, id string) (*swyapi.UserLimits, error) {
	c := ses.DB(DBTenantDB).C(DBColPlans)
	var v swyapi.UserLimits
	err := c.Find(bson.M{"planid":id}).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}
	return &v, err
}

func dbSetUserLimits(ses *mgo.Session, conf *YAMLConf, limits *swyapi.UserLimits) error {
	c := ses.DB(DBTenantDB).C(DBColLimits)
	_, err := c.Upsert(bson.M{"uid":limits.UId}, limits)
	return err
}

func dbDelUserLimits(ses *mgo.Session, conf *YAMLConf, id string) {
	c := ses.DB(DBTenantDB).C(DBColLimits)
	c.Remove(bson.M{"uid":id})
}

func dbConnect(conf *YAMLConf) error {
	var err error

	dbc := swy.ParseXCreds(conf.DB)
	info := mgo.DialInfo{
		Addrs:		[]string{dbc.Addr()},
		Database:	DBSwiftyDB,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	admdSecrets[dbc.Pass]}

	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, DBTenantDB, err.Error())
		return err
	}

	session.SetMode(mgo.Monotonic, true)

	log.Debugf("Connected to mongo:%s", DBTenantDB)
	return nil
}
