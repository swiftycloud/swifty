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
}

func memdGetFn(fn *FunctionDesc) *FnMemData {
	return fndatGetOrInit(fn.Cookie, fn)
}

func memdGet(cookie string) *FnMemData {
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

func setupLimits(td *TenantMemData, ul *swyapi.UserLimits) {
	td.fnlim = ul.Fn.MaxInProj

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

func tendatGet(tenant string) *TenantMemData {
	return tendatGetOrInit(tenant)
}

func tendatGetOrInit(tenant string) *TenantMemData {
	ret, ok := tdat.Load(tenant)
	if ok {
		return ret.(*TenantMemData)
	}

	nret := &TenantMemData{}
	nret.stats.Init(tenant)
	ul, err := dbTenantGetLimits(tenant)
	if err != nil {
		glog.Errorf("No way to read user limits: %s", err.Error())
	}

	setupLimits(nret, ul)

	var loaded bool
	ret, loaded = tdat.LoadOrStore(tenant, nret)
	lret := ret.(*TenantMemData)

	if loaded {
		nret.stats.Stop()
	} else {
		go func() {
			for {
				time.Sleep(SwyTenantLimitsUpdPeriod)
				ul, err := dbTenantGetLimits(tenant)
				if err == nil {
					setupLimits(lret, ul)
					glog.Debugf("Updated limits for %s", tenant)
				} else {
					glog.Errorf("No way to read user limits: %s", err.Error())
				}
			}
		}()
	}

	return lret
}

func fndatGetOrInit(cookie string, fn *FunctionDesc) *FnMemData {
	var err error

	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData)
	}

	if fn == nil {
		fn, err = dbFuncFindByCookie(cookie)
		if err != nil {
			return nil
		}
	}

	nret := &FnMemData{}
	if fn.Size.Rate != 0 {
		nret.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
	}

	nret.stats.Init(fn)
	nret.mem = fn.Size.Mem
	nret.td = tendatGetOrInit(fn.SwoId.Tennant)
	nret.public = fn.URLCall
	nret.depname = fn.DepName()

	ret, _ = fdmd.LoadOrStore(fn.Cookie, nret)
	lret := ret.(*FnMemData)

	if lret != nret {
		nret.stats.Stop()
	}

	return lret
}

func memdGone(fn *FunctionDesc) {
	fdmd.Delete(fn.Cookie)
}
