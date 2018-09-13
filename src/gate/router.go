package main

import (
	"net/http"
	"net/url"
	"../apis"
	"context"
	"../common"
	"../common/xrest"
	"gopkg.in/mgo.v2/bson"
)

type RouterDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Cookie		string			`bson:"cookie"`
	Table		[]*swyapi.RouterEntry	`bson:"table"`
}

type Routers struct {}

const TableKeyLenMax = 64

func ckTable(tbl []*swyapi.RouterEntry) *xrest.ReqErr {
	for _, t := range tbl {
		if len(t.Key) > TableKeyLenMax {
			return GateErrM(swy.GateBadRequest, "Too long key")
		}
	}

	return nil
}

func getRouterDesc(id *SwoId, params *swyapi.RouterAdd) (*RouterDesc, *xrest.ReqErr) {
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

func (_ Routers)iterate(ctx context.Context, q url.Values, cb func(context.Context, Obj) *xrest.ReqErr) *xrest.ReqErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}
	rname := q.Get("name")

	rts, cerr := listRouters(ctx, project, rname)
	if cerr != nil {
		return cerr
	}

	for _, rt := range rts {
		cerr = cb(ctx, rt)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (_ Routers)create(ctx context.Context, p interface{}) (Obj, *xrest.ReqErr) {
	params := p.(*swyapi.RouterAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getRouterDesc(id, params)
}

func (rt *RouterDesc)add(ctx context.Context, params interface{}) *xrest.ReqErr {
	return rt.Create(ctx)
}

func (rt *RouterDesc)info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return rt.toInfo(ctx, details), nil
}

func (rt *RouterDesc)upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrM(swy.GateGenErr, "Not updatable")
}

func (rt *RouterDesc)del(ctx context.Context) *xrest.ReqErr {
	return rt.Remove(ctx)
}

func (rt *RouterDesc)toInfo(ctx context.Context, details bool) *swyapi.RouterInfo {
	ri := swyapi.RouterInfo {
		Id:		rt.ObjID.Hex(),
		Name:		rt.SwoId.Name,
		Project:	rt.SwoId.Project,
		TLen:		len(rt.Table),
	}

	ri.URL = rt.getURL()

	return &ri
}

func (rd *RouterDesc)Create(ctx context.Context) *xrest.ReqErr {
	rd.ObjID = bson.NewObjectId()
	rd.Cookie = rd.SwoId.Cookie()
	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (rd *RouterDesc)Remove(ctx context.Context) *xrest.ReqErr {
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
	swyapi.RouterEntry
	cookie	string
}

type RouterURL struct {
	URL
	table	[]*RouterEntry
}

func listRouters(ctx context.Context, project, name string) ([]*RouterDesc, *xrest.ReqErr) {
	var rts []*RouterDesc

	if name == "" {
		err := dbFindAll(ctx, listReq(ctx, project, []string{}), &rts)
		if err != nil {
			return nil, GateErrD(err)
		}
	} else {
		var rt RouterDesc

		err := dbFind(ctx, cookieReq(ctx, project, name), &rt)
		if err != nil {
			return nil, GateErrD(err)
		}
		rts = append(rts, &rt)
	}

	return rts, nil
}

func (rt *RouterURL)Handle(ctx context.Context, w http.ResponseWriter, r *http.Request, sopq *statsOpaque) {
	path_match := false
	path := reqPath(r)
	for _, e := range rt.table {
		if e.Path != path {
			continue
		}
		path_match = true
		if e.Method != r.Method {
			continue
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

		fmd.Handle(ctx, w, r, sopq, path, e.Key)
		return
	}

	code := http.StatusNotFound
	if path_match {
		code = http.StatusMethodNotAllowed
	}
	http.Error(w, "", code)
}

type RtTblProp struct {
	rt *RouterDesc
}

func (p *RtTblProp)info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	rt := p.rt
	f := 0
	t := len(rt.Table)

	if q != nil {
		f, e := reqAtoi(q, "from", f)
		if f < 0 || e != nil {
			return nil, GateErrM(swy.GateBadRequest, "Invalid range")
		}
		t, e := reqAtoi(q, "to", t)
		if t > len(rt.Table) || e != nil {
			return nil, GateErrM(swy.GateBadRequest, "Invalid range")
		}
	}

	return rt.Table[f:t], nil
}

func (p *RtTblProp)upd(ctx context.Context, par interface{}) *xrest.ReqErr {
	return p.rt.setTable(ctx, *par.(*[]*swyapi.RouterEntry))
}

func (p *RtTblProp)del(context.Context) *xrest.ReqErr { return GateErrC(swy.GateNotAvail) }
func (p *RtTblProp)add(context.Context, interface{}) *xrest.ReqErr { return GateErrC(swy.GateNotAvail) }
