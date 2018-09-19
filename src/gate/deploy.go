package main

import (
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/yaml.v2"
	"encoding/base64"
	"context"
	"net/url"
	"../common/xrest"
	"../apis"
)

const (
	DBDepStateIni	int = 1
	DBDepStateRdy	int = 2
	DBDepStateStl	int = 3
	DBDepStateTrm	int = 4
)

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

func (i *DeployFunction)stop(ctx context.Context) *xrest.ReqErr {
	return removeFunctionId(ctx, &i.Id)
}

func (i *DeployMware)stop(ctx context.Context) *xrest.ReqErr {
	return mwareRemoveId(ctx, &i.Id)
}

func (i *DeployFunction)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	ret := &swyapi.DeployItemInfo{Type: "function", Name: i.Id.Name}

	if details {
		var fn FunctionDesc
		err := dbFind(ctx, i.Id.dbReq(), &fn)
		if err == nil {
			ret.State = fnStates[fn.State]
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
		} else {
			ret.State = mwStates[DBMwareStateNo]
		}
	}

	return ret
}

type DeployDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Labels		[]string		`bson:"labels"`
	Cookie		string			`bson:"cookie"`
	State		int			`bson:"state"`
	Functions	[]*DeployFunction	`bson:"functions"`
	Mwares		[]*DeployMware		`bson:"mwares"`

	_Items		[]*_DeployItemDesc	`bson:"items,omitempty"`
}

type Deployments struct {}

func deployStartItems(dep *DeployDesc) {
	ctx, done := mkContext("::deploy start")
	defer done(ctx)

	mws := []*DeployMware{}
	for i, mw := range dep.Mwares {
		cerr := mw.start(ctx)
		if cerr == nil {
			mws = append(mws, &DeployMware{Id: mw.Id})
			continue
		}

		deployStopMwares(ctx, dep, i)
		dbUpdatePart(ctx, dep, bson.M{"state": DBDepStateStl})
		return
	}

	fns := []*DeployFunction{}
	for i, fn := range dep.Functions {
		cerr := fn.start(ctx)
		if cerr == nil {
			fns = append(fns, &DeployFunction{Id: fn.Id})
			continue
		}

		deployStopFunctions(ctx, dep, i)
		deployStopMwares(ctx, dep, len(dep.Mwares))
		dbUpdatePart(ctx, dep, bson.M{"state": DBDepStateStl})
		return
	}

	dep.State = DBDepStateRdy
	dep.Functions = fns
	dep.Mwares = mws
	dbUpdateAll(ctx, dep)
	return
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

func getDeployDesc(id *SwoId) *DeployDesc {
	dd := &DeployDesc {
		SwoId: *id,
		State: DBDepStateIni,
		Cookie: id.Cookie(),
	}

	return dd
}

func (dep *DeployDesc)getItems(ctx context.Context, ds *swyapi.DeployStart) *xrest.ReqErr {
	var dd swyapi.DeployDescription
	var desc []byte
	var err error

	switch ds.From.Type {
	case "desc":
		desc, err = base64.StdEncoding.DecodeString(ds.From.Descr)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	case "repo":
		desc, err = repoReadFile(ctx, ds.From.Repo)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

	default:
		return GateErrM(swyapi.GateBadRequest, "Unsupported type")
	}

	err = yaml.Unmarshal(desc, &dd)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	return dep.getItemsDesc(&dd)
}

func (dep *DeployDesc)getItemsDesc(dd *swyapi.DeployDescription) *xrest.ReqErr {
	id := dep.SwoId

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
		md := getMwareDesc(&id, mw)
		md.Labels = dep.Labels
		dep.Mwares = append(dep.Mwares, &DeployMware{
			Id: id, Mw: md,
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

func (_ Deployments)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	var deps []*DeployDesc
	var err error

	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	dname := q.Get("name")
	if dname == "" {
		err = dbFindAll(ctx, listReq(ctx, project, q["label"]), &deps)
		if err != nil {
			return GateErrD(err)
		}
	} else {
		var dep DeployDesc

		err = dbFind(ctx, cookieReq(ctx, project, dname), &dep)
		if err != nil {
			return GateErrD(err)
		}
		deps = append(deps, &dep)
	}

	for _, d := range deps {
		cerr := cb(ctx, d)
		if cerr != nil {
			return cerr
		}
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

	return ret, nil
}

func (dep *DeployDesc)Del(ctx context.Context) (*xrest.ReqErr) {
	cerr := deployStopFunctions(ctx, dep, len(dep.Functions))
	if cerr != nil {
		return cerr
	}

	cerr = deployStopMwares(ctx, dep, len(dep.Mwares))
	if cerr != nil {
		return cerr
	}

	err := dbRemove(ctx, dep)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func DeployInit(ctx context.Context, conf *YAMLConf) error {
	var deps []*DeployDesc

	err := dbFindAll(ctx, bson.M{}, &deps)
	if err != nil {
		return err
	}

	for _, dep := range deps {
		if len(dep._Items) != 0 {
			ctxlog(ctx).Debugf("Convert deploy %s", dep.ObjID.Hex())
			for _, i := range dep._Items {
				if i.Fn != nil {
					dep.Functions = append(dep.Functions, &DeployFunction{
						Fn: i.Fn, FnSrc: i.FnSrc,
					})
				}
				if i.Mw != nil {
					dep.Mwares = append(dep.Mwares, &DeployMware {
						Mw: i.Mw,
					})
				}
			}
			err = dbUpdateAll(ctx, dep)
			if err != nil {
				ctxlog(ctx).Debugf("Error updating mware: %s", err.Error())
				return err
			}
		}

		if dep.State == DBDepStateIni {
			ctxlog(ctx).Debugf("Will restart deploy %s in state %d", dep.SwoId.Str(), dep.State)
			deployStopFunctions(ctx, dep, len(dep.Functions))
			deployStopMwares(ctx, dep, len(dep.Mwares))
			go deployStartItems(dep)
		}
	}

	return nil
}
