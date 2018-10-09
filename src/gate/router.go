package main

import (
	"net/http"
	"net/url"
	"strings"
	"context"
	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/xrest"
	"gopkg.in/mgo.v2/bson"
)

type RouterDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Cookie		string			`bson:"cookie"`
	Labels		[]string		`bson:"labels,omitempty"`
	Table		[]*swyapi.RouterEntry	`bson:"table"`
}

type Routers struct {}

const TableKeyLenMax = 64

func ckTable(tbl []*swyapi.RouterEntry) *xrest.ReqErr {
	for _, t := range tbl {
		if len(t.Key) > TableKeyLenMax {
			return GateErrM(swyapi.GateBadRequest, "Too long key")
		}
	}

	return nil
}

func getRouterDesc(id *SwoId, params *swyapi.RouterAdd) (*RouterDesc, *xrest.ReqErr) {
	if !id.NameOK() {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad function name")
	}

	cerr := ckTable(params.Table)
	if cerr != nil {
		return nil, cerr
	}

	rd := RouterDesc {
		SwoId:	*id,
		Table:	params.Table,
	}

	return &rd, nil
}

func (rt *RouterDesc)getURL() string {
	return getURL(URLRouter, rt.Cookie)
}

func makeRouterURL(ctx context.Context, urlid string) (*RouterURL, error) {
	var rt RouterDesc

	err := dbFind(ctx, bson.M{"cookie": urlid}, &rt)
	if err != nil {
		if dbNF(err) {
			err = nil
		}
		return nil, err
	}

	rurl := RouterURL{}
	rurl.table = make(map[string]*RouterEntry)
	id := rt.SwoId
	for _, e := range rt.Table {
		id.Name = e.Call
		re := RouterEntry{}
		re.cookie = id.Cookie()
		re.key = e.Key
		if e.Method == "*" {
			re.methods.Fill()
		} else {
			for _, m := range strings.Fields(e.Method) {
				re.methods.Set(methodNr(m))
			}
		}
		rurl.table[e.Path] = &re
	}

	return &rurl, nil
}

func (_ Routers)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var rt RouterDesc

	/* FIXME -- omit table here */
	cerr := objFindForReq(ctx, r, "rid", &rt)
	if cerr != nil {
		return nil, cerr
	}

	return &rt, nil
}

func (_ Routers)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}
	rname := q.Get("name")

	var rt RouterDesc

	if rname != "" {
		err := dbFind(ctx, cookieReq(ctx, project, rname), &rt)
		if err != nil {
			return GateErrD(err)
		}

		return cb(ctx, &rt)
	}

	iter := dbIterAll(ctx, listReq(ctx, project, q["label"]), &rt)
	defer iter.Close()

	for iter.Next(&rt) {
		cerr := cb(ctx, &rt)
		if cerr != nil {
			return cerr
		}
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (_ Routers)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.RouterAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getRouterDesc(id, params)
}

func (rt *RouterDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return rt.toInfo(ctx, details), nil
}

func (rt *RouterDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (rt *RouterDesc)toInfo(ctx context.Context, details bool) *swyapi.RouterInfo {
	ri := swyapi.RouterInfo {
		Id:		rt.ObjID.Hex(),
		Name:		rt.SwoId.Name,
		Project:	rt.SwoId.Project,
		Labels:		rt.Labels,
		TLen:		len(rt.Table),
	}

	ri.URL = rt.getURL()

	return &ri
}

func (rd *RouterDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	rd.ObjID = bson.NewObjectId()
	rd.Cookie = rd.SwoId.Cookie()
	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func routerStopId(ctx context.Context, id *SwoId) *xrest.ReqErr {
	var rt RouterDesc

	err := dbFind(ctx, id.dbReq(), &rt)
	if err != nil {
		return GateErrD(err)
	}

	return rt.Del(ctx)
}

func (rd *RouterDesc)Del(ctx context.Context) *xrest.ReqErr {
	err := dbRemove(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	urlClean(ctx, URLRouter, rd.Cookie)
	return nil
}

func (rd *RouterDesc)setTable(ctx context.Context, tbl []*swyapi.RouterEntry) *xrest.ReqErr {
	cerr := ckTable(tbl)
	if cerr != nil {
		return cerr
	}

	err := dbUpdatePart(ctx, rd, bson.M{"table": tbl})
	if err != nil {
		return GateErrD(err)
	}

	/*
	 * XXX: Maybe it's worth fixing the table on RouterURL, but
	 * flushing the cache and making urlFind repopulate one from
	 * DB seems like acceptable approach, at least for now.
	 */
	urlClean(ctx, URLRouter, rd.Cookie)
	return nil
}

type RouterEntry struct {
	cookie	string
	methods	xh.Bitmask
	key	string
}

type RouterURL struct {
	URL
	table	map[string]*RouterEntry
}

func (rt *RouterURL)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque) {
	path := reqPath(r)
	e, ok := rt.table[path]
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	mnr := methodNr(r.Method)
	if !e.methods.Test(mnr) {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	/* FIXME -- cache guy on e */
	fmd, err := memdGet(ctx, e.cookie)
	if err != nil {
		http.Error(w, "Error getting FN handler", http.StatusInternalServerError)
		return
	}

	if fmd == nil {
		http.Error(w, "No such function", http.StatusServiceUnavailable)
		return
	}

	fmd.Handle(ctx, w, r, sopq, path, e.key)
}

type RtTblProp struct { }

func (_ *RtTblProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	rt := o.(*RouterDesc)
	f := 0
	t := len(rt.Table)

	if q != nil {
		f, e := xhttp.ReqAtoi(q, "from", f)
		if f < 0 || e != nil {
			return nil, GateErrM(swyapi.GateBadRequest, "Invalid range")
		}
		t, e := xhttp.ReqAtoi(q, "to", t)
		if t > len(rt.Table) || e != nil {
			return nil, GateErrM(swyapi.GateBadRequest, "Invalid range")
		}
	}

	return rt.Table[f:t], nil
}

func (_ *RtTblProp)Upd(ctx context.Context, o xrest.Obj, par interface{}) *xrest.ReqErr {
	return o.(*RouterDesc).setTable(ctx, *par.(*[]*swyapi.RouterEntry))
}
