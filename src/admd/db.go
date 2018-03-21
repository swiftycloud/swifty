package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
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

	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	DBSwiftyDB,
		Timeout:	60 * time.Second,
		Username:	conf.DB.User,
		Password:	admdSecrets[conf.DB.Pass]}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB.Addr, DBTenantDB, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()

	log.Debugf("Connected to mongo:%s", DBTenantDB)
	return nil
}
