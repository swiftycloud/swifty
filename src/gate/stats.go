package main

import (
	"time"
	"../apis/apps"
	"../common/http"
)

const (
	statsFlushPeriod	= 8
)

type statsWriter interface {
	Write()
}

type statsFlush struct {
	id		string /* unused now, use it for logging/debugging */
	dirty		bool
	closed		bool
	done		chan chan bool
	flushed		chan bool

	writer		statsWriter
}

type FnStats struct {
//	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Cookie		string		`bson:"cookie"`

	Called		uint64		`bson:"called"`
	Timeouts	uint64		`bson:"timeouts"`
	Errors		uint64		`bson:"errors"`
	LastCall	time.Time	`bson:"lastcall"`
	RunTime		time.Duration	`bson:"rtime"`

	/* RunCost is a value that represents the amount of
	 * resources spent for this function. It's used by
	 * billing to change the tennant.
	 */
	RunCost		uint64		`bson:"runcost"`

	statsFlush			`bson:"-"`
}

func (fs *FnStats)GBS() float64 {
	/* RunCost is in fn.mem * Duration, i.e. -- MB * nanoseconds */
	return float64(fs.RunCost) / float64(1024 * time.Second)
}

type TenStats struct {
	Tenant		string		`bson:"tenant"`
	Called		uint64		`bsin:"called"`
	RunCost		uint64		`bson:"runcost"`

	statsFlush			`bson:"-"`
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
	rt := res.FnTime()
	gatelat := time.Since(op.ts) - rt
	gateCalLat.Observe(gatelat.Seconds())

	fmd.lock.Lock()
	if res.Code == 0 {
		fmd.stats.Called++
		gateCalls.WithLabelValues("success").Inc()
	} else if res.Code == swyhttp.StatusTimeoutOccurred {
		fmd.stats.Timeouts++
		gateCalls.WithLabelValues("timeout").Inc()
	} else {
		fmd.stats.Errors++
		gateCalls.WithLabelValues("error").Inc()
	}
	fmd.stats.LastCall = op.ts

	fmd.stats.RunTime += rt

	rc := uint64(rt) * fmd.mem
	fmd.stats.RunCost += rc
	fmd.lock.Unlock()

	fmd.stats.Dirty()

	td := fmd.td
	td.lock.Lock()
	td.stats.RunCost += rc
	td.stats.Called++
	td.lock.Unlock()

	td.stats.Dirty()
}

var statsFlushReqs chan *statsFlush

func statsInit(conf *YAMLConf) error {
	statsFlushReqs = make(chan *statsFlush)
	go func() {
		for {
			fc := <-statsFlushReqs
			fc.writer.Write()
			fc.flushed <- true
		}
	}()
	return nil
}

func statsDrop(fn *FunctionDesc) error {
	md := memdGetCond(fn.Cookie)
	if md != nil && !md.stats.closed {
		md.stats.Stop()
	}

	return dbFnStatsDrop(fn.Cookie)
}

func (st *FnStats)Init(fn *FunctionDesc) {
	dbFnStatsGet(fn.Cookie, st)
	st.Cookie = fn.Cookie
	st.Start(st, fn.Cookie)
}

func (st *FnStats)Write() {
	dbFnStatsUpdate(st)
}

func (st *TenStats)Init(tenant string) {
	dbTenStatsGet(tenant, st)
	st.Tenant = tenant
	st.Start(st, tenant)
}

func (st *TenStats)Write() {
	dbTenStatsUpdate(st)
}

func (fc *statsFlush)Start(writer statsWriter, id string) {
	fc.id = id
	fc.writer = writer
	fc.done = make(chan chan bool)
	fc.flushed = make(chan bool)
	fc.dirty = false

	go func() {
		for {
			select {
			case done := <-fc.done:
				fc.closed = true
				close(done)
				return
			case <-time.After(statsFlushPeriod * time.Second):
				if fc.dirty {
					fc.dirty = false
					statsFlushReqs <-fc
					<-fc.flushed
				}
			}
		}
	}()
}

func (fc *statsFlush)Dirty() {
	fc.dirty = true
}

func (fc *statsFlush)Stop() {
	done := make(chan bool)
	fc.done <-done
	<-done
}
