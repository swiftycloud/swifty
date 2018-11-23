package main

import (
	"log"
	"sync"
	"time"
	"errors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type ratelimit struct {}

var colRates string = "Rates"
var ratesCacheDuration time.Duration = 10 * time.Minute

func (*ratelimit)config(cfg map[string]interface{}) error {
	if x, ok := cfg["rates"]; ok {
		switch y := x.(type) {
		case string:
			colRates = y
		default:
			return errors.New("rates must be string")
		}
		log.Printf("Set quotas collection to %s\n", colRates)
	}

	if x, ok := cfg["cache_duration"]; ok {
		switch y := x.(type) {
		case string:
			var err error
			ratesCacheDuration, err = time.ParseDuration(y)
			if err != nil {
				return err
			}
		default:
			return errors.New("cache_duration must be string")
		}
		log.Printf("Set rates cache duration to %s\n", ratesCacheDuration.String())
	}

	return nil
}

type dbRates struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	DB		string		`bson:"db"`
	ReqSize		uint32		`bson:"req_size"`
	ReqRate		uint32		`bson:"req_rate_psec"`

	q		chan bool
}

var rates sync.Map

func getDbRates(db string) *dbRates {
	x, ok := rates.Load(db)
	if ok {
		return x.(*dbRates)
	}

	sess, err := mgo.DialWithInfo(pinfo)
	if err != nil {
		log.Printf("R: error dialing (c): %s\n", err.Error())
		return nil
	}

	defer sess.Close()

	var r dbRates
	err = sess.DB(pinfo.Database).C(colRates).Find(bson.M{"db": db}).One(&r)
	if err != nil {
		if err == mgo.ErrNotFound {
			r = dbRates{}
			goto cache
		}

		log.Printf("ERROR: cannot get rates for %s: %s\n", db, err.Error())
		return nil
	}

	if r.ReqRate != 0 {
		r.q = make(chan bool)
	}

cache:
	x, ok = rates.LoadOrStore(db, &r)
	if !ok { /* stored */
		if r.q != nil {
			go func() {
				delay := time.Second / time.Duration(r.ReqRate)
				for {
					goon := <-r.q
					if !goon {
						break
					}
					time.Sleep(delay)
				}
			}()

			time.AfterFunc(ratesCacheDuration, func() {
				rates.Delete(db)
				if r.q != nil {
					r.q <-false
				}
			})
		}
	}

	return x.(*dbRates)
}

func (*ratelimit)request(conid string, rq *mongo_req) error {
	if rq.inf == nil {
		return nil
	}

	rates := getDbRates(rq.inf.db)
	if rates == nil {
		return nil
	}

	if rates.ReqSize != 0 && uint32(rq.rlen) > rates.ReqSize {
		log.Printf("%s: R: msg-size %d exceeded for %s, stopping\n", conid, rq.rlen, rq.inf.db)
		return errors.New("quota force abort")
	}

	if rates.ReqRate != 0 {
		/* Chan blocks us until the reader is ready */
		rates.q <-true
	}

	return nil
}
