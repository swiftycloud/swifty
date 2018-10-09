package main

import (
	"fmt"
	"flag"
	"time"
	"strings"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"swifty/s3/mgo"
)

const (
	DBName					= "swifty-s3"
	DBColS3Iams				= "S3Iams"
	DBColS3Stats				= "S3Stats"
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

var stats map[string]*s3mgo.AcctStats
func checkStats() error {
	var sts []*s3mgo.AcctStats

	err := session.DB(DBName).C(DBColS3Stats).Find(bson.M{}).All(&sts)
	if err != nil {
		fmt.Printf("Can't lookup accounts: %s", err.Error())
		return err
	}

	stats = make(map[string]*s3mgo.AcctStats)
	fmt.Printf("   Stats:\n")
	for _, st := range sts {
		stats[st.NamespaceID] = st
		fmt.Printf("\t%s: nsid=%s\n", st.ObjID.Hex(), st.NamespaceID)
	}

	return nil
}

var accounts map[string]*s3mgo.Account
var accnsid map[string]*s3mgo.Account

func checkAccounts() error {
	var acs []*s3mgo.Account

	err := session.DB(DBName).C(DBColS3Iams).Find(bson.M{"namespace":bson.M{"$exists":1}}).All(&acs)
	if err != nil {
		fmt.Printf("Can't lookup accounts: %s", err.Error())
		return err
	}

	accounts = make(map[string]*s3mgo.Account)
	accnsid = make(map[string]*s3mgo.Account)
	fmt.Printf("   Accounts:\n")
	for _, ac := range acs {
		accounts[ac.ObjID.Hex()] = ac
		accnsid[ac.NamespaceID()] = ac
		fmt.Printf("\t%s: ns=%s acid=%s\n", ac.ObjID.Hex(), ac.Namespace, ac.ObjID.Hex())
	}

	return nil
}

var iams map[string]*s3mgo.Iam

func checkIams() error {
	var is []*s3mgo.Iam

	err := session.DB(DBName).C(DBColS3Iams).Find(bson.M{"namespace":bson.M{"$exists":0}}).All(&is)
	if err != nil {
		fmt.Printf("Can't lookup iams: %s", err.Error())
		return err
	}

	iams = make(map[string]*s3mgo.Iam)
	fmt.Printf("   IAMs:\n")
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
	var keys []*s3mgo.AccessKey

	err := session.DB(DBName).C(DBColS3AccessKeys).Find(bson.M{}).All(&keys)
	if err != nil {
		fmt.Printf("Can't lookup keys: %s", err.Error())
		return err
	}

	fmt.Printf("   Keys:\n")
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
		} else if key.ExpirationTimestamp == s3mgo.TimeStampMax {
			exp = " (perpetual)"
		}
		fmt.Printf("\t%s: ac=..%s iam=..%s %s%s\n", key.ObjID.Hex(),
				key.AccountObjID.Hex()[12:], key.IamObjID.Hex()[12:],
				key.AccessKeyID, exp)
	}

	return nil
}

var buckets map[string]*s3mgo.Bucket

func checkBuckets() error {
	var bks []*s3mgo.Bucket

	err := session.DB(DBName).C(DBColS3Buckets).Find(bson.M{}).All(&bks)
	if err != nil {
		fmt.Printf("Can't lookup buckets: %s", err.Error())
		return err
	}

	buckets = make(map[string]*s3mgo.Bucket)
	fmt.Printf("   Buckets:\n")
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

		buckets[b.ObjID.Hex()] = b
		fmt.Printf("\t%s: ac=..%s name=%-24s c=%s.. o=%d/%d%s\n", b.ObjID.Hex(),
				ac.ObjID.Hex()[12:], b.Name, b.BCookie[:8], b.CntBytes, b.CntObjects, st)
	}

	return nil
}

var objects map[string]*s3mgo.Object

func checkObjects() error {
	var objs []*s3mgo.Object

	err := session.DB(DBName).C(DBColS3Objects).Find(bson.M{}).All(&objs)
	if err != nil {
		fmt.Printf("Can't lookup objects: %s", err.Error())
		return err
	}

	objects = make(map[string]*s3mgo.Object)
	fmt.Printf("   Objects:\n")
	for _, o := range(objs) {
		b, ok := buckets[o.BucketObjID.Hex()]
		if !ok {
			fmt.Printf("!!!\t Dangling object %s (no bucket)\n", o.ObjID.Hex())
			continue
		}

		ocookie := b.OCookie(o.Key, o.Version)
		if o.OCookie != ocookie {
			fmt.Printf("!!!\t Corrupted Object %s (key %s.%d cookie %s want %s)\n", o.ObjID.Hex(),
					o.Key, o.Version, o.OCookie[:6], ocookie[:6])
			continue
		}

		s, ok := stats[b.NamespaceID]
		if !ok {
			fmt.Printf("!!!\t No stats for bucket %s\n", b.ObjID.Hex())
			continue
		}

		st := ""
		if o.Version != 1 {
			st += " ver"
		}

		b.CntObjects--
		b.CntBytes -= o.Size
		s.CntObjects--
		s.CntBytes -= o.Size

		objects[o.ObjID.Hex()] = o
		fmt.Printf("\t%s: bk=..%s key=%-32s c=%s.. s=%d%s\n", o.ObjID.Hex(),
				b.ObjID.Hex()[12:], b.Name + "::" + o.Key, o.OCookie[:8], o.Size, st)
	}

	for _, b := range(buckets) {
		if b.CntObjects != 0 {
			fmt.Printf("!!!\tBucket %s nobj mismatch, %d left\n", b.ObjID.Hex(), b.CntObjects)
		}
		if b.CntBytes != 0 {
			fmt.Printf("!!!\tBucket %s size mismatch, %d left\n", b.ObjID.Hex(), b.CntBytes)
		}
	}

	for _, s := range(stats) {
		if s.CntObjects != 0 {
			fmt.Printf("!!!\tStats %s nobj mismatch, %d left\n", s.ObjID.Hex(), s.CntObjects)
		}
		if s.CntBytes != 0 {
			fmt.Printf("!!!\tStats %s size mismatch, %d left\n", s.ObjID.Hex(), s.CntBytes)
		}
	}

	return nil
}

var pchunks map[string]*s3mgo.ObjectPart

func checkParts() error {
	var pts []*s3mgo.ObjectPart

	err := session.DB(DBName).C(DBColS3ObjectData).Find(bson.M{}).All(&pts)
	if err != nil {
		fmt.Printf("Can't lookup objects: %s", err.Error())
		return err
	}

	pchunks = make(map[string]*s3mgo.ObjectPart)
	fmt.Printf("   Parts:\n")
	for _, p := range(pts) {
		o, ok := objects[p.RefID.Hex()]
		if !ok {
			fmt.Printf("!!!\t Dangling Part %s (no object)\n", p.ObjID.Hex())
			continue
		}

		if o.OCookie != p.OCookie {
			fmt.Printf("!!!\t Corrupted Part %s (ocookie %s want %s)\n", p.ObjID.Hex(),
					o.OCookie[:6], p.OCookie[:6])
			continue
		}

		b := buckets[o.BucketObjID.Hex()]
		if b.BCookie != p.BCookie {
			fmt.Printf("!!!\t Corrupted Part %s (bcookie %s want %s)\n", p.ObjID.Hex(),
					o.OCookie[:6], p.BCookie[:6])
			continue
		}


		st := ""

		for _, ci := range(p.Chunks) {
			pchunks[ci.Hex()] = p
		}

		o.Size -= p.Size
		fmt.Printf("\t%s: #%d %-32s %s\n", p.ObjID.Hex(), p.Part,
				b.Name + "::" + o.Key, st)
	}

	for _, o := range(objects) {
		if o.Size != 0 {
			fmt.Printf("!!!\tObject %s size mismatch, %d left\n", o.ObjID.Hex(), o.Size)
		}
	}

	return nil
}

func checkChunks() error {
	var cks []*s3mgo.DataChunk

	err := session.DB(DBName).C(DBColS3DataChunks).Find(bson.M{}).Select(bson.M{"data":0}).All(&cks)
	if err != nil {
		fmt.Printf("Can't lookup objects: %s", err.Error())
		return err
	}

	fmt.Printf("   Chunks:\n")
	for _, c := range(cks) {
		_, ok := pchunks[c.ObjID.Hex()]
		if !ok {
			fmt.Printf("\tDangling chunk %s\n", c.ObjID.Hex())
		}
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

	err = checkStats()
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

	err = checkParts()
	if err != nil {
		return
	}

	err = checkChunks()
	if err != nil {
		return
	}
}
