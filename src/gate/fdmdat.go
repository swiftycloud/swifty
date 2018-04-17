package main

import (
	"sync"
	"time"
	"../common/xratelimit"
	"../apis/apps"
)

var fdmd sync.Map
var tdat sync.Map

type FnMemData struct {
	public	bool
	mem	uint64
	depname	string
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

func memdGetFn(fn *FunctionDesc) (*FnMemData, error) {
	return fndatGetOrInit(fn.Cookie, fn)
}

func memdGet(cookie string) (*FnMemData, error) {
	return fndatGetOrInit(cookie, nil)
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

func tendatGet(tenant string) (*TenantMemData, error) {
	return tendatGetOrInit(tenant)
}

func tendatGetOrInit(tenant string) (*TenantMemData, error) {
	ret, ok := tdat.Load(tenant)
	if ok {
		return ret.(*TenantMemData), nil
	}

	nret := &TenantMemData{}
	err := nret.stats.Init(tenant)
	if err != nil {
		return nil, err
	}

	ul, err := dbTenantGetLimits(tenant)
	if err != nil {
		return nil, err
	}

	off, err := dbTenStatsGetLatestArch(tenant)
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
				time.Sleep(SwyTenantLimitsUpdPeriod)
				ul, err := dbTenantGetLimits(tenant)
				if err != nil {
					glog.Errorf("No way to read user limits: %s", err.Error())
					continue
				}

				off, err := dbTenStatsGetLatestArch(tenant)
				if err != nil {
					glog.Errorf("No way to read user latest stats: %s", err.Error())
					continue
				}

				setupLimits(tenant, lret, ul, off)
			}
		}()
	}

	return lret, nil
}

func fndatGetOrInit(cookie string, fn *FunctionDesc) (*FnMemData, error) {
	var err error

	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData), nil
	}

	if fn == nil {
		fn, err = dbFuncFindByCookie(cookie)
		if err != nil || fn == nil {
			return nil, err
		}
	}

	nret := &FnMemData{}
	err = nret.stats.Init(fn)
	if err != nil {
		return nil, err
	}

	nret.td, err = tendatGetOrInit(fn.SwoId.Tennant)
	if err != nil {
		return nil, err
	}

	if fn.Size.Rate != 0 {
		nret.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
	}

	nret.mem = fn.Size.Mem
	nret.public = fn.Event.isURL()
	nret.depname = fn.DepName()
	if fn.AuthCtx != "" {
		nret.ac, err = authCtxGet(fn)
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
