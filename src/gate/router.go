package main

import (
	"net/http"
	"../apis"
	"context"
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

func getRouterDesc(id *SwoId, params *swyapi.RouterAdd) (*RouterDesc, *swyapi.GateErr) {
	rd := RouterDesc {
		SwoId:	*id,
		Table:	params.Table,
	}

	return &rd, nil
}

func (rt *RouterDesc)getURL() string {
	return getURL(URLRouter, rt.Cookie)
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
	path := reqPath(r) /* FIXME -- this will be evaluated again in makeArgs */
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

		furl := FnURL{fd: fmd}
		furl.Handle(ctx, w, r, sopq)
		return
	}

	code := http.StatusNotFound
	if path_match {
		code = http.StatusMethodNotAllowed
	}
	http.Error(w, "", code)
}
