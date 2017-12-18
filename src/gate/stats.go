package main

import (
	"sync/atomic"
	"time"
)

const (
	statsFlushPeriod	= 8
)

type FnStats struct {
//	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Cookie		string		`bson:"cookie"`

	Called		uint64		`bson:"called"`
	LastCall	time.Time	`bson:"lastcall"`

	dirty		bool
	done		chan bool
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

func statsUpdate(fmd *FnMemData, op *statsOpaque) {
	fmd.stats.dirty = true
	atomic.AddUint64(&fmd.stats.Called, 1)
	fmd.stats.LastCall = op.ts
}

var statsFlusher chan *FnStats

func statsInit(conf *YAMLConf) error {
	statsFlusher = make(chan *FnStats)
	go func() {
		for {
			dbStatsUpdate(<-statsFlusher)
		}
	}()
	return nil
}

func statsDrop(fn *FunctionDesc) {
	md := memdGetCond(fn.Cookie)
	if md != nil {
		md.stats.done <-true
		dbStatsDrop(&md.stats)
	}
}

func fnStatsInit(st *FnStats, fn *FunctionDesc) {
	st.Cookie = fn.Cookie
	st.done = make(chan bool)
	dbStatsGet(fn.Cookie, st)
	go func() {
		for {
			select {
			case <-st.done:
				return
			case <-time.After(statsFlushPeriod * time.Second):
				if st.dirty {
					st.dirty = false
					statsFlusher <-st
				}
			}
		}
	}()
}
