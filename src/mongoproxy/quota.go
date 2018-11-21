package main

import (
	"log"
	"sync"
	"errors"
	"sync/atomic"
	"gopkg.in/mgo.v2"
)

const quotaCheckThresh uint32 = 4

var quotas sync.Map
var cheq chan string

type dbQuota struct {
	check	uint32
	locked	bool
}

type quota struct {}

func quotaLocked(db string) bool {
	x, _ := quotas.LoadOrStore(db, &dbQuota{})
	qs := x.(*dbQuota)
	if qs.locked {
		return true
	}

	if atomic.AddUint32(&qs.check, 1) % quotaCheckThresh == 0 {
		cheq <-db
	}

	return false
}

func quotaLock(db string) {
	x, ok := quotas.Load(db)
	if ok {
		qs := x.(*dbQuota)
		qs.locked = true
	} else {
		log.Printf("Q: cannot mark %s locked\n", db)
	}
}

var growOps = map[string]bool {
	"insert":	true,
	"update":	true,
}

func (*quota)request(conid string, rq *mongo_req) error {
	if rq.inf == nil {
		return nil
	}

	_, ok := growOps[rq.inf.act]
	if ok && quotaLocked(rq.inf.db) {
		log.Printf("Q: quota exceeded for %s, stopping\n", rq.inf.db)
		return errors.New("quota force abort")
	}

	return nil
}

var pinfo *mgo.DialInfo

func quotaSetCreds(addr, user, pass, db string) {
	pinfo = &mgo.DialInfo {
		Addrs:		[]string{addr},
		Database:	db,
		Username:	user,
		Password:	pass,
	}
}

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
	DSize	uint64	`bson:"dataSize"`
	Indexes	uint32	`bson:"indexes"`
}

func quotaCheckDB(db string) {
	log.Printf("Q: Will check quota for %s\n", db)

	sess, err := mgo.DialWithInfo(pinfo)
	if err != nil {
		log.Printf("Q: error dialing: %s\n", err.Error())
		return
	}
	defer sess.Close()

	var st MgoStat
	err = sess.DB(db).Run("dbStats", &st)
	if err != nil {
		log.Printf("Q: can't get dbStats for %s: %s\n", db, err.Error())
		return
	}

	log.Printf("Q: db %s is %v\n", db, &st)
	if quotaExceeded(sess, db, &st) {
		quotaLock(db)
	}
}

func init() {
	cheq = make(chan string)
	go func() {
		for {
			quotaCheckDB(<-cheq)
		}
	}()
}
