package main

import (
	"sync"
	"../common/xratelimit"
)

var fdmd sync.Map

type FnMemData struct {
	crl	*xratelimit.RL
	stats	FnStats
}

func memdGetFn(fn *FunctionDesc) *FnMemData {
	return memdGetOrInit(fn.Cookie, fn)
}

func memdGet(cookie string) *FnMemData {
	return memdGetOrInit(cookie, nil)
}

func memdGetCond(cookie string) *FnMemData {
	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData)
	} else {
		return nil
	}
}

func memdGetOrInit(cookie string, fn *FunctionDesc) *FnMemData {
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

	fnStatsInit(&nret.stats, fn)

	ret, _ = fdmd.LoadOrStore(fn.Cookie, nret)
	return ret.(*FnMemData)
}

func memdGone(fn *FunctionDesc) {
	fdmd.Delete(fn.Cookie)
}
