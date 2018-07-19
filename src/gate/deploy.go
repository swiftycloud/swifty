package main

import (
	"encoding/json"
	"gopkg.in/mgo.v2/bson"
	"context"
	"../common"
	"../apis/apps"
)

var depStates = map[int]string {
	swy.DBDepStateIni: "initializing",
	swy.DBDepStateRdy: "ready",
	swy.DBDepStateStl: "stalled",
	swy.DBDepStateTrm: "terminating",
}

type DeployItemDesc struct {
	Fn	*FunctionDesc	`bson:"fn"`
	Mw	*MwareDesc	`bson:"mw"`

	FnSrc	string		`bson:"fnsrc,omitempty"`
	src	*swyapi.FunctionSources	`bson:"-"`
}

func (i *DeployItemDesc)start(ctx context.Context) *swyapi.GateErr {
	if i.Fn != nil {
		if i.src == nil {
			var src swyapi.FunctionSources

			err := json.Unmarshal([]byte(i.FnSrc), &src)
			if err != nil {
				return GateErrE(swy.GateGenErr, err)
			}
			i.src = &src
		}

		_, cerr := i.Fn.Add(ctx, &conf, i.src)
		return cerr
	}

	if i.Mw != nil {
		_, cerr := i.Mw.Setup(ctx, &conf.Mware)
		return cerr
	}

	return GateErrM(swy.GateGenErr, "Bad deploy item")
}

func (i *DeployItemDesc)stop(ctx context.Context) *swyapi.GateErr {
	if i.Fn != nil {
		return removeFunctionId(ctx, &conf, &i.Fn.SwoId)
	}

	if i.Mw != nil {
		return mwareRemoveId(ctx, &conf.Mware, &i.Mw.SwoId)
	}

	return GateErrM(swy.GateGenErr, "Bad deploy item")
}

func (i *DeployItemDesc)info(ctx context.Context, details bool) (*swyapi.DeployItemInfo) {
	if i.Fn != nil {
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

	if i.Mw != nil {
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

	return &swyapi.DeployItemInfo{Type: "unknown"}
}

type DeployDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Labels		[]string		`bson:"labels"`
	Cookie		string			`bson:"cookie"`
	Items		[]*DeployItemDesc	`bson:"items"`
	State		int			`bson:"state"`
}

func deployStartItems(dep *DeployDesc) {
	ctx, done := mkContext("::deploy start")
	defer done(ctx)

	for i, item := range dep.Items {
		cerr := item.start(ctx)
		if cerr == nil {
			continue
		}

		deployStopItems(ctx, dep, i)
		dbUpdatePart(ctx, dep, bson.M{"state": swy.DBDepStateStl})
		return
	}

	dbUpdatePart(ctx, dep, bson.M{"state": swy.DBDepStateRdy})
	return
}

func deployStopItems(ctx context.Context, dep *DeployDesc, till int) *swyapi.GateErr {
	var err *swyapi.GateErr

	for i, item := range dep.Items {
		if i >= till {
			break
		}

		e := item.stop(ctx)
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

func (dep *DeployDesc)getItems(items []*swyapi.DeployItem) *swyapi.GateErr {
	id := dep.SwoId
	for _, item := range items {
		if item.Function != nil && item.Mware == nil {
			er := swyFixSize(&item.Function.Size, &conf)
			if er != nil {
				return GateErrE(swy.GateBadRequest, er)
			}

			srcd, er := json.Marshal(&item.Function.Sources)
			if er != nil {
				return GateErrE(swy.GateGenErr, er)
			}

			id.Name = item.Function.Name
			fd := getFunctionDesc(&id, item.Function)
			fd.Labels = dep.Labels
			dep.Items = append(dep.Items, &DeployItemDesc{ Fn: fd, FnSrc: string(srcd), src: &item.Function.Sources })
			continue
		}

		if item.Mware != nil && item.Function == nil {
			id.Name = item.Mware.Name
			md := getMwareDesc(&id, item.Mware)
			md.Labels = dep.Labels
			dep.Items = append(dep.Items, &DeployItemDesc{ Mw: md })
			continue
		}

		return GateErrM(swy.GateBadRequest, "Bad item")
	}

	return nil
}

func deployStart(ctx context.Context, dep *DeployDesc) (string, *swyapi.GateErr) {
	dep.ObjID = bson.NewObjectId()
	err := dbInsert(ctx, dep)
	if err != nil {
		return "", GateErrD(err)
	}

	go deployStartItems(dep)

	return dep.ObjID.Hex(), nil
}

func (dep *DeployDesc)toInfo(ctx context.Context, details bool) (*swyapi.DeployInfo, *swyapi.GateErr) {
	ret := &swyapi.DeployInfo {
		Id:		dep.ObjID.Hex(),
		Name:		dep.SwoId.Name,
		Project:	dep.SwoId.Project,
		State:		depStates[dep.State],
		Labels:		dep.Labels,
	}

	for _, item := range dep.Items {
		ret.Items = append(ret.Items, item.info(ctx, details))
	}

	return ret, nil
}

func deployStop(ctx context.Context, dep *DeployDesc) (*swyapi.GateErr) {
	cerr := deployStopItems(ctx, dep, len(dep.Items))
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
		if dep.State == swy.DBDepStateIni {
			glog.Debugf("Will restart deploy %s in state %d", dep.SwoId.Str(), dep.State)
			deployStopItems(ctx, dep, len(dep.Items))
			go deployStartItems(dep)
		}
	}

	return nil
}
