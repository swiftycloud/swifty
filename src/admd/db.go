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

func dbGetPlanLimits(ses *mgo.Session, id bson.ObjectId) (*PlanLimits, error) {
	c := ses.DB(DBTenantDB).C(DBColPlans)
	var v PlanLimits
	q := bson.M{"_id": id}
	err := c.Find(q).One(&v)
	if err == mgo.ErrNotFound {
		err = nil
	}

	return &v, err
}

func dbAddPlanLimits(ses *mgo.Session, pl *PlanLimits) error {
	c := ses.DB(DBTenantDB).C(DBColPlans)
	return c.Insert(pl)
}

func dbListPlanLimits(ses *mgo.Session) ([]*PlanLimits, error) {
	c := ses.DB(DBTenantDB).C(DBColPlans)
	var v []*PlanLimits
	err := c.Find(bson.M{}).All(&v)
	return v, err
}

func dbDelPlanLimits(ses *mgo.Session, id bson.ObjectId) error {
	c := ses.DB(DBTenantDB).C(DBColPlans)
	return c.Remove(bson.M{"_id": id})
}

func dbSetUserLimits(ses *mgo.Session, conf *YAMLConf, limits *swyapi.UserLimits) error {
	c := ses.DB(DBTenantDB).C(DBColLimits)
	_, err := c.Upsert(bson.M{"uid":limits.UId}, limits)
	return err
}

func dbDelUserLimits(ses *mgo.Session, conf *YAMLConf, id string) error {
	c := ses.DB(DBTenantDB).C(DBColLimits)
	err := c.Remove(bson.M{"uid":id})
	if err == mgo.ErrNotFound {
		err = nil
	}
	return err
}

func dbConnect(conf *YAMLConf) error {
	var err error

	dbc := xh.ParseXCreds(conf.DB)
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
