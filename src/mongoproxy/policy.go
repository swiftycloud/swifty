package main

import (
	"log"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var colQuotas string = "Quotas"

type DbQuota struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	DB		string		`bson:"db"`
	DataLimit	uint64		`bson:"data_limit"`
	RealLimit	uint64		`bson:"real_limit"`
	VirtLimit	uint64		`bson:"virt_limit"`
	IndxLimit	uint64		`bson:"indx_limit"`
}

func exceeded(lim uint64, val uint64, db, l string) bool {
	if lim != 0 && val > lim {
		log.Printf("Q: DB %s %s quota exceeded %d > %d\n", db, l, val, lim)
		return true
	}

	return false
}

func quotaExceeded(ses *mgo.Session, db string, st *MgoStat) bool {
	var q DbQuota

	err := ses.DB(pinfo.Database).C(colQuotas).Find(bson.M{"db": db}).One(&q)
	if err != nil {
		if err != mgo.ErrNotFound {
			log.Printf("ERROR: cannot get quota for %s: %s\n", db, err.Error())
		}
		return false
	}

	if exceeded(q.RealLimit, st.SSize + st.ISize, db, "realize") {
		return true
	}

	if exceeded(q.DataLimit, st.DSize, db, "datasize") {
		return true
	}

	if exceeded(q.VirtLimit, st.DSize + st.ISize, db, "virtsize") {
		return true
	}

	if exceeded(q.IndxLimit, st.ISize, db, "indxsize") {
		return true
	}

	return false
}
