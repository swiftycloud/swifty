package main

import (
	"errors"
	"context"
	"gopkg.in/mgo.v2/bson"
	"swifty/apis"
	"sync"
	"net/http"
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
	setup:	func(ed *FnEventDesc, evt *swyapi.FunctionEvent) error { return nil },
	start:	urlEventStart,
	stop:	urlEventStop,
}

func (furl *FnURL)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque) {
	furl.fd.Handle(ctx, w, r, sopq, "", "")
}

func (fmd *FnMemData)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque,
		path, key string) {
	var args *swyapi.FunctionRun
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
	args = makeArgs(sopq, r, path, key)

	if fmd.ac != nil {
		args.Claims, err = fmd.ac.Verify(r)
		if err != nil {
			code = http.StatusUnauthorized
			goto out
		}
	}

	res, err = conn.Run(ctx, sopq, "", "call", args)
	if err != nil {
		code = http.StatusInternalServerError
		goto out
	}

	if res.Code < 0 {
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

	statsUpdate(fmd, sopq, res)

	return

out:
	http.Error(w, err.Error(), code)
}
