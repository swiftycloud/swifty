package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"reflect"
	"time"
)

var dbColMap map[reflect.Type]string
var dbSession *mgo.Session
var dbName string

const (
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3ObjectData			= "S3ObjectData"
	DBColS3Keys				= "S3Keys"
	DBColS3AccessKeys			= "S3AccessKeys"
)

func dbConnect(conf *YAMLConf) error {
	info := mgo.DialInfo{
		Addrs:		[]string{conf.DB.Addr},
		Database:	conf.DB.Name,
		Timeout:	60 * time.Second,
		Username:	conf.DB.User,
		Password:	s3Secrets[conf.DB.Pass]}

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

	index.Key = []string{"_id", "bid"}
	dbSession.DB(dbName).C(DBColS3Buckets).EnsureIndex(index)
	index.Key = []string{"_id", "bid"}
	dbSession.DB(dbName).C(DBColS3Objects).EnsureIndex(index)
	index.Key = []string{"_id", "next-id"}
	dbSession.DB(dbName).C(DBColS3ObjectData).EnsureIndex(index)

	index.Key = []string{"_id", "access-key-id"}
	dbSession.DB(dbName).C(DBColS3AccessKeys).EnsureIndex(index)

	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(&S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf([]S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(&[]S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(&S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf([]S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(&[]S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(S3ObjectData{})] = DBColS3ObjectData
	dbColMap[reflect.TypeOf(&S3ObjectData{})] = DBColS3ObjectData
	dbColMap[reflect.TypeOf([]S3ObjectData{})] = DBColS3ObjectData
	dbColMap[reflect.TypeOf(&[]S3ObjectData{})] = DBColS3ObjectData

	return nil
}

func dbDisconnect() {
	dbSession.Close()
	dbSession = nil
	dbName = ""
}

func dbColl(object interface{}) (string) {
	if name, ok := dbColMap[reflect.TypeOf(object)]; ok {
		return name
	}
	log.Fatalf("Unmapped object %v", object)
	return ""
}

func dbS3Insert(o interface{}) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Insert(o)
}

func dbS3Remove(o interface{}, query bson.M) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Remove(query)
}

func dbS3Update(query bson.M, update bson.M, o interface{}) (error) {
	c := dbSession.DB(dbName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3RemoveCond(query bson.M, o interface{}) (error) {
	c := dbSession.DB(dbName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		true,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3FindOne(query bson.M, o interface{}) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Find(query).One(o)
}

func dbS3FindAll(query bson.M, o interface{}) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Find(query).All(o)
}
