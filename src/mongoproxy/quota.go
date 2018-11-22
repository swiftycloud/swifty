package main

import (
	"log"
	"time"
	"sync"
	"errors"
	"sync/atomic"
	"gopkg.in/mgo.v2"
)

var quotaCheckThresh uint32 = 4
var unlockScanPeriod time.Duration = time.Minute
var quotas sync.Map
var cheq chan string

type dbQuota struct {
	check	uint32
	locked	bool
}

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

func quotaUnlock(qs *dbQuota) {
	qs.locked = false
}

var growOps = map[string]bool {
	"insert":	true,
	"update":	true,
}

type quota struct {}

func (*quota)config(mc map[string]interface{}, conf *Config) error {
	if conf.Target.Addr == "" {
		return errors.New("No target.address")
	}
	if conf.Target.DB == "" {
		return errors.New("No target.db")
	}
	if conf.Target.User == "" {
		return errors.New("No target.user")
	}
	if conf.Target.Pass == "" {
		return errors.New("No target.password")
	}

	pinfo = &mgo.DialInfo {
		Addrs:		[]string{conf.Target.Addr},
		Database:	conf.Target.DB,
		Username:	conf.Target.User,
		Password:	conf.Target.Pass,
	}

	if x, ok := mc["check_thresh"]; ok {
		switch y := x.(type) {
		case int:
			quotaCheckThresh = uint32(y)
		case float64:
			quotaCheckThresh = uint32(y)
		default:
			return errors.New("check_thres must be integer")
		}
		log.Printf("Set quota check thresh to %v\n", quotaCheckThresh)
	}

	if x, ok := mc["unlock_period"]; ok {
		switch y := x.(type) {
		case string:
			var err error
			unlockScanPeriod, err = time.ParseDuration(y)
			if err != nil {
				return err
			}
		default:
			return errors.New("unlock_period must be string")
		}
		log.Printf("Set unlock check period to %s\n", unlockScanPeriod.String())
	}

	if x, ok := mc["quotas"]; ok {
		switch y := x.(type) {
		case string:
			colQuotas = y
		default:
			return errors.New("quotas must be string")
		}
		log.Printf("Set quotas collection to %s\n", colQuotas)
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

var pinfo *mgo.DialInfo

type MgoStat struct {
	ISize	uint64	`bson:"indexSize"`
	SSize	uint64	`bson:"storageSize"`
	DSize	uint64	`bson:"dataSize"`
	Indexes	uint32	`bson:"indexes"`
}

func quotaCheckDB(db string) {
	log.Printf("Q: Will check if quota exceeded for %s\n", db)

	sess, err := mgo.DialWithInfo(pinfo)
	if err != nil {
		log.Printf("Q: error dialing (c): %s\n", err.Error())
		return
	}
	defer sess.Close()

	var st MgoStat
	err = sess.DB(db).Run("dbStats", &st)
	if err != nil {
		log.Printf("Q: can't get dbStats for %s: %s\n", db, err.Error())
		return
	}

	if quotaExceeded(sess, db, &st) {
		quotaLock(db)
	}
}

func maybeUnlockQuota(sess *mgo.Session, db string, q *dbQuota) {
	if !q.locked {
		return
	}

	log.Printf("Q: Will check if quota clamped for %s\n", db)

	var st MgoStat
	err := sess.DB(db).Run("dbStats", &st)
	if err != nil {
		if err == mgo.ErrNotFound {
			log.Printf("Q: No stats for %s -- dropping the qouta cache\n", db)
			quotas.Delete(db)
		} else {
			log.Printf("Q: can't get dbStats for %s: %s\n", db, err.Error())
		}
		return
	}

	if !quotaExceeded(sess, db, &st) {
		log.Printf("Q: DB %s is OK, unlocking\n", db)
		quotaUnlock(q)
	}
}

func maybeUnlockQuotas() {
	for {
		time.Sleep(unlockScanPeriod)
		sess, err := mgo.DialWithInfo(pinfo)
		if err != nil {
			log.Printf("Q: error dialing (u): %s\n", err.Error())
			continue
		}
		quotas.Range(func(k, v interface{}) bool {
			maybeUnlockQuota(sess, k.(string), v.(*dbQuota))
			return true
		})
		sess.Close()
	}
}

func init() {
	cheq = make(chan string)
	go func() {
		for {
			quotaCheckDB(<-cheq)
		}
	}()
	go maybeUnlockQuotas()
}
