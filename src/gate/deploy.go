package main

import (
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/yaml.v2"
	"encoding/base64"
	"context"
	"net/url"
	"net/http"
	"bytes"
	"swifty/common/xrest"
	"swifty/common/http"
	"swifty/apis"
)

const (
	DBDepStateIni	int = 1
	DBDepStateRdy	int = 2
	DBDepStateStl	int = 3
	DBDepStateTrm	int = 4
)

var maxIncludeDepth = 4 /* FIXME -- properly detect recursion */

func init() {
	addIntSysctl("deploy_include_depth_max", &maxIncludeDepth)
}

var depStates = map[int]string {
	DBDepStateIni: "initializing",
	DBDepStateRdy: "ready",
	DBDepStateStl: "stalled",
	DBDepStateTrm: "terminating",
}

type _DeployItemDesc struct {
	Fn	*FunctionDesc	`bson:"fn"`
	Mw	*MwareDesc	`bson:"mw"`

	FnSrc	string		`bson:"fnsrc,omitempty"`
	src	*swyapi.FunctionSources	`bson:"-"`
}

type DeployFunction struct {
	Id	SwoId		`bson:"id"`

	Fn	*FunctionDesc	`bson:"fn,omitempty"`
	FnSrc	string		`bson:"fnsrc,omitempty"`
	src	*swyapi.FunctionSources	`bson:"-"`
	Evs	[]*FnEventDesc	`bson:"events,omitempty"`
}

type DeployMware struct {
	Id	SwoId		`bson:"id"`

	Mw	*MwareDesc	`bson:"mw,omitempty"`
}

type DeployRouter struct {
	Id	SwoId		`bson:"id"`

	Rt	*RouterDesc	`bson:"rt,omitempty"`
}

func (i *DeployFunction)start(ctx context.Context) *xrest.ReqErr {
	if i.src == nil {
		var src swyapi.FunctionSources

		err := json.Unmarshal([]byte(i.FnSrc), &src)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
		i.src = &src
	}

	cerr := i.Fn.Add(ctx, &swyapi.FunctionAdd{Sources: *i.src})
	if cerr != nil {
		return cerr
	}

	for _, ed := range i.Evs {
		cerr = ed.Add(ctx, i.Fn)
		if cerr != nil {
			i.stop(ctx) /* fn.Remove() would kill all events */
			return cerr
		}
	}

	return nil
}

func (i *DeployMware)start(ctx context.Context) *xrest.ReqErr {
	return i.Mw.Add(ctx, nil)
}

func (i *DeployRouter)start(ctx context.Context) *xrest.ReqErr {
	return i.Rt.Add(ctx, nil)
}

func (i *DeployFunction)stop(ctx context.Context) *xrest.ReqErr {
	return removeFunctionId(ctx, &i.Id)
}

func (i *DeployMware)stop(ctx context.Context) *xrest.ReqErr {
	return mwareRemoveId(ctx, &i.Id)
}

func (i *DeployRouter)stop(ctx context.Context) *xrest.ReqErr {
	return routerStopId(ctx, &i.Id)
}

func (i *DeployFunction)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	ret := &swyapi.DeployItemInfo{Type: "function", Name: i.Id.Name}

	if details {
		var fn FunctionDesc
		err := dbFind(ctx, i.Id.dbReq(), &fn)
		if err == nil {
			ret.State = fnStates[fn.State]
			ret.Id = fn.ObjID.Hex()
		} else {
			ret.State = fnStates[DBFuncStateNo]
		}
	}

	return ret
}

func (i *DeployMware)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	ret := &swyapi.DeployItemInfo{Type: "mware", Name: i.Id.Name}

	if details {
		var mw MwareDesc
		err := dbFind(ctx, i.Id.dbReq(), &mw)
		if err == nil {
			ret.State = mwStates[mw.State]
			ret.Id = mw.ObjID.Hex()
		} else {
			ret.State = mwStates[DBMwareStateNo]
		}
	}

	return ret
}

func (i *DeployRouter)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	return &swyapi.DeployItemInfo{Type: "routers", Name: i.Id.Name}
}

type DeployDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Labels		[]string		`bson:"labels"`
	Cookie		string			`bson:"cookie"`
	State		int			`bson:"state"`
	Functions	[]*DeployFunction	`bson:"functions"`
	Mwares		[]*DeployMware		`bson:"mwares"`
	Routers		[]*DeployRouter		`bson:"routers"`
}

type Deployments struct {
	auth bool
}

func deployStartItems(dep *DeployDesc) {
	ctx, done := mkContext("::deploy start")
	defer done(ctx)
	gctx(ctx).tpush(dep.SwoId.Tennant)

	dep.StartItems(ctx)
}

func deployRestartItems(dep *DeployDesc) {
	ctx, done := mkContext("::deploy restart")
	defer done(ctx)
	gctx(ctx).tpush(dep.SwoId.Tennant)

	if dep.StopItems(ctx) == nil {
		dep.StartItems(ctx)
	}
}

func deployStopItems(dep *DeployDesc) {
	ctx, done := mkContext("::deploy stop")
	defer done(ctx)
	gctx(ctx).tpush(dep.SwoId.Tennant)

	dep.StopItems(ctx)
}

func (dep *DeployDesc)StartItems(ctx context.Context) {
	var fs, ms, rs int
	var fn *DeployFunction
	var mw *DeployMware
	var rt *DeployRouter

	mws := []*DeployMware{}
	fns := []*DeployFunction{}
	rts := []*DeployRouter{}

	for ms, mw = range dep.Mwares {
		cerr := mw.start(ctx)
		if cerr == nil {
			mws = append(mws, &DeployMware{Id: mw.Id})
		} else {
			ctxlog(ctx).Errorf("Cannot start mw.%s: %s", mw.Id.Str(), cerr.Message)
			goto erm
		}
	}

	for fs, fn = range dep.Functions {
		cerr := fn.start(ctx)
		if cerr == nil {
			fns = append(fns, &DeployFunction{Id: fn.Id})
		} else {
			ctxlog(ctx).Errorf("Cannot start fn.%s: %s", fn.Id.Str(), cerr.Message)
			goto erf
		}
	}

	for rs, rt = range dep.Routers {
		cerr := rt.start(ctx)
		if cerr == nil {
			rts = append(rts, &DeployRouter{Id: rt.Id})
		} else {
			ctxlog(ctx).Errorf("Cannot start rt.%s: %s", rt.Id.Str(), cerr.Message)
			goto err
		}
	}

	dep.State = DBDepStateRdy
	dep.Functions = fns
	dep.Mwares = mws
	dep.Routers = rts
	dbUpdateAll(ctx, dep)
	return

err:
	deployStopRouters(ctx, dep, rs)
erf:
	deployStopFunctions(ctx, dep, fs)
erm:
	deployStopMwares(ctx, dep, ms)

	ctxlog(ctx).Errorf("Failed to start %s dep (stopped %d,%d,%d)", dep.SwoId.Str(), rs, fs, ms)
	dbUpdatePart(ctx, dep, bson.M{"state": DBDepStateStl})
}

func deployStopFunctions(ctx context.Context, dep *DeployDesc, till int) *xrest.ReqErr {
	var err *xrest.ReqErr

	for i, f := range dep.Functions {
		if i >= till {
			break
		}

		e := f.stop(ctx)
		if e != nil  && e.Code != swyapi.GateNotFound {
			err = e
		}
	}

	return err
}

func deployStopMwares(ctx context.Context, dep *DeployDesc, till int) *xrest.ReqErr {
	var err *xrest.ReqErr

	for i, m := range dep.Mwares {
		if i >= till {
			break
		}

		e := m.stop(ctx)
		if e != nil  && e.Code != swyapi.GateNotFound {
			err = e
		}
	}

	return err
}

func deployStopRouters(ctx context.Context, dep *DeployDesc, till int) *xrest.ReqErr {
	var err *xrest.ReqErr

	for i, r := range dep.Routers {
		if i >= till {
			break
		}

		e := r.stop(ctx)
		if e != nil  && e.Code != swyapi.GateNotFound {
			err = e
		}
	}

	return err
}

func getDeployDesc(id *SwoId) *DeployDesc {
	dd := &DeployDesc {
		SwoId: *id,
		State: DBDepStateIni,
		Cookie: id.Cookie(),
	}

	return dd
}

func (dep *DeployDesc)getItems(ctx context.Context, ds *swyapi.DeployStart) *xrest.ReqErr {
	if ds.Params == nil {
		ds.Params = map[string]string { "name": dep.SwoId.Name }
	} else if _, ok := ds.Params["name"]; !ok {
		ds.Params["name"] = dep.SwoId.Name
	}

	return dep.getItemsParams(ctx, &ds.From, ds.Params, 0)
}

func (dep *DeployDesc)getItemsParams(ctx context.Context, from *swyapi.DeploySource, params map[string]string, depth int) *xrest.ReqErr {
	var dd swyapi.DeployDescription
	var desc []byte
	var err error

	switch {
	case from.Descr != "":
		desc, err = base64.StdEncoding.DecodeString(from.Descr)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	case from.Repo != "":
		desc, err = repoReadFile(ctx, from.Repo)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	case from.URL != "":
		desc, err = xhttp.ReadFromURL(from.URL)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

	default:
		return GateErrM(swyapi.GateBadRequest, "Unsupported type")
	}

	for name, value := range params {
		desc = bytes.Replace(desc, []byte("%" + name + "%"), []byte(value), -1)
	}

	err = yaml.Unmarshal(desc, &dd)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	return dep.getItemsDesc(ctx, &dd, params, depth)
}

func (dep *DeployDesc)getItemsDesc(ctx context.Context, dd *swyapi.DeployDescription, params map[string]string, depth int) *xrest.ReqErr {
	id := dep.SwoId

	for _, inc := range dd.Include {
		if depth >= maxIncludeDepth {
			return GateErrM(swyapi.GateBadRequest, "Too many includes")
		}

		cer := dep.getItemsParams(ctx, &inc.DeploySource, params, depth + 1)
		if cer != nil {
			return cer
		}
	}

	for _, fn := range dd.Functions {
		srcd, er := json.Marshal(fn.Sources)
		if er != nil {
			return GateErrE(swyapi.GateGenErr, er)
		}

		id.Name = fn.Name
		fd, cerr := getFunctionDesc(&id, fn)
		if cerr != nil {
			return cerr
		}

		evs := []*FnEventDesc{}
		for _, ev := range fn.Events {
			ed, cerr := getEventDesc(&ev)
			if cerr != nil {
				return cerr
			}

			evs = append(evs, ed)
		}

		fd.Labels = dep.Labels
		dep.Functions = append(dep.Functions, &DeployFunction{
			Id: id, Fn: fd, FnSrc: string(srcd), src: &fn.Sources, Evs: evs,
		})
	}

	for _, mw := range dd.Mwares {
		id.Name = mw.Name
		md, cerr := getMwareDesc(&id, mw)
		if cerr != nil {
			return cerr
		}

		md.Labels = dep.Labels
		dep.Mwares = append(dep.Mwares, &DeployMware{
			Id: id, Mw: md,
		})
	}

	for _, rt := range dd.Routers {
		id.Name = rt.Name
		rt, cerr := getRouterDesc(&id, rt)
		if cerr != nil {
			return cerr
		}

		rt.Labels = dep.Labels
		dep.Routers = append(dep.Routers, &DeployRouter{
			Id: id, Rt: rt,
		})
	}

	return nil
}

func (dep *DeployDesc)Start(ctx context.Context) *xrest.ReqErr {
	dep.ObjID = bson.NewObjectId()
	err := dbInsert(ctx, dep)
	if err != nil {
		return GateErrD(err)
	}

	go deployStartItems(dep)

	return nil
}

func (ds Deployments)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var dd DeployDesc
	var cerr *xrest.ReqErr

	if ds.auth {
		cerr = objFindForReq2(ctx, r, "aid", &dd, bson.M{"labels": "auth"})
	} else {
		cerr = objFindForReq(ctx, r, "did", &dd)
	}

	if cerr != nil {
		return nil, cerr
	}

	return &dd, nil
}

func (_ Deployments)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	var err error

	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	dname := q.Get("name")
	var dep DeployDesc

	if dname != "" {
		err = dbFind(ctx, cookieReq(ctx, project, dname), &dep)
		if err != nil {
			return GateErrD(err)
		}

		return cb(ctx, &dep)
	}

	iter := dbIterAll(ctx, listReq(ctx, project, q["label"]), &dep)
	defer iter.Close()

	for iter.Next(&dep) {
		cerr := cb(ctx, &dep)
		if cerr != nil {
			return cerr
		}
	}

	err = iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (_ Deployments)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.DeployStart)
	return getDeployDesc(ctxSwoId(ctx, params.Project, params.Name)), nil
}

func (dep *DeployDesc)Add(ctx context.Context, p interface{}) *xrest.ReqErr {
	params := p.(*swyapi.DeployStart)

	cerr := dep.getItems(ctx, params)
	if cerr != nil {
		return cerr
	}

	cerr = dep.Start(ctx)
	if cerr != nil {
		return cerr
	}

	return nil
}

func (dep *DeployDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return dep.toInfo(ctx, details)
}

func (dep *DeployDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (dep *DeployDesc)toInfo(ctx context.Context, details bool) (*swyapi.DeployInfo, *xrest.ReqErr) {
	ret := &swyapi.DeployInfo {
		Id:		dep.ObjID.Hex(),
		Name:		dep.SwoId.Name,
		Project:	dep.SwoId.Project,
		State:		depStates[dep.State],
		Labels:		dep.Labels,
	}

	for _, f := range dep.Functions {
		ret.Items = append(ret.Items, f.info(ctx, details))
	}
	for _, m := range dep.Mwares {
		ret.Items = append(ret.Items, m.info(ctx, details))
	}

	for _, r := range dep.Routers {
		ret.Items = append(ret.Items, r.info(ctx, details))
	}

	return ret, nil
}

func (dep *DeployDesc)StopItems(ctx context.Context) *xrest.ReqErr {
	cerr := deployStopRouters(ctx, dep, len(dep.Routers))
	if cerr != nil {
		return cerr
	}

	cerr = deployStopFunctions(ctx, dep, len(dep.Functions))
	if cerr != nil {
		return cerr
	}

	cerr = deployStopMwares(ctx, dep, len(dep.Mwares))
	if cerr != nil {
		return cerr
	}

	return nil
}

func (dep *DeployDesc)Del(ctx context.Context) (*xrest.ReqErr) {
	err := dbUpdatePart(ctx, dep, bson.M{"state": DBDepStateTrm})
	if err != nil {
		return GateErrD(err)
	}

	cerr := dep.StopItems(ctx)
	if cerr != nil {
		return cerr
	}

	err = dbRemove(ctx, dep)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func DeployInit(ctx context.Context) error {
	var dep DeployDesc

	iter := dbIterAll(ctx, bson.M{}, &dep)
	defer iter.Close()

	for iter.Next(&dep) {
		if dep.State == DBDepStateIni {
			ctxlog(ctx).Debugf("Will restart deploy %s", dep.SwoId.Str())
			go deployRestartItems(&dep)
		}
		if dep.State == DBDepStateTrm {
			ctxlog(ctx).Debugf("Will finish deploy stop %s", dep.SwoId.Str())
			go deployStopItems(&dep)
		}
	}

	return nil
}
