package main

import (
	"fmt"
	"swifty"
	"gopkg.in/mgo.v2/bson"
)

func Main(rq *Request) (interface{}, *Responce) {
	db, err := swifty.MongoDatabase(rq.Args["dbname"])
	if err != nil {
		fmt.Println(err)
		panic("Can't get mgo dbase")
	}

	col := db.C(rq.Args["collection"])
	res := "done"

	switch rq.Args["action"] {
	case "insert":
		err = col.Insert(bson.M{"key": rq.Args["key"], "val": rq.Args["val"]})
	case "select":
		var ent struct { Key, Val string }

		err = col.Find(bson.M{"key": rq.Args["key"]}).One(&ent)
		if err == nil {
			res = ent.Val
		}
	default:
		res = "invalid"
	}

	if err != nil {
		res = "operation failed"
	}

	return map[string]string{"res": res}, nil
}
