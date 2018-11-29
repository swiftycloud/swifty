package main

import (
	"log"
	"time"
	"sync"
	"errors"
	"sync/atomic"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var quotaCheckThresh uint32 = 4
var unlockScanPeriod time.Duration = time.Minute
var quotas sync.Map

type cheqReq struct {
	db	string
	locked	bool
}

var cheq chan *cheqReq

type dbQState struct {
	check	uint32
	locked	bool
}

func quotaLocked(db string) bool {
	x, _ := quotas.LoadOrStore(db, &dbQState{})
	qs := x.(*dbQState)
	if qs.locked {
		return true
	}

	if atomic.AddUint32(&qs.check, 1) % quotaCheckThresh == 0 {
		cheq <-&cheqReq{db: db, locked: false}
	}

	return false
}

func quotaSetLocked(db string, val bool) {
	x, ok := quotas.Load(db)
	if ok {
		qs := x.(*dbQState)
		qs.locked = val
	} else {
		log.Printf("Q: cannot mark %s locked=%v\n", db, val)
	}
}

var growOps = map[string]bool {
	"insert":	true,
	"update":	true,
}

type quota struct {}

func (*quota)config(mc map[string]interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error: %s\n", r)
			err = errors.New("Error parsing config")
		}
	}()

	if x, ok := mc["check_thresh"]; ok {
		quotaCheckThresh = uint32(x.(int))
		log.Printf("Set quota check thresh to %v\n", quotaCheckThresh)
	}

	if x, ok := mc["unlock_period"]; ok {
		unlockScanPeriod, err = time.ParseDuration(x.(string))
		if err != nil {
			return err
		}
		log.Printf("Set unlock check period to %s\n", unlockScanPeriod.String())
	}

	if x, ok := mc["quotas"]; ok {
		colQuotas = x.(string)
		log.Printf("Set quotas collection to %s\n", colQuotas)
	}

	if x, ok := mc["methods"]; ok {
		for _, m := range x.([]interface{}) {
			growOps[m.(string)] = true
		}
		log.Printf("Set grow ops to %v\n", growOps)
	}

	return nil
}

func (*quota)request(conid string, rq *mongo_req) error {
	if rq.inf == nil {
		return nil
	}

	_, ok := growOps[rq.inf.act]
	if ok && quotaLocked(rq.inf.db) {
		log.Printf("%s: Q: quota exceeded for %s, stopping\n", conid, rq.inf.db)
		return errors.New("quota force abort")
	}

	return nil
}

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
	DSize	uint64	`bson:"dataSize"`
	Indexes	uint32	`bson:"indexes"`
}

func quotaCheckDB(rq *cheqReq) {
	log.Printf("Q: Will check quota for %s (locked %v)\n", rq.db, rq.locked)

	sess, err := mgo.DialWithInfo(pinfo)
	if err != nil {
		log.Printf("Q: error dialing (c): %s\n", err.Error())
		return
	}
	defer sess.Close()

	var st MgoStat
	err = sess.DB(rq.db).Run("dbStats", &st)
	if err != nil {
		log.Printf("Q: can't get dbStats for %s: %s\n", rq.db, err.Error())
		if err == mgo.ErrNotFound {
			quotas.Delete(rq.db)
		}
		return
	}

	if quotaExceeded(sess, rq.db, &st) {
		if !rq.locked {
			quotaSetLocked(rq.db, true)
		}
	} else {
		if rq.locked {
			quotaSetLocked(rq.db, false)
		}
	}
}

func init() {
	cheq = make(chan *cheqReq)
	go func() {
		for {
			quotaCheckDB(<-cheq)
		}
	}()
	go func() {
		for {
			time.Sleep(unlockScanPeriod)
			quotas.Range(func(k, v interface{}) bool {
				q := v.(*dbQState)
				if q.locked {
					cheq <-&cheqReq{db: k.(string), locked: true}
				}
				return true
			})
		}
	}()
}

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
