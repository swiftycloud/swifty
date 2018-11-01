package main

import (
	"time"
	"context"
	"swifty/gate/mgo"
	"swifty/apis"
	"swifty/common/http"
	"swifty/common/xrest"
)

var statsFlushPeriod = 8 * time.Second

func init() {
	addTimeSysctl("stats_fush_period", &statsFlushPeriod)
}

type statsWriter interface {
	Write(ctx context.Context)
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
	gmgo.FnStatValues		`bson:",inline"`
	Dropped		*time.Time	`bson:"dropped,omitempty"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
	onDisk		*gmgo.FnStatValues	`bson:"-"`
}

func GBS(runcost uint64) float64 {
	/* RunCost is in fn.mem * Duration, i.e. -- MB * nanoseconds */
	return float64(runcost) / float64(1024 * time.Second)
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

func getCallStats(ctx context.Context, periods int) ([]swyapi.TenantStatsFn, *xrest.ReqErr) {
	var cs []swyapi.TenantStatsFn

	ten := gctx(ctx).Tenant

	td, err := tendatGet(ctx, ten)
	if err != nil {
		return nil, GateErrD(err)
	}

	prev := &td.stats

	if periods > 0 {
		atst, err := dbTenStatsGetArch(ctx, ten, periods)
		if err != nil {
			return nil, GateErrD(err)
		}

		for i := 0; i < periods && i < len(atst); i++ {
			cur := &atst[i]
			cs = append(cs, swyapi.TenantStatsFn{
				Called:		prev.Called - cur.Called,
				GBS:		GBS(prev.RunCost - cur.RunCost),
				BytesOut:	prev.BytesOut - cur.BytesOut,
				Till:		prev.TillS(),
				From:		cur.TillS(),
			})
			prev = cur
		}
	}

	cs = append(cs, swyapi.TenantStatsFn{
		Called:		prev.Called,
		GBS:		GBS(prev.RunCost),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	return cs, nil
}

type TenStats struct {
	Tenant		string		`bson:"tenant"`
	gmgo.TenStatValues			`bson:",inline"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
	onDisk		*gmgo.TenStatValues	`bson:"-"`
}

func (fn *FunctionDesc)getStats(ctx context.Context, periods int) ([]swyapi.FunctionStats, *xrest.ReqErr) {
	var stats []swyapi.FunctionStats

	prev, err := statsGet(ctx, fn)
	if err != nil {
		return nil, GateErrM(swyapi.GateGenErr, "Error getting stats")
	}

	if periods > 0 {
		var afst []FnStats

		afst, err = dbFnStatsGetArch(ctx, fn.Cookie, periods)
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
				GBS:		GBS(prev.RunCost - cur.RunCost),
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
		GBS:		GBS(prev.RunCost),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	return stats, nil
}

type statsOpaque struct {
	ts		time.Time
	argsSz		int
	bodySz		int
	trace		map[string]time.Duration
}

func statsGet(ctx context.Context, fn *FunctionDesc) (*FnStats, error) {
	md, err := memdGetFn(ctx, fn)
	if err != nil {
		return nil, err
	} else {
		return &md.stats, nil
	}
}

func statsStart() *statsOpaque {
	sopq := &statsOpaque{ts: time.Now()}
	if traceEnabled() {
		sopq.trace = make(map[string]time.Duration)
		sopq.trace["start"] = time.Duration(0)
	}
	return sopq
}

func statsUpdate(fmd *FnMemData, op *statsOpaque, res *swyapi.WdogFunctionRunResult, event string) {
	lat := time.Since(op.ts)
	if op.trace != nil {
		op.trace["stop"] = lat
		traceCall(fmd.fnid, op.trace)
	}

	rt := res.FnTime()
	gatelat := lat - rt
	gateCalLat.Observe(gatelat.Seconds())
	gateCalls.WithLabelValues(event).Inc()

	fmd.lock.Lock()
	fmd.stats.Called++
	if res.Code != 0 {
		if res.Code == xhttp.StatusTimeoutOccurred {
			fmd.stats.Timeouts++
			gateCalls.WithLabelValues("timeout").Inc()
		} else {
			fmd.stats.Errors++
			gateCalls.WithLabelValues("error").Inc()
		}
	}
	fmd.stats.LastCall = op.ts

	fmd.stats.RunTime += rt

	rc := uint64(rt) * uint64(fmd.mem)
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

func statsInit() error {
	statsFlushReqs = make(chan *statsFlush)
	go func() {
		for {
			fc := <-statsFlushReqs
			ctx, done := mkContext("::statswrite")
			fc.writer.Write(ctx)
			done(ctx)
			fc.flushed <- true
		}
	}()
	return nil
}

func statsDrop(ctx context.Context, fn *FunctionDesc) error {
	md, err := memdGetFnForRemoval(ctx, fn)
	if err != nil {
		return err
	}

	if !md.stats.closed {
		md.stats.Stop()
	}

	return dbFnStatsDrop(ctx, fn.Cookie, &md.stats)
}

func (st *FnStats)Init(ctx context.Context, fn *FunctionDesc) error {
	err := dbFnStatsGet(ctx, fn.Cookie, st)
	if err == nil {
		st.onDisk = &gmgo.FnStatValues{}
		*st.onDisk = st.FnStatValues
		st.Cookie = fn.Cookie
		st.statsFlush.Init(st, fn.Cookie)
	}
	return err
}

func (st *FnStats)Write(ctx context.Context) {
	var now gmgo.FnStatValues = st.FnStatValues
	delta := gmgo.FnStatValues {
		Called: now.Called - st.onDisk.Called,
		Timeouts: now.Timeouts - st.onDisk.Timeouts,
		Errors: now.Errors - st.onDisk.Errors,
		RunTime: now.RunTime - st.onDisk.RunTime,
		BytesIn: now.BytesIn - st.onDisk.BytesIn,
		BytesOut: now.BytesOut - st.onDisk.BytesOut,
		RunCost: now.RunCost - st.onDisk.RunCost,
	}
	err := dbFnStatsUpdate(ctx, st.Cookie, &delta, now.LastCall)
	if err == nil {
		st.onDisk = &now
	} else {
		ctxlog(ctx).Errorf("Error upserting fn stats: %s", err.Error())
	}
}

func (st *TenStats)Init(ctx context.Context, tenant string) error {
	err := dbTenStatsGet(ctx, tenant, st)
	if err == nil {
		st.onDisk = &gmgo.TenStatValues{}
		*st.onDisk = st.TenStatValues
		st.Tenant = tenant
		st.statsFlush.Init(st, tenant)
	}
	return err
}

func (st *TenStats)Write(ctx context.Context) {
	var now gmgo.TenStatValues = st.TenStatValues
	delta := gmgo.TenStatValues {
		Called: now.Called - st.onDisk.Called,
		RunCost: now.RunCost - st.onDisk.RunCost,
		BytesIn: now.BytesIn - st.onDisk.BytesIn,
		BytesOut: now.BytesOut - st.onDisk.BytesOut,
	}
	err := dbTenStatsUpdate(ctx, st.Tenant, &delta)
	if err == nil {
		st.onDisk = &now
	} else {
		/* Next time we'll get here with largetr deltas */
		ctxlog(ctx).Errorf("Error upserting tenant stats: %s", err.Error())
	}
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
			case <-time.After(statsFlushPeriod):
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
