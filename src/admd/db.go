package main

import (
	"gopkg.in/mgo.v2"
	"time"
)

const (
	DBTenantDB	= "swifty-tenant"
)

var dbSession *mgo.Session

func dbConnect(conf *YAMLConf) error {
	var err error

	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	DBTenantDB,
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
