package main

import (
	"gopkg.in/mgo.v2"

	"time"
)

var dbSession *mgo.Session
var dbName string

const (
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3Keys				= "S3Keys"
	DBColS3AccessKeys			= "S3AccessKeys"
)

func dbConnect(conf *YAMLConf) error {
	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	conf.DB.Name,
		Timeout:	60 * time.Second,
		Username:	conf.DB.User,
		Password:	conf.DB.Pass}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB.Addr, conf.DB.Name, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()
	dbName = conf.DB.Name

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"oid"}
	dbSession.DB(dbName).C(DBColS3Buckets).EnsureIndex(index)
	dbSession.DB(dbName).C(DBColS3Objects).EnsureIndex(index)

	index.Key = []string{"access-key-id"}
	dbSession.DB(dbName).C(DBColS3AccessKeys).EnsureIndex(index)

	return nil
}

func dbDisconnect() {
	dbSession.Close()
	dbSession = nil
	dbName = ""
}
