package main

import (
	"errors"
	"context"
	"gopkg.in/mgo.v2/bson"
	"../apis"
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

func urlEvFind(ctx context.Context, urlid string) (*FnEventDesc, error) {
	var ed FnEventDesc
	/* URLid is FN cookie now, but we may allocate random value for it */
	err := dbFind(ctx, bson.M{"fnid": urlid, "source": "url"}, &ed)
	if err != nil {
		return nil, err
	}
	return &ed, err
}

func urlFind(ctx context.Context, urlid string) (URL, error) {
	res, ok := urls.Load(urlid)
	if !ok {
		if urlid[0] == URLRouter[0] {
			var rt RouterDesc

			err := dbFind(ctx, bson.M{"cookie": urlid[1:]}, &rt)
			if err != nil {
				if dbNF(err) {
					err = nil
				}
				return nil, err
			}

			rurl := RouterURL{}
			id := rt.SwoId
			for _, e := range rt.Table {
				id.Name = e.Call
				re := RouterEntry{*e, id.Cookie()}
				rurl.table = append(rurl.table, &re)
			}

			res, _ = urls.LoadOrStore(urlid, &rurl)
		} else {
			ed, err := urlEvFind(ctx, urlid)
			if err != nil {
				if dbNF(err) {
					err = nil
				}
				return nil, err
			}

			fdm, err := memdGet(ctx, ed.FnId)
			if err != nil {
				return nil, err
			}

			res, _ = urls.LoadOrStore(urlid, &FnURL{ fd: fdm })
		}
	}

	return res.(URL), nil
}

/* FIXME -- set up public IP address/port for this FN */

func urlEventStart(ctx context.Context, _ *FunctionDesc, ed *FnEventDesc) error {
	return nil /* XXX -- pre-populate urls? */
}

func urlEventStop(ctx context.Context, ed *FnEventDesc) error {
	return nil
}

func urlEventClean(ctx context.Context, ed *FnEventDesc) {
	urlClean(ctx, URLFunction, ed.FnId)
}

func urlClean(ctx context.Context, typ, urlid string) {
	urls.Delete(typ + urlid)
}

var urlEOps = EventOps {
	setup:	func(ed *FnEventDesc, evt *swyapi.FunctionEvent) { /* nothing to do */ },
	start:	urlEventStart,
	stop:	urlEventStop,
	cleanup:urlEventClean,
}

func (furl *FnURL)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque) {
	furl.fd.Handle(ctx, w, r, sopq, "", "")
}

func (fmd *FnMemData)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque,
		path, key string) {
	var args *swyapi.SwdFunctionRun
	var res *swyapi.SwdFunctionRunResult
	var err error
	var code int
	var conn *podConn

	if ratelimited(fmd) {
		code = http.StatusTooManyRequests
		err = errors.New("Ratelimited")
		goto out
	}

	if rslimited(fmd) {
		code = http.StatusLocked
		err = errors.New("Resources exhausted")
		goto out
	}

	conn, err = balancerGetConnAny(ctx, fmd.fnid, fmd)
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

	res, err = doRunConn(ctx, conn, fmd.fnid, "", "call", args)
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
