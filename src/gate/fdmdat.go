package main

import (
	"sync"
	"../common/xratelimit"
)

var fdmd sync.Map

/*
 * This is a cache of data about function. It's zeroified
 * value should be valid initial one, as there's NO routine
 * that repopulates this object on gate restart.
 *
 * Or init fields with some at hands data or constants.
 */
type FnMemData struct {
	crl	*xratelimit.RL
}

func memdGetFn(fn *FunctionDesc) *FnMemData {
	return memdGetOrInit(fn.Cookie, fn)
}

func memdGet(cookie string) *FnMemData {
	return memdGetOrInit(cookie, nil)
}

func memdGetOrInit(cookie string, fn *FunctionDesc) *FnMemData {
	ret, ok := fdmd.Load(cookie)
	if ok {
		return ret.(*FnMemData)
	}

	if fn == nil {
		fx, _ := dbFuncFindByCookie(cookie)
		fn = &fx
	}
	nret := &FnMemData{}
	if fn.Size.Rate != 0 {
		nret.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
	}

	ret, _ = fdmd.LoadOrStore(fn.Cookie, nret)
	return ret.(*FnMemData)
}

func memdGone(fn *FunctionDesc) {
	fdmd.Delete(fn.Cookie)
}
