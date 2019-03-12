/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"log"
	"time"
	"sync"
	"errors"
	"sync/atomic"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
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

type quota struct {}

func init() {
	addModule("quota", &quota{})
}

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

	return nil
}

func queryCheckQuota(rq *maria_req) bool {
	return true /* XXX Need to parse ... oh well */
}

func reqCheckQuota(rq *maria_req) bool {
	return true

	switch rq.cmd {
	case COM_CREATE_DB, COM_DELAYED_INSERT, COM_STMT_EXECUTE:
		return true
	case COM_QUERY:
		return queryCheckQuota(rq)
	default:
		return false
	}
}

func (*quota)request(conid string, rq *maria_req) error {
	if rq.schema == "" {
		return nil
	}

	chk := reqCheckQuota(rq)
	if chk && quotaLocked(rq.schema) {
		log.Printf("%s: Q: quota exceeded for %s, stopping\n", conid, rq.schema)
		return errors.New("quota force abort")
	}

	return nil
}

const quotaReq = `
	SELECT 
		information_schema.tables.table_schema, 
		QUOTAS.rows as rowsl, 
		SUM(information_schema.tables.table_rows) as rows, 
		QUOTAS.size as sizel, 
		SUM(information_schema.tables.data_length + information_schema.tables.index_length) as size 
	FROM information_schema.tables
	JOIN QUOTAS ON QUOTAS.id=information_schema.tables.table_schema
	GROUP BY information_schema.tables.table_schema
`
func quotaCheckDB(rq *cheqReq) {
	log.Printf("Q: Will check quota for %s (locked %v)\n", rq.db, rq.locked)

	qdb, err := sql.Open("mysql", connstr)
	if err == nil {
		err = qdb.Ping()
		defer qdb.Close()
	}
	if err != nil {
		log.Printf("Can't connect to maria (%s)", err.Error())
		return
	}

	rows, err := qdb.Query(quotaReq)
	if err != nil {
		log.Printf("Can't get quota status: %s", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var swid string
		var tqsize uint
		var tqrows uint
		var tsize uint
		var trows uint

		err = rows.Scan(&swid, &tqrows, &trows, &tqsize, &tsize)
		if err != nil {
			log.Printf("Can't scan row: %s", err.Error())
			continue
		}

		log.Printf("%s: size %d/%d rows %d/%d\n", swid, tsize, tqsize, trows, tqrows)
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
