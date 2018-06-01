package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"reflect"
	"time"
	"fmt"
	"../common"
)

var dbColMap map[reflect.Type]string
var dbSession *mgo.Session

const (
	DBName					= "swifty-s3"
	DBColS3Iams				= "S3Iams"
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3Uploads				= "S3Uploads"
	DBColS3ObjectData			= "S3ObjectData"
	DBColS3AccessKeys			= "S3AccessKeys"
)

const (
	S3StateNone			= 0
	S3StateActive			= 1
	S3StateInactive			= 2
)

var s3StateTransition = map[uint32][]uint32 {
	S3StateNone:		[]uint32{ S3StateNone, },
	S3StateActive:		[]uint32{ S3StateNone, },
	S3StateInactive:	[]uint32{ S3StateActive, },
}

func dbConnect(conf *YAMLConf) error {
	dbc := swy.ParseXCreds(conf.DB)
	info := mgo.DialInfo{
		Addrs:		[]string{dbc.Addr()},
		Database:	DBName,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	s3Secrets[dbc.Pass]}

	session, err := mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, DBName, err.Error())
		return err
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	dbSession = session.Copy()

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"namespace"}
	dbSession.DB(DBName).C(DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"user"}
	dbSession.DB(DBName).C(DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"bcookie"}
	dbSession.DB(DBName).C(DBColS3Buckets).EnsureIndex(index)

	index.Key = []string{"ocookie"}
	dbSession.DB(DBName).C(DBColS3Objects).EnsureIndex(index)

	index.Key = []string{"bid"}
	dbSession.DB(DBName).C(DBColS3ObjectData).EnsureIndex(index)

	index.Key = []string{"uid"}
	dbSession.DB(DBName).C(DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"ucookie"}
	dbSession.DB(DBName).C(DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"access-key-id"}
	dbSession.DB(DBName).C(DBColS3AccessKeys).EnsureIndex(index)

	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf([]S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&[]S3Iam{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(S3Account{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&S3Account{})] = DBColS3Iams
	dbColMap[reflect.TypeOf([]S3Account{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(&[]S3Account{})] = DBColS3Iams
	dbColMap[reflect.TypeOf(S3AccessKey{})] = DBColS3AccessKeys
	dbColMap[reflect.TypeOf(&S3AccessKey{})] = DBColS3AccessKeys
	dbColMap[reflect.TypeOf([]S3AccessKey{})] = DBColS3AccessKeys
	dbColMap[reflect.TypeOf(&[]S3AccessKey{})] = DBColS3AccessKeys
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
}

func dbRepair() error {
	var err error

	log.Debugf("s3: Running db consistency test/repair")

	if err = s3RepairUpload(); err != nil {
		return err
	}

	if err = s3RepairObject(); err != nil {
		return err
	}

	if err = s3RepairObjectData(); err != nil {
		return err
	}

	if err = s3RepairBucket(); err != nil {
		return err
	}

	log.Debugf("s3: Finished db consistency test/repair")
	return nil
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
	case reflect.TypeOf(&S3AccessKey{}):
		akey := o.(*S3AccessKey)
		return fmt.Sprintf("{ S3AccessKey: %s/%s/%s/%d }",
			akey.ObjID, akey.IamObjID,
			akey.AccessKeyID, akey.State)
	case reflect.TypeOf(&S3Account{}):
		account := o.(*S3Account)
		return fmt.Sprintf("{ S3Account: %s/%s/%d/%s/%s }",
			account.ObjID, account.Namespace,
			account.State, account.User, account.Email)
	case reflect.TypeOf(&S3Iam{}):
		iam := o.(*S3Iam)
		return fmt.Sprintf("{ S3Iam: %s/%s/%d/%s }",
			iam.ObjID, iam.AccountObjID, iam.State,
			iam.Policy.infoLong())
	case reflect.TypeOf(&S3Bucket{}):
		bucket := o.(*S3Bucket)
		return fmt.Sprintf("{ S3Bucket: %s/%s/%s/%d/%s }",
			bucket.ObjID, bucket.BCookie,
			bucket.NamespaceID, bucket.State,
			bucket.Name)
	case reflect.TypeOf(&S3ObjectData{}):
		objd := o.(*S3ObjectData)
		return fmt.Sprintf("{ S3ObjectData: %s/%s/%s/%s/%s/%d/%d }",
			objd.ObjID, objd.RefID, objd.BackendID,
			objd.BucketBID, objd.ObjectBID,
			objd.State, objd.Size)
	case reflect.TypeOf(&S3Object{}):
		object := o.(*S3Object)
		return fmt.Sprintf("{ S3Object: %s/%s/%s/%d/%s }",
			object.ObjID, object.BucketObjID,
			object.OCookie, object.State,
			object.Key)
	case reflect.TypeOf(&S3Upload{}):
		upload := o.(*S3Upload)
		return fmt.Sprintf("{ S3Upload: %s/%s/%s/%d/%s }",
			upload.ObjID, upload.BucketObjID,
			upload.UploadID, upload.Ref, upload.Key)
	case reflect.TypeOf(&S3UploadPart{}):
		part := o.(*S3UploadPart)
		return fmt.Sprintf("{ S3UploadPart: %s/%s/%s/%d/%s }",
			part.ObjID, part.UploadObjID,
			part.UCookie, part.Part,
			part.Key)
	}
	return "{ Unknown type }"
}

func dbS3SetObjID(o interface{}, query bson.M) {
	if _, ok := query["_id"]; ok == false {
		elem := reflect.ValueOf(o).Elem()
		val := elem.FieldByName("ObjID")
		if val != reflect.ValueOf(nil) {
			id := val.Interface().(bson.ObjectId)
			if id != "" {
				query["_id"] = id
			}
		}
	}
}

func dbS3UpdateMTime(query bson.M) {
	if val, ok := query["$set"]; ok {
		val.(bson.M)["mtime"] = current_timestamp()
	}
}

func dbS3SetMTime(o interface{}) {
	elem := reflect.ValueOf(o).Elem()
	val := elem.FieldByName("MTime")
	if val != reflect.ValueOf(nil) {
		val.SetInt(current_timestamp())
	}
}

func dbS3Insert(o interface{}) (error) {
	dbS3SetMTime(o)

	err := dbSession.DB(DBName).C(dbColl(o)).Insert(o)
	if err != nil {
		log.Errorf("dbS3Insert: %s: %s", infoLong(o), err.Error())
	}
	return err
}

func dbS3Update(query bson.M, update bson.M, retnew bool, o interface{}) (error) {
	if query == nil { query = make(bson.M) }

	dbS3SetObjID(o, query)
	dbS3UpdateMTime(update)

	c := dbSession.DB(DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	retnew,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3Upsert(query bson.M, update bson.M, o interface{}) (error) {
	if query == nil { query = make(bson.M) }

	c := dbSession.DB(DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		true,
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

	err := dbS3Update(query, update, true, o)
	if err != nil {
		log.Errorf("s3: Can't set state %d on %s: %s",
			state, infoLong(o), err.Error())
	}
	return err
}

func dbS3SetState(o interface{}, state uint32, query bson.M) (error) {
	return dbS3SetOnState(o, state, query, bson.M{"state": state})
}

func dbS3RemoveCond(o interface{}, query bson.M) (error) {
	if query == nil { query = make(bson.M) }

	dbS3SetObjID(o, query)

	c := dbSession.DB(DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		true,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("dbS3RemoveCond: Can't remove %s: %s",
			infoLong(o), err.Error())
	}
	return err
}

func dbS3RemoveOnState(o interface{}, state uint32, query bson.M) (error) {
	if query == nil { query = make(bson.M) }

	query["state"] = state

	return dbS3RemoveCond(o, query)
}

func dbS3Remove(o interface{}) (error) {
	return dbS3RemoveCond(o, nil)
}

func dbS3FindOne(query bson.M, o interface{}) (error) {
	return dbSession.DB(DBName).C(dbColl(o)).Find(query).One(o)
}

func dbS3FindAll(query bson.M, o interface{}) (error) {
	return dbSession.DB(DBName).C(dbColl(o)).Find(query).All(o)
}

func dbS3FindAllInactive(o interface{}) (error) {
	states := bson.M{ "$in": []uint32{ S3StateNone, S3StateInactive } }
	query := bson.M{ "state": states }

	return dbS3FindAll(query, o)
}

func dbS3Pipe(o interface{}, pipeline interface{}) (*mgo.Pipe) {
	return dbSession.DB(DBName).C(dbColl(o)).Pipe(pipeline)
}
