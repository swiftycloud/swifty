package main

import (
	"errors"
	"context"
	"gopkg.in/mgo.v2/bson"
	"swifty/apis"
	"sync"
	"net/http"
	"swifty/common/xratelimit"
	"strconv"
	"strings"
)

type URL interface {
	Handle(context.Context, http.ResponseWriter, *http.Request, *statsOpaque)
}

var urls sync.Map

type FnURL struct {
	URL
	fd	*FnMemData
}

const (
	URLAuto		= "auto"
	URLRouter	= "r"
	URLFunction	= ""
)

func getURL(typ, urlid string) string {
	cg := conf.Daemon.CallGate
	if cg == "" {
		cg = conf.Daemon.Addr
	}
	return cg + "/call/" + typ + urlid
}

func urlKey(fnid string) string { return "url:" + fnid }

func urlEvFind(ctx context.Context, urlid string) (*FnEventDesc, error) {
	var ed FnEventDesc
	/* URLid is FN cookie now, but we may allocate random value for it */
	err := dbFind(ctx, bson.M{"key": urlKey(urlid) }, &ed)
	if err != nil {
		return nil, err
	}
	return &ed, err
}

func makeFnURL(ctx context.Context, urlid string) (*FnURL, error) {
	ed, err := urlEvFind(ctx, urlid)
	if err != nil {
		return nil, err
	}

	fdm, err := memdGet(ctx, ed.FnId)
	if err != nil {
		return nil, err
	}

	return &FnURL{fd: fdm}, nil
}

func urlCreate(ctx context.Context, urlid string) (URL, error) {
	if urlid[0] == URLRouter[0] {
		return makeRouterURL(ctx, urlid[1:])
	} else {
		return makeFnURL(ctx, urlid)
	}
}

func urlFind(ctx context.Context, urlid string) (URL, error) {
	res, ok := urls.Load(urlid)
	if !ok {
		url, err := urlCreate(ctx, urlid)
		if err != nil {
			return nil, err
		}

		res, _ = urls.LoadOrStore(urlid, url)

	}

	return res.(URL), nil
}

/* XXX -- set up public IP address/port for this FN */

func urlEventStart(ctx context.Context, fn *FunctionDesc, ed *FnEventDesc) error {
	ed.Key = urlKey(fn.Cookie)
	return nil /* XXX -- pre-populate urls? */
}

func urlEventStop(ctx context.Context, ed *FnEventDesc) error {
	urlClean(ctx, URLFunction, ed.FnId)
	return nil
}

func urlClean(ctx context.Context, typ, urlid string) {
	urls.Delete(typ + urlid)
}

var urlEOps = EventOps {
	setup:	func(ed *FnEventDesc, evt *swyapi.FunctionEvent) error {
		/* XXX -- empty is for backward compat */
		if evt.URL != "" && evt.URL != URLAuto {
			return errors.New("Invalid \"url\" parameter")
		}

		return nil
	},
	start:	urlEventStart,
	stop:	urlEventStop,
}

func (furl *FnURL)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque) {
	path := reqPath(r)
	args := &swyapi.FunctionRun{Path: &path}
	furl.fd.Handle(ctx, w, r, sopq, args)
}

var wrl *xratelimit.RL

func init() {
	wrl = xratelimit.MakeRL(5, 1)
	addSysctl("fn_call_error_rate",
			func() string {
				vs := wrl.If()
				return strconv.Itoa(int(vs[2])) + ":" + strconv.Itoa(int(vs[1]))
			},
			func(nv string) error {
				vs := strings.Split(nv, ":")
				if len(vs) != 2 {
					return errors.New("Invalid value, use \"burst\":\"rate\"")
				}

				nb, err := strconv.Atoi(vs[0])
				if err != nil || nb < 0 {
					return errors.New("Bad burst")
				}

				nr, err := strconv.Atoi(vs[1])
				if err != nil || nr <= 0 {
					return errors.New("Bad rate")
				}

				wrl.Update(uint(nb), uint(nr))
				return nil
			})
}

func (fmd *FnMemData)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque,
		args *swyapi.FunctionRun) {
	var res *swyapi.WdogFunctionRunResult
	var err error
	var code int
	var conn *podConn

	if fmd.ratelimited() {
		code = http.StatusTooManyRequests
		err = errors.New("Ratelimited")
		goto out
	}

	if fmd.rslimited() {
		code = http.StatusLocked
		err = errors.New("Resources exhausted")
		goto out
	}

	conn, err = balancerGetConnAny(ctx, fmd)
	if err != nil {
		code = http.StatusInternalServerError
		err = errors.New("DB error")
		goto out
	}

	defer balancerPutConn(fmd)

	if args.Claims == nil && fmd.ac != nil {
		args.Claims, err = fmd.ac.Verify(r)
		if err != nil {
			code = http.StatusUnauthorized
			goto out
		}
	}

	makeArgs(args, sopq, r)
	res, err = conn.Run(ctx, sopq, "", "call", args)
	if err != nil {
		code = http.StatusInternalServerError
		gateCallErrs.WithLabelValues("fail").Inc()
		goto out
	}

	statsUpdate(fmd, sopq, res, "url")

	if res.Code < 0 {
		if wrl.Get() {
			ctxlog(ctx).Warnf("Function call falied: %d/%s", res.Code, res.Return)
		}
		code = -res.Code
		err = errors.New(res.Return)
		goto out
	}

	if res.Code == 0 {
		res.Code = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.Code)
	w.Write([]byte(res.Return))

	return

out:
	http.Error(w, err.Error(), code)
}
