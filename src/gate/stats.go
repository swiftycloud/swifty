package main

import (
	"time"
	"../apis/apps"
	"../common"
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
	BytesIn		uint64		`bson:"bytesin"`
	BytesOut	uint64		`bson:"bytesout"`

	/* RunCost is a value that represents the amount of
	 * resources spent for this function. It's used by
	 * billing to change the tennant.
	 */
	RunCost		uint64		`bson:"runcost"`
	Dropped		*time.Time	`bson:"dropped,omitempty"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
}

func (fs *FnStats)GBS() float64 {
	/* RunCost is in fn.mem * Duration, i.e. -- MB * nanoseconds */
	return float64(fs.RunCost) / float64(1024 * time.Second)
}

func (fs *TenStats)GBS() float64 {
	/* RunCost is in fn.mem * Duration, i.e. -- MB * nanoseconds */
	return float64(fs.RunCost) / float64(1024 * time.Second)
}

func (fs *TenStats)TillS() string {
	t := fs.Till
	if t == nil {
		n := time.Now()
		t = &n
	}

	return t.Format(time.RFC1123Z)
}

func (fs *FnStats)LastCallS() string {
	if fs.Called != 0 {
		return fs.LastCall.Format(time.RFC1123Z)
	} else {
		return ""
	}
}

func (fs *FnStats)TillS() string {
	t := fs.Till
	if t == nil {
		n := time.Now()
		t = &n
	}

	return t.Format(time.RFC1123Z)
}

func (fs *FnStats)RunTimeUsec() uint64 {
	return uint64(fs.RunTime/time.Microsecond)
}

type TenStats struct {
	Tenant		string		`bson:"tenant"`
	Called		uint64		`bsin:"called"`
	RunCost		uint64		`bson:"runcost"`
	BytesIn		uint64		`bson:"bytesin"`
	BytesOut	uint64		`bson:"bytesout"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
}

func getFunctionStats(fn *FunctionDesc, periods int) ([]swyapi.FunctionStats, *swyapi.GateErr) {
	var stats []swyapi.FunctionStats

	prev, err := statsGet(fn)
	if err != nil {
		return nil, GateErrM(swy.GateGenErr, "Error getting stats")
	}

	if periods > 0 {
		var afst []FnStats

		afst, err = dbFnStatsGetArch(fn.Cookie, periods)
		if err != nil {
			return nil, GateErrD(err)
		}

		for i := 0; i < periods && i < len(afst); i++ {
			cur := &afst[i]
			stats = append(stats, swyapi.FunctionStats{
				Called:		prev.Called - cur.Called,
				Timeouts:	prev.Timeouts - cur.Timeouts,
				Errors:		prev.Errors - cur.Errors,
				LastCall:	prev.LastCallS(),
				Time:		prev.RunTimeUsec() - cur.RunTimeUsec(),
				GBS:		prev.GBS() - cur.GBS(),
				BytesOut:	prev.BytesOut - cur.BytesOut,
				Till:		prev.TillS(),
				From:		cur.TillS(),
			})
			prev = cur
		}
	}

	stats = append(stats, swyapi.FunctionStats{
		Called:		prev.Called,
		Timeouts:	prev.Timeouts,
		Errors:		prev.Errors,
		LastCall:	prev.LastCallS(),
		Time:		prev.RunTimeUsec(),
		GBS:		prev.GBS(),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	return stats, nil
}

type statsOpaque struct {
	ts		time.Time
	argsSz		int
	bodySz		int
}

func statsGet(fn *FunctionDesc) (*FnStats, error) {
	md, err := memdGetFn(fn)
	if err != nil {
		return nil, err
	} else {
		return &md.stats, nil
	}
}

func statsStart() *statsOpaque {
	return &statsOpaque{ts: time.Now()}
}

func statsUpdate(fmd *FnMemData, op *statsOpaque, res *swyapi.SwdFunctionRunResult) {
	rt := res.FnTime()
	gatelat := time.Since(op.ts) - rt
	gateCalLat.Observe(gatelat.Seconds())
	gateCalls.WithLabelValues("calls").Inc()

	fmd.lock.Lock()
	fmd.bd.rover[1]++
	fmd.stats.Called++
	if res.Code != 0 {
		if res.Code == swyhttp.StatusTimeoutOccurred {
			fmd.stats.Timeouts++
			gateCalls.WithLabelValues("timeout").Inc()
		} else {
			fmd.stats.Errors++
			gateCalls.WithLabelValues("error").Inc()
		}
	}
	fmd.stats.LastCall = op.ts

	fmd.stats.RunTime += rt

	rc := uint64(rt) * fmd.mem
	fmd.stats.RunCost += rc
	fmd.stats.BytesIn += uint64(op.argsSz + op.bodySz)
	fmd.stats.BytesOut += uint64(len(res.Return))
	fmd.lock.Unlock()

	fmd.stats.Dirty()

	td := fmd.td
	td.lock.Lock()
	td.stats.RunCost += rc
	td.stats.Called++
	td.stats.BytesIn += uint64(op.argsSz + op.bodySz)
	td.stats.BytesOut += uint64(len(res.Return))
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
	md, err := memdGetFn(fn)
	if err != nil {
		return err
	}

	if !md.stats.closed {
		md.stats.Stop()
	}

	return dbFnStatsDrop(fn.Cookie, &md.stats)
}

func (st *FnStats)Init(fn *FunctionDesc) error {
	err := dbFnStatsGet(fn.Cookie, st)
	if err == nil {
		st.Cookie = fn.Cookie
		st.statsFlush.Init(st, fn.Cookie)
	}
	return err
}

func (st *FnStats)Write() {
	dbFnStatsUpdate(st)
}

func (st *TenStats)Init(tenant string) error {
	err := dbTenStatsGet(tenant, st)
	if err == nil {
		st.Tenant = tenant
		st.statsFlush.Init(st, tenant)
	}
	return err
}

func (st *TenStats)Write() {
	dbTenStatsUpdate(st)
}

func (fc *statsFlush)Init(writer statsWriter, id string) {
	fc.id = id
	fc.writer = writer
	fc.done = make(chan chan bool)
	fc.flushed = make(chan bool)
	fc.dirty = false
}

func (fc *statsFlush)Start() {
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
