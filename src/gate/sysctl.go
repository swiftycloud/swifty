package main

import (
	"code.cloudfoundry.org/bytefmt"
	"github.com/gorilla/mux"
	"context"
	"errors"
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

func addRoSysctl(name string, read func() string) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get:  read,
		Set: func(v string) error { return errors.New("R/O sysctl") },
	}
}

func addBoolSysctl(name string, b *bool) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { if *b { return "true" } else { return "false" } },
		Set: func(v string) error {
			switch v {
			case "1", "true", "yes", "on":
				*b = true
			case "0", "false", "no", "off":
				*b = false
			default:
				return errors.New("Invalid value")
			}
			return nil
		},
	}
}

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
			if ni < 0 {
				return errors.New("Negative value not allowed")
			}

			*i = ni
			return nil
		},
	}
}

func addStringSysctl(name string, s *string) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { return *s },
		Set: func(v string) error {
			*s = v
			return nil
		},
	}
}

func addMemSysctl(name string, mem *uint64) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string {
			return bytefmt.ByteSize(*mem)
		},
		Set: func(nv string) error {
			nmem, err := bytefmt.ToBytes(nv)
			if err != nil {
				return err
			}

			*mem = nmem
			return nil
		},
	}
}

func addSysctl(name string, get func() string, set func(string) error) {
	sysctls[name] = &Sysctl{ Name: name, Get: get, Set: set }
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

