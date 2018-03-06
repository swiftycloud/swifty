package main

import (
	"sync"
	"../common/xratelimit"
)

var fdmd sync.Map
var tdat sync.Map

type FnMemData struct {
	mem	uint64
	crl	*xratelimit.RL
	td	*TenantMemData
	stats	FnStats
}

type TenantMemData struct {
	stats	TenStats
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

func tendatGetOrInit(tenant string) *TenantMemData {
	ret, ok := tdat.Load(tenant)
	if ok {
		return ret.(*TenantMemData)
	}

	nret := &TenantMemData{}
	nret.stats.Init(tenant)
	ret, _ = tdat.LoadOrStore(tenant, nret)
	lret := ret.(*TenantMemData)

	if lret != nret {
		nret.stats.Stop()
	}

	return lret
}

func fndatGetOrInit(cookie string, fn *FunctionDesc) *FnMemData {
	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData)
	}

	if fn == nil {
		fn, _ = dbFuncFindByCookie(cookie)
	}

	nret := &FnMemData{}
	if fn.Size.Rate != 0 {
		nret.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
	}

	nret.stats.Init(fn)
	nret.mem = fn.Size.Mem
	nret.td = tendatGetOrInit(fn.SwoId.Tennant)

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
