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

func setupLimits(td *TenantMemData, ul *swyapi.UserLimits) {
	if ul.Fn != nil {
		td.fnlim = ul.Fn.MaxInProj
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

	setupLimits(nret, ul)

	var loaded bool
	ret, loaded = tdat.LoadOrStore(tenant, nret)
	lret := ret.(*TenantMemData)

	if !loaded {
		lret.stats.Start()
		go func() {
			for {
				time.Sleep(SwyTenantLimitsUpdPeriod)
				ul, err := dbTenantGetLimits(tenant)
				if err == nil {
					setupLimits(lret, ul)
				} else {
					glog.Errorf("No way to read user limits: %s", err.Error())
				}
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
	nret.public = fn.URLCall
	nret.depname = fn.DepName()

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
