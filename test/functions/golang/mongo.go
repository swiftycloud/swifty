package swifty

import (
	"fmt"
	"gopkg.in/mgo.v2/bson"
)

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
