package main

import (
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"context"
	"net/url"
	"../common"
	"../apis"
)

var depStates = map[int]string {
	swy.DBDepStateIni: "initializing",
	swy.DBDepStateRdy: "ready",
	swy.DBDepStateStl: "stalled",
	swy.DBDepStateTrm: "terminating",
}

type _DeployItemDesc struct {
	Fn	*FunctionDesc	`bson:"fn"`
	Mw	*MwareDesc	`bson:"mw"`

	FnSrc	string		`bson:"fnsrc,omitempty"`
	src	*swyapi.FunctionSources	`bson:"-"`
}

type DeployFunction struct {
	Fn	*FunctionDesc	`bson:"fn"`
	FnSrc	string		`bson:"fnsrc,omitempty"`
	src	*swyapi.FunctionSources	`bson:"-"`
	Evs	[]*FnEventDesc	`bson:"events,omitempty"`
}

type DeployMware struct {
	Mw	*MwareDesc	`bson:"mw"`
}

func (i *DeployFunction)start(ctx context.Context) *swyapi.GateErr {
	if i.src == nil {
		var src swyapi.FunctionSources

		err := json.Unmarshal([]byte(i.FnSrc), &src)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
		i.src = &src
	}

	cerr := i.Fn.Add(ctx, i.src)
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

func (i *DeployMware)start(ctx context.Context) *swyapi.GateErr {
	return i.Mw.Setup(ctx)
}

func (i *DeployFunction)stop(ctx context.Context) *swyapi.GateErr {
	return removeFunctionId(ctx, &i.Fn.SwoId)
}

func (i *DeployMware)stop(ctx context.Context) *swyapi.GateErr {
	return mwareRemoveId(ctx, &i.Mw.SwoId)
}

func (i *DeployFunction)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	ret := &swyapi.DeployItemInfo{Type: "function", Name: i.Fn.SwoId.Name}

	if details {
		var fn FunctionDesc
		err := dbFind(ctx, i.Fn.SwoId.dbReq(), &fn)
		if err == nil {
			ret.State = fnStates[fn.State]
		} else {
			ret.State = fnStates[swy.DBFuncStateNo]
		}
	}

	return ret
}

func (i *DeployMware)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	ret := &swyapi.DeployItemInfo{Type: "mware", Name: i.Mw.SwoId.Name}

	if details {
		var mw MwareDesc
		err := dbFind(ctx, i.Mw.SwoId.dbReq(), &mw)
		if err == nil {
			ret.State = mwStates[mw.State]
		} else {
			ret.State = mwStates[swy.DBMwareStateNo]
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

	for i, mw := range dep.Mwares {
		cerr := mw.start(ctx)
		if cerr == nil {
			continue
		}

		deployStopMwares(ctx, dep, i)
		dbUpdatePart(ctx, dep, bson.M{"state": swy.DBDepStateStl})
		return
	}

	for i, fn := range dep.Functions {
		cerr := fn.start(ctx)
		if cerr == nil {
			continue
		}

		deployStopFunctions(ctx, dep, i)
		deployStopMwares(ctx, dep, len(dep.Mwares))
		dbUpdatePart(ctx, dep, bson.M{"state": swy.DBDepStateStl})
		return
	}

	dbUpdatePart(ctx, dep, bson.M{"state": swy.DBDepStateRdy})
	return
}

func deployStopFunctions(ctx context.Context, dep *DeployDesc, till int) *swyapi.GateErr {
	var err *swyapi.GateErr

	for i, f := range dep.Functions {
		if i >= till {
			break
		}

		e := f.stop(ctx)
		if e != nil  && e.Code != swy.GateNotFound {
			err = e
		}
	}

	return err
}

func deployStopMwares(ctx context.Context, dep *DeployDesc, till int) *swyapi.GateErr {
	var err *swyapi.GateErr

	for i, m := range dep.Mwares {
		if i >= till {
			break
		}

		e := m.stop(ctx)
		if e != nil  && e.Code != swy.GateNotFound {
			err = e
		}
	}

	return err
}

func getDeployDesc(id *SwoId) *DeployDesc {
	dd := &DeployDesc {
		SwoId: *id,
		State: swy.DBDepStateIni,
		Cookie: id.Cookie(),
	}

	return dd
}

func (dep *DeployDesc)getItems(ds *swyapi.DeployStart) *swyapi.GateErr {
	id := dep.SwoId

	for _, fn := range ds.Functions {
		srcd, er := json.Marshal(&fn.Sources)
		if er != nil {
			return GateErrE(swy.GateGenErr, er)
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
			Fn: fd, FnSrc: string(srcd), src: &fn.Sources, Evs: evs,
		})
	}

	for _, mw := range ds.Mwares {
		id.Name = mw.Name
		md := getMwareDesc(&id, mw)
		md.Labels = dep.Labels
		dep.Mwares = append(dep.Mwares, &DeployMware{ Mw: md })
	}

	return nil
}

func (dep *DeployDesc)Start(ctx context.Context) *swyapi.GateErr {
	dep.ObjID = bson.NewObjectId()
	err := dbInsert(ctx, dep)
	if err != nil {
		return GateErrD(err)
	}

	go deployStartItems(dep)

	return nil
}

func (_ Deployments)iterate(ctx context.Context, q url.Values, cb func(context.Context, Obj) *swyapi.GateErr) *swyapi.GateErr {
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

func (_ Deployments)create(ctx context.Context, q url.Values, p interface{}) (Obj, *swyapi.GateErr) {
	params := p.(*swyapi.DeployStart)
	return getDeployDesc(ctxSwoId(ctx, params.Project, params.Name)), nil
}

func (dep *DeployDesc)add(ctx context.Context, p interface{}) *swyapi.GateErr {
	params := p.(*swyapi.DeployStart)

	cerr := dep.getItems(params)
	if cerr != nil {
		return cerr
	}

	cerr = dep.Start(ctx)
	if cerr != nil {
		return cerr
	}

	return nil
}

func (dep *DeployDesc)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	return dep.toInfo(ctx, details)
}

func (dep *DeployDesc)upd(ctx context.Context, upd interface{}) *swyapi.GateErr {
	return GateErrM(swy.GateGenErr, "Not updatable")
}

func (dep *DeployDesc)del(ctx context.Context) *swyapi.GateErr {
	return dep.Stop(ctx)
}

func (dep *DeployDesc)toInfo(ctx context.Context, details bool) (*swyapi.DeployInfo, *swyapi.GateErr) {
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

func (dep *DeployDesc)Stop(ctx context.Context) (*swyapi.GateErr) {
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

		if dep.State == swy.DBDepStateIni {
			ctxlog(ctx).Debugf("Will restart deploy %s in state %d", dep.SwoId.Str(), dep.State)
			deployStopFunctions(ctx, dep, len(dep.Functions))
			deployStopMwares(ctx, dep, len(dep.Mwares))
			go deployStartItems(dep)
		}
	}

	return nil
}
