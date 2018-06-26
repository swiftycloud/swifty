package main

import (
	"time"
	"context"
	"../apis/apps"
	"../common"
	"../common/http"
)

const (
	statsFlushPeriod	= 8
)

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

type FnStatValues struct {
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
}

type FnStats struct {
//	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Cookie		string		`bson:"cookie"`
	FnStatValues			`bson:",inline"`
	Dropped		*time.Time	`bson:"dropped,omitempty"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
	onDisk		*FnStatValues	`bson:"-"`
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

type TenStatValues struct {
	Called		uint64		`bson:"called"`
	RunCost		uint64		`bson:"runcost"`
	BytesIn		uint64		`bson:"bytesin"`
	BytesOut	uint64		`bson:"bytesout"`
}

type TenStats struct {
	Tenant		string		`bson:"tenant"`
	TenStatValues			`bson:",inline"`
	Till		*time.Time	`bson:"till,omitempty"`

	statsFlush			`bson:"-"`
	onDisk		*TenStatValues	`bson:"-"`
}

func getFunctionStats(ctx context.Context, fn *FunctionDesc, periods int) ([]swyapi.FunctionStats, *swyapi.GateErr) {
	var stats []swyapi.FunctionStats

	prev, err := statsGet(ctx, fn)
	if err != nil {
		return nil, GateErrM(swy.GateGenErr, "Error getting stats")
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

func statsGet(ctx context.Context, fn *FunctionDesc) (*FnStats, error) {
	md, err := memdGetFn(ctx, fn)
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
			ctx := mkContext("::statswrite")
			fc := <-statsFlushReqs
			fc.writer.Write(ctx)
			fc.flushed <- true
		}
	}()
	return nil
}

func statsDrop(ctx context.Context, fn *FunctionDesc) error {
	md, err := memdGetFn(ctx, fn)
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
		st.onDisk = &FnStatValues{}
		*st.onDisk = st.FnStatValues
		st.Cookie = fn.Cookie
		st.statsFlush.Init(st, fn.Cookie)
	}
	return err
}

func (st *FnStats)Write(ctx context.Context) {
	var now FnStatValues = st.FnStatValues
	delta := FnStatValues {
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
		glog.Errorf("Error upserting fn stats: %s", err.Error())
	}
}

func (st *TenStats)Init(ctx context.Context, tenant string) error {
	err := dbTenStatsGet(ctx, tenant, st)
	if err == nil {
		st.onDisk = &TenStatValues{}
		*st.onDisk = st.TenStatValues
		st.Tenant = tenant
		st.statsFlush.Init(st, tenant)
	}
	return err
}

func (st *TenStats)Write(ctx context.Context) {
	var now TenStatValues = st.TenStatValues
	delta := TenStatValues {
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
		glog.Errorf("Error upserting tenant stats: %s", err.Error())
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
