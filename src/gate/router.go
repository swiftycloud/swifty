package main

import (
	"net/http"
	"../apis"
	"context"
	"../common"
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

func ckTable(tbl []*swyapi.RouterEntry) *swyapi.GateErr {
	for _, t := range tbl {
		if len(t.Key) > TableKeyLenMax {
			return GateErrM(swy.GateBadRequest, "Too long key")
		}
	}

	return nil
}

func getRouterDesc(id *SwoId, params *swyapi.RouterAdd) (*RouterDesc, *swyapi.GateErr) {
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

func (_ Routers)create(ctx context.Context, r *http.Request, p interface{}) (Obj, *swyapi.GateErr) {
	params := p.(*swyapi.RouterAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getRouterDesc(id, params)
}

func (rt *RouterDesc)add(ctx context.Context, params interface{}) *swyapi.GateErr {
	return rt.Create(ctx)
}

func (rt *RouterDesc)info(ctx context.Context, r *http.Request, details bool) (interface{}, *swyapi.GateErr) {
	return rt.toInfo(ctx, details), nil
}

func (rt *RouterDesc)upd(ctx context.Context, upd interface{}) *swyapi.GateErr {
	return GateErrM(swy.GateGenErr, "Not updatable")
}

func (rt *RouterDesc)del(ctx context.Context) *swyapi.GateErr {
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

func (rd *RouterDesc)Create(ctx context.Context) *swyapi.GateErr {
	rd.ObjID = bson.NewObjectId()
	rd.Cookie = rd.SwoId.Cookie()
	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (rd *RouterDesc)Remove(ctx context.Context) *swyapi.GateErr {
	err := dbRemove(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	urlClean(ctx, URLRouter, rd.Cookie)
	return nil
}

func (rd *RouterDesc)setTable(ctx context.Context, tbl []*swyapi.RouterEntry) *swyapi.GateErr {
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

func listRouters(ctx context.Context, project, name string) ([]*RouterDesc, *swyapi.GateErr) {
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
