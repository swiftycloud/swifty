/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package sysctl

import (
	"code.cloudfoundry.org/bytefmt"
	"github.com/gorilla/mux"
	"context"
	"errors"
	"strconv"
	"time"
	"net/http"
	"net/url"
	"swifty/common/xrest"
)

type Sysctl struct {
	Get	func() string
	Set	func(string) error
	Name	string
}

func (_ *Sysctl)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return &xrest.ReqErr{xrest.BadRequest, ""}
}

func (_ *Sysctl)Del(ctx context.Context) *xrest.ReqErr {
	return &xrest.ReqErr{xrest.BadRequest, ""}
}

func (ctl *Sysctl)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return map[string]string {
		"name": ctl.Name,
		"value": ctl.Get(),
	}, nil
}

func (ctl *Sysctl)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	err := ctl.Set(*upd.(*string))
	if err != nil {
		return &xrest.ReqErr{xrest.BadRequest, err.Error()}
	}

	return nil
}

var sysctls = map[string]*Sysctl {}

func AddRoSysctl(name string, read func() string) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get:  read,
		Set: func(v string) error { return errors.New("R/O sysctl") },
	}
}

func AddBoolSysctl(name string, b *bool) {
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

func AddTimeSysctl(name string, d *time.Duration) {
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

func AddInt64Sysctl(name string, i *int64) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { return strconv.FormatInt(*i, 10) },
		Set: func(v string) error {
			ni, er := strconv.ParseInt(v, 10, 64)
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

func AddIntSysctl(name string, i *int) {
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

func AddStringSysctl(name string, s *string) {
	sysctls[name] = &Sysctl{
		Name: name,
		Get: func() string { return *s },
		Set: func(v string) error {
			*s = v
			return nil
		},
	}
}

func AddMemSysctl(name string, mem *uint64) {
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

func AddSysctl(name string, get func() string, set func(string) error) {
	sysctls[name] = &Sysctl{ Name: name, Get: get, Set: set }
}

type Sysctls struct{}

func (_ Sysctls)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	x, ok := sysctls[mux.Vars(r)["name"]]
	if !ok {
		return nil, &xrest.ReqErr{xrest.BadRequest, "No such ctl"}
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
	return nil, &xrest.ReqErr{xrest.BadRequest, ""}
}


