package swifty

import (
	"fmt"
	"os"
	"strings"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var ses *mgo.Session

func MongoDatabase(mwn string) (*mgo.Database, error) {
	mwn = strings.ToUpper(mwn)
	dbn := os.Getenv("MWARE_MONGO" + mwn + "_DBNAME")

	if ses == nil {
		var err error

		addr := os.Getenv("MWARE_MONGO" + mwn + "_ADDR")
		user := os.Getenv("MWARE_MONGO" + mwn + "_USER")
		pass := os.Getenv("MWARE_MONGO" + mwn + "_PASS")

		info := mgo.DialInfo{
			Addrs:		[]string{addr},
			Database:	dbn,
			Username:	user,
			Password:	pass,
		}

		ses, err = mgo.DialWithInfo(&info);
		if err != nil {
			return nil, err
		}
	}

	return ses.DB(dbn), nil
}

func Main(args map[string]string) interface{} {
	db, err := MongoDatabase(args["dbname"])
	if err != nil {
		fmt.Println(err)
		panic("Can't get mgo dbase")
	}

	col := db.C(args["collection"])
	res := "done"

	switch args["action"] {
	case "insert":
		err = col.Insert(bson.M{"key": args["key"], "val": args["val"]})
	case "select":
		var ent struct { Key, Val string }

		err = col.Find(bson.M{"key": args["key"]}).One(&ent)
		if err == nil {
			res = ent.Val
		}
	default:
		res = "invalid"
	}

	if err != nil {
		res = "operation failed"
	}

	return map[string]string{"res": res}
}
