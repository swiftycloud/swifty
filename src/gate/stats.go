package main

import (
	"sync/atomic"
	"time"
	"../apis/apps"
)

const (
	statsFlushPeriod	= 8
)

type FnStats struct {
//	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Cookie		string		`bson:"cookie"`

	Called		uint64		`bson:"called"`
	Timeouts	uint64		`bson:"timeouts"`
	Errors		uint64		`bson:"errors"`
	LastCall	time.Time	`bson:"lastcall"`
	RunTime		time.Duration	`bson:"rtime"`
	WdogTime	time.Duration	`bson:"wtime"`
	GateTime	time.Duration	`bson:"gtime"`

	/* RunCost is a value that represents the amount of
	 * resources spent for this function. It's used by
	 * billing to change the tennant.
	 */
	RunCost		uint64		`bson:"runcost"`

	dirty		bool
	done		chan chan bool
	flushed		chan bool
}

type statsOpaque struct {
	ts		time.Time
}

func statsGet(fn *FunctionDesc) *FnStats {
	md := memdGetFn(fn)
	return &md.stats
}

func statsStart() *statsOpaque {
	return &statsOpaque{ts: time.Now()}
}

func statsUpdate(fmd *FnMemData, op *statsOpaque, res *swyapi.SwdFunctionRunResult) {
	fmd.stats.dirty = true
	if res.Code == 0 {
		atomic.AddUint64(&fmd.stats.Called, 1)
	} else if res.Code == 524 {
		atomic.AddUint64(&fmd.stats.Timeouts, 1)
	} else {
		atomic.AddUint64(&fmd.stats.Errors, 1)
	}
	fmd.stats.LastCall = op.ts

	rt := time.Duration(res.Time) * time.Microsecond
	fmd.stats.RunTime += rt
	fmd.stats.WdogTime += time.Duration(res.CTime) * time.Microsecond
	fmd.stats.GateTime += time.Since(op.ts)

	fmd.stats.RunCost += uint64(rt) * fmd.mem
}

var statsFlusher chan *FnStats

func statsInit(conf *YAMLConf) error {
	statsFlusher = make(chan *FnStats)
	go func() {
		for {
			st := <-statsFlusher
			dbStatsUpdate(st)
			st.flushed <- true
		}
	}()
	return nil
}

func statsStop(st *FnStats) {
	done := make(chan bool)
	st.done <-done
	<-done
}

func statsDrop(fn *FunctionDesc) error {
	md := memdGetCond(fn.Cookie)
	if md != nil {
		statsStop(&md.stats)
	}

	return dbStatsDrop(fn.Cookie)
}

func fnStatsInit(st *FnStats, fn *FunctionDesc) {
	st.Cookie = fn.Cookie
	st.done = make(chan chan bool)
	st.flushed = make(chan bool)
	dbStatsGet(fn.Cookie, st)
	go func() {
		for {
			select {
			case done := <-st.done:
				done <- true
				return
			case <-time.After(statsFlushPeriod * time.Second):
				if st.dirty {
					st.dirty = false
					statsFlusher <-st
					<-st.flushed
				}
			}
		}
	}()
}
