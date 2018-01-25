package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"reflect"
	"time"
	"fmt"
)

var dbColMap map[reflect.Type]string
var dbSession *mgo.Session
var dbName string

const (
	DBColS3Iams				= "S3Iams"
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3Uploads				= "S3Uploads"
	DBColS3ObjectData			= "S3ObjectData"
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

	index.Key = []string{"iam"}
	dbSession.DB(dbName).C(DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"namespace"}
	dbSession.DB(dbName).C(DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"bid"}
	dbSession.DB(dbName).C(DBColS3Buckets).EnsureIndex(index)

	index.Key = []string{"bid"}
	dbSession.DB(dbName).C(DBColS3Objects).EnsureIndex(index)

	index.Key = []string{"uid"}
	dbSession.DB(dbName).C(DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"bid"}
	dbSession.DB(dbName).C(DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"access-key-id"}
	dbSession.DB(dbName).C(DBColS3AccessKeys).EnsureIndex(index)

	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf([]S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&[]S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(&S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf([]S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(&[]S3Bucket{})] = DBColS3Buckets
	dbColMap[reflect.TypeOf(S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(&S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf([]S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(&[]S3Object{})] = DBColS3Objects
	dbColMap[reflect.TypeOf(S3Upload{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf(&S3Upload{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf([]S3Upload{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf(&[]S3Upload{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf(S3UploadPart{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf(&S3UploadPart{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf([]S3UploadPart{})] = DBColS3Uploads
	dbColMap[reflect.TypeOf(&[]S3UploadPart{})] = DBColS3Uploads
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

func infoLong(o interface{}) (string) {
	switch (reflect.TypeOf(o)) {
	case reflect.TypeOf(&S3Bucket{}):
		bucket := o.(*S3Bucket)
		return fmt.Sprintf("{ S3Bucket: %s/%s/%s/%d/%s }",
			bucket.ObjID, bucket.BackendID,
			bucket.NamespaceID, bucket.State,
			bucket.Name)
	case reflect.TypeOf(&S3ObjectData{}):
		objd := o.(*S3ObjectData)
		return fmt.Sprintf("{ S3ObjectData: %s/%s/%s/%s/%d/%d }",
			objd.ObjID, objd.RefID,
			objd.BucketBID, objd.ObjectBID,
			objd.State, objd.Size)
	case reflect.TypeOf(&S3Object{}):
		object := o.(*S3Object)
		return fmt.Sprintf("{ S3Object: %s/%s/%s/%d/%s }",
			object.ObjID, object.BucketObjID,
			object.BackendID, object.State,
			object.Key)
	case reflect.TypeOf(&S3Upload{}):
		upload := o.(*S3Upload)
		return fmt.Sprintf("{ S3Upload: %s/%s/%s/%d/%d/%s }",
			upload.ObjID, upload.BucketObjID,
			upload.UploadID, upload.Ref, upload.Key)
	case reflect.TypeOf(&S3UploadPart{}):
		part := o.(*S3UploadPart)
		return fmt.Sprintf("{ S3UploadPart: %s/%s/%s/%d/%s }",
			part.ObjID, part.UploadObjID,
			part.BackendID, part.Part,
			part.Key)
	}
	return "{ Unknown type }"
}

func dbS3SetObjID(o interface{}, query bson.M) {
	if _, ok := query["_id"]; ok == false {
		val := reflect.ValueOf(o).Elem()
		query["_id"] = val.FieldByName("ObjID").Interface().(bson.ObjectId)
	}
}

func dbS3Insert(o interface{}) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Insert(o)
}

func dbS3Remove(o interface{}, query bson.M) (error) {
	return dbSession.DB(dbName).C(dbColl(o)).Remove(query)
}

func dbS3Update(query bson.M, update bson.M, o interface{}) (error) {
	dbS3SetObjID(o, query)
	c := dbSession.DB(dbName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	true,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3SetOnState(o interface{}, state uint32, query bson.M, fields bson.M) (error) {
	if query == nil { query = make(bson.M) }

	query["state"] = bson.M{"$in": s3StateTransition[state]}
	update := bson.M{"$set": fields}

	err := dbS3Update(query, update, o)
	if err != nil {
		log.Errorf("s3: Can't set state %d on %s: %s",
			state, infoLong(o), err.Error())
	}
	return err
}

func dbS3SetState(o interface{}, state uint32, query bson.M) (error) {
	return dbS3SetOnState(o, state, query, bson.M{"state": state})
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
