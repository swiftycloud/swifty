package main

import (
	"github.com/gorilla/mux"
	"context"
	"strconv"
	"time"
	"net/http"
	"net/url"
	"swifty/apis"
	"swifty/common/xrest"
)

type Sysctl struct {
	Get	func() string
	Set	func(string) error
	Name	string
}

func (_ *Sysctl)Add(ctx context.Context, _ interface{}) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }
func (_ *Sysctl)Del(ctx context.Context) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }

func (ctl *Sysctl)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return map[string]string {
		"name": ctl.Name,
		"value": ctl.Get(),
	}, nil
}

func (ctl *Sysctl)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	err := ctl.Set(*upd.(*string))
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	return nil
}

var sysctls = map[string]*Sysctl {}

func addTimeSysctl(name string, d *time.Duration) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { return (*d).String() },
		Set: func(v string) error {
			nd, er := time.ParseDuration(v)
			if er != nil {
				return er
			}

			*d = nd
			return nil
		},
	}
}

func addIntSysctl(name string, i *int) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { return strconv.Itoa(*i) },
		Set: func(v string) error {
			ni, er := strconv.Atoi(v)
			if er != nil {
				return er
			}

			*i = ni
			return nil
		},
	}
}

type Sysctls struct{}

func (_ Sysctls)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	x, ok := sysctls[mux.Vars(r)["name"]]
	if !ok {
		return nil, GateErrM(swyapi.GateNotFound, "No such ctl")
	}

	return x, nil
}

func (_ Sysctls)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	for _, v := range sysctls {
		cer := cb(ctx, v)
		if cer != nil {
			return cer
		}
	}
	return nil
}

func (_ Sysctls)Create(ctx context.Context, _ interface{}) (xrest.Obj, *xrest.ReqErr) {
	return nil, GateErrC(swyapi.GateNotAvail)
}

