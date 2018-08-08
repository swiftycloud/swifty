package main

import (
	"sync"
	"time"
	"context"
	"../common/xratelimit"
	"../apis/apps"
	"gopkg.in/mgo.v2/bson"
)

var fdmd sync.Map
var tdat sync.Map

type FnMemData struct {
	public	bool
	mem	uint64
	depname	string
	fnid	string
	ac	*AuthCtx
	bd	BalancerDat
	crl	*xratelimit.RL
	td	*TenantMemData
	stats	FnStats
	lock	sync.Mutex
}

type TenantMemData struct {
	crl	*xratelimit.RL
	stats	TenStats
	fnlim	uint
	lock	sync.Mutex

	GBS_l, GBS_o	float64
	BOut_l, BOut_o	uint64
}

func memdGetFnForRemoval(ctx context.Context, fn *FunctionDesc) (*FnMemData, error) {
	return fndatGetOrInit(ctx, fn.Cookie, fn, true)
}

func memdGetFn(ctx context.Context, fn *FunctionDesc) (*FnMemData, error) {
	return fndatGetOrInit(ctx, fn.Cookie, fn, false)
}

func memdGet(ctx context.Context, cookie string) (*FnMemData, error) {
	return fndatGetOrInit(ctx, cookie, nil, false)
}

func memdGetCond(cookie string) *FnMemData {
	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData)
	} else {
		return nil
	}
}

func setupLimits(ten string, td *TenantMemData, ul *swyapi.UserLimits, off *TenStats) {
	if ul.Fn != nil {
		td.fnlim = ul.Fn.MaxInProj

		/*
		 * Some explanation about limiting. The GBS(RunCost) and BytesOut are
		 * monotonic counters, that constantly gorw. At the same time, limit
		 * should be per-someperiod. The pariod is the same as the snapshot
		 * one for stats, so to get the idea of what to limit, we get the latest
		 * available stats snapshot and make current stat go over these values
		 * not much than the configured limit.
		 *
		 * Bad design? Maybe, but what are other options?
		 */
		td.GBS_l = ul.Fn.GBS
		td.GBS_o = off.GBS()
		td.BOut_l = ul.Fn.BytesOut
		td.BOut_o = off.BytesOut
	}

	if ul.Fn == nil || ul.Fn.Rate == 0 {
		td.crl = nil
	} else {
		if td.crl == nil {
			td.crl = xratelimit.MakeRL(ul.Fn.Burst, ul.Fn.Rate)
		} else {
			td.crl.Update(ul.Fn.Burst, ul.Fn.Rate)
		}
	}
}

func tendatGet(ctx context.Context, tenant string) (*TenantMemData, error) {
	return tendatGetOrInit(ctx, tenant)
}

func tendatGetOrInit(ctx context.Context, tenant string) (*TenantMemData, error) {
	ret, ok := tdat.Load(tenant)
	if ok {
		return ret.(*TenantMemData), nil
	}

	nret := &TenantMemData{}
	err := nret.stats.Init(ctx, tenant)
	if err != nil {
		return nil, err
	}

	ul, err := dbTenantGetLimits(ctx, tenant)
	if err != nil {
		return nil, err
	}

	off, err := dbTenStatsGetLatestArch(ctx, tenant)
	if err != nil {
		return nil, err
	}

	setupLimits(tenant, nret, ul, off)

	var loaded bool
	ret, loaded = tdat.LoadOrStore(tenant, nret)
	lret := ret.(*TenantMemData)

	if !loaded {
		lret.stats.Start()
		go func() {
			for {
				cctx, done := mkContext("::tenlimupd")

				time.Sleep(TenantLimitsUpdPeriod)
				ul, err := dbTenantGetLimits(cctx, tenant)
				if err != nil {
					ctxlog(cctx).Errorf("No way to read user limits: %s", err.Error())
					done(cctx)
					continue
				}

				off, err := dbTenStatsGetLatestArch(cctx, tenant)
				if err != nil {
					ctxlog(cctx).Errorf("No way to read user latest stats: %s", err.Error())
					done(cctx)
					continue
				}

				setupLimits(tenant, lret, ul, off)
				done(cctx)
			}
		}()
	}

	return lret, nil
}

func fndatGetOrInit(ctx context.Context, cookie string, fn *FunctionDesc, forRemoval bool) (*FnMemData, error) {
	var err error

	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData), nil
	}

	if fn == nil {
		var fnd FunctionDesc

		err = dbFind(ctx, bson.M{"cookie": cookie}, &fnd)
		if err != nil {
			if dbNF(err) {
				err = nil /* XXX -- why? */
			}
			return nil, err
		}

		fn = &fnd
	}

	nret := &FnMemData{}
	err = nret.stats.Init(ctx, fn)
	if err != nil {
		return nil, err
	}

	nret.td, err = tendatGetOrInit(ctx, fn.SwoId.Tennant)
	if err != nil {
		return nil, err
	}

	if fn.Size.Rate != 0 {
		nret.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
	}

	nret.mem = fn.Size.Mem
	nret.public = fn.isURL()
	nret.depname = fn.DepName()
	nret.fnid = fn.Cookie

	if fn.AuthCtx != "" && !forRemoval {
		nret.ac, err = authCtxGet(ctx, fn.SwoId, fn.AuthCtx)
		if err != nil {
			return nil, err
		}
	}

	var loaded bool
	ret, loaded = fdmd.LoadOrStore(fn.Cookie, nret)
	lret := ret.(*FnMemData)

	if !loaded {
		lret.stats.Start()
	}

	return lret, nil
}

func memdGone(fn *FunctionDesc) {
	fdmd.Delete(fn.Cookie)
}
