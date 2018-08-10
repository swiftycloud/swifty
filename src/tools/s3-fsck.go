package main

import (
	"fmt"
	"flag"
	"time"
	"strings"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"../s3/mgo"
)

const (
	DBName					= "swifty-s3"
	DBColS3Iams				= "S3Iams"
	DBColS3Buckets				= "S3Buckets"
	DBColS3Objects				= "S3Objects"
	DBColS3Uploads				= "S3Uploads"
	DBColS3ObjectData			= "S3ObjectData"
	DBColS3DataChunks			= "S3DataChunks"
	DBColS3AccessKeys			= "S3AccessKeys"
	DBColS3Websites				= "S3Websites"
)

var session *mgo.Session

func dbConnect(user, pass, host string) error {
	info := mgo.DialInfo{
		Addrs:		[]string{host},
		Database:	DBName,
		Timeout:	60 * time.Second,
		Username:	user,
		Password:	pass}

	var err error
	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		fmt.Printf("Can't dial %s@%s: %s\n", user, host, err.Error())
		return err
	}

	session.SetMode(mgo.Monotonic, true)
	return nil
}

var accounts map[string]*s3mgo.S3Account
var accnsid map[string]*s3mgo.S3Account

func checkAccounts() error {
	var acs []*s3mgo.S3Account

	err := session.DB(DBName).C(DBColS3Iams).Find(bson.M{"namespace":bson.M{"$exists":1}}).All(&acs)
	if err != nil {
		fmt.Printf("Can't lookup accounts: %s", err.Error())
		return err
	}

	accounts = make(map[string]*s3mgo.S3Account)
	accnsid = make(map[string]*s3mgo.S3Account)
	fmt.Printf("Accounts:\n")
	for _, ac := range acs {
		accounts[ac.ObjID.Hex()] = ac
		accnsid[ac.NamespaceID()] = ac
		fmt.Printf("\t%s: ns=%s acid=%s\n", ac.ObjID.Hex(), ac.Namespace, ac.ObjID.Hex())
	}

	return nil
}

var iams map[string]*s3mgo.S3Iam

func checkIams() error {
	var is []*s3mgo.S3Iam

	err := session.DB(DBName).C(DBColS3Iams).Find(bson.M{"namespace":bson.M{"$exists":0}}).All(&is)
	if err != nil {
		fmt.Printf("Can't lookup iams: %s", err.Error())
		return err
	}

	iams = make(map[string]*s3mgo.S3Iam)
	fmt.Printf("IAMs:\n")
	for _, iam := range is {
		_, ok := accounts[iam.AccountObjID.Hex()]
		if !ok {
			fmt.Printf("\tDangling IAM %s\n", iam.ObjID.Hex())
			continue
		}

		iams[iam.ObjID.Hex()] = iam
		fmt.Printf("\t%s: ac=..%s %-32s %x\n", iam.ObjID.Hex(),
				iam.AccountObjID.Hex()[12:],
				strings.Join(iam.Policy.Resource, ", "),
				iam.Policy.Action.ToSwy())

	}

	return nil
}

func checkKeys() error {
	var keys []*s3mgo.S3AccessKey

	err := session.DB(DBName).C(DBColS3AccessKeys).Find(bson.M{}).All(&keys)
	if err != nil {
		fmt.Printf("Can't lookup keys: %s", err.Error())
		return err
	}

	fmt.Printf("Keys:\n")
	for _, key := range keys {
		_, ok := accounts[key.AccountObjID.Hex()]
		if !ok {
			fmt.Printf("\tDangling Key %s (no account)\n", key.ObjID.Hex())
			continue
		}

		_, ok = iams[key.IamObjID.Hex()]
		if !ok {
			fmt.Printf("\tDangling Key %s (no iam)\n", key.ObjID.Hex())
			continue
		}

		var exp = ""
		if key.Expired() {
			exp = " (expired)"
		} else if key.ExpirationTimestamp == s3mgo.S3TimeStampMax {
			exp = " (perpetual)"
		}
		fmt.Printf("\t%s: ac=..%s iam=..%s %s%s\n", key.ObjID.Hex(),
				key.AccountObjID.Hex()[12:], key.IamObjID.Hex()[12:],
				key.AccessKeyID, exp)
	}

	return nil
}

var buckets map[string]*s3mgo.S3Bucket

func checkBuckets() error {
	var bks []*s3mgo.S3Bucket

	err := session.DB(DBName).C(DBColS3Buckets).Find(bson.M{}).All(&bks)
	if err != nil {
		fmt.Printf("Can't lookup buckets: %s", err.Error())
		return err
	}

	buckets = make(map[string]*s3mgo.S3Bucket)
	fmt.Printf("Buckets:\n")
	for _, b := range(bks) {
		ac, ok := accnsid[b.NamespaceID]
		if !ok {
			fmt.Printf("\tDangling bucket %s (not in act ns)\n", b.ObjID.Hex())
			continue
		}

		bcookie := ac.BCookie(b.Name)
		if b.BCookie != bcookie {
			fmt.Printf("\tCorrupted Bucket %s (name %s cookie %s want %s)\n", b.ObjID.Hex(), b.Name, b.BCookie, bcookie)
			continue
		}

		st := ""
		_, ok = iams[b.IamObjID.Hex()]
		if !ok {
			/* FIXME -- iams can go away with keys expired */
			st += " noiam!"
		}

		buckets[b.ObjID.Hex()] = b
		fmt.Printf("\t%s: ac=..%s name=%-24s c=%s.. %s\n", b.ObjID.Hex(),
				ac.ObjID.Hex()[12:], b.Name, b.BCookie[:8], st)
	}

	return nil
}

func checkObjects() error {
	var objs []s3mgo.S3Object

	err := session.DB(DBName).C(DBColS3Objects).Find(bson.M{}).All(&objs)
	if err != nil {
		fmt.Printf("Can't lookup objects: %s", err.Error())
		return err
	}

	fmt.Printf("Objects:\n")
	for _, o := range(objs) {
		b, ok := buckets[o.BucketObjID.Hex()]
		if !ok {
			fmt.Printf("\t!!! Dangling object %s (no bucket)\n", o.ObjID.Hex())
			continue
		}

		ocookie := b.OCookie(o.Key, o.Version)
		if o.OCookie != ocookie {
			fmt.Printf("\t!!! Corrupted Object %s (key %s.%d cookie %s want %s)\n", o.ObjID.Hex(),
					o.Key, o.Version, o.OCookie[:6], ocookie[:6])
			continue
		}

		st := ""
		_, ok = iams[b.IamObjID.Hex()]
		if !ok {
			/* FIXME -- iams can go away with keys expired */
			st += " noiam!"
		}
		if o.Version != 1 {
			st += " ver"
		}

		fmt.Printf("\t%s: bk=..%s key=%-32s c=%s.. %s\n", o.ObjID.Hex(),
				b.ObjID.Hex()[12:], b.Name + "::" + o.Key, o.OCookie[:8], st)
	}

	return nil
}

func main() {
	var user string
	var pass string
	var host string

	flag.StringVar(&user, "user", "swifty-s3", "user name")
	flag.StringVar(&pass, "pass", "", "password")
	flag.StringVar(&host, "host", "127.0.0.1", "db host")
	flag.Parse()

	err := dbConnect(user, pass, host)
	if err != nil {
		return
	}

	err = checkAccounts()
	if err != nil {
		return
	}

	err = checkIams()
	if err != nil {
		return
	}

	err = checkKeys()
	if err != nil {
		return
	}

	err = checkBuckets()
	if err != nil {
		return
	}

	err = checkObjects()
	if err != nil {
		return
	}
}
