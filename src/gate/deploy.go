package main

import (
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
}

func (i *DeployItemDesc)start(ctx context.Context) *swyapi.GateErr {
	if i.Fn != nil {
		return addFunction(ctx, &conf, i.Fn)
	}

	if i.Mw != nil {
		return mwareSetup(ctx, &conf.Mware, i.Mw)
	}

	return GateErrM(swy.GateGenErr, "Bad deploy item")
}

func (i *DeployItemDesc)stop(ctx context.Context) *swyapi.GateErr {
	if i.Fn != nil {
		return removeFunction(ctx, &conf, &i.Fn.SwoId)
	}

	if i.Mw != nil {
		return mwareRemove(ctx, &conf.Mware, &i.Mw.SwoId)
	}

	return GateErrM(swy.GateGenErr, "Bad deploy item")
}

func (i *DeployItemDesc)info() (*swyapi.DeployItemInfo) {
	if i.Fn != nil {
		ret := &swyapi.DeployItemInfo{Type: "function", Name: i.Fn.SwoId.Name}

		fn, err := dbFuncFind(&i.Fn.SwoId)
		if err == nil {
			ret.State = fnStates[fn.State]
		} else {
			ret.State = fnStates[swy.DBFuncStateNo]
		}

		return ret
	}

	if i.Mw != nil {
		ret := &swyapi.DeployItemInfo{Type: "mware", Name: i.Mw.SwoId.Name}

		mw, err := dbMwareGetItem(&i.Mw.SwoId)
		if err == nil {
			ret.State = mwStates[mw.State]
		} else {
			ret.State = mwStates[swy.DBMwareStateNo]
		}

		return ret
	}

	return &swyapi.DeployItemInfo{Type: "unknown"}
}

type DeployDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Cookie		string			`bson:"cookie"`
	Items		[]*DeployItemDesc	`bson:"items"`
	State		int			`bson:"state"`
}

func deployStartItems(ctx context.Context, dep *DeployDesc) {
	for i, item := range dep.Items {
		cerr := item.start(ctx)
		if cerr == nil {
			continue
		}

		deployStopItems(ctx, dep, i)
		dbDeployStateUpdate(dep, swy.DBDepStateStl)
		return
	}

	dbDeployStateUpdate(dep, swy.DBDepStateRdy)
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

func deployStart(ctx context.Context, params *swyapi.DeployStart) *swyapi.GateErr {
	var err error

	ten := fromContext(ctx).Tenant
	id := makeSwoId(ten, params.Project, params.Name)
	dep := &DeployDesc{ SwoId: *id, State: swy.DBDepStateIni, Cookie: id.Cookie() }

	for _, item := range params.Items {
		if item.Function != nil && item.Mware == nil {
			if item.Function.Project == "" {
				item.Function.Project = id.Project
			} else if item.Function.Project != id.Project {
				return GateErrM(swy.GateBadRequest, "Cross-project deploy")
			}

			dep.Items = append(dep.Items, &DeployItemDesc{
				Fn: getFunctionDesc(ten, item.Function),
			})
			continue
		}

		if item.Mware != nil && item.Function == nil {
			if item.Mware.Project == "" {
				item.Mware.Project = id.Project
			} else if item.Mware.Project != id.Project {
				return GateErrM(swy.GateBadRequest, "Cross-project deploy")
			}

			dep.Items = append(dep.Items, &DeployItemDesc{
				Mw: getMwareDesc(ten, item.Mware),
			})
			continue
		}

		return GateErrM(swy.GateBadRequest, "Bad item")
	}

	err = dbDeployAdd(dep)
	if err != nil {
		return GateErrD(err)
	}

	go deployStartItems(ctx, dep)

	return nil
}

func deployInfo(ctx context.Context, id *SwoId) (*swyapi.DeployInfo, *swyapi.GateErr) {
	var ret swyapi.DeployInfo

	dep, err := dbDeployGet(id)
	if err != nil {
		return nil, GateErrD(err)
	}

	ret.State = depStates[dep.State]
	for _, item := range dep.Items {
		ret.Items = append(ret.Items, item.info())
	}

	return &ret, nil
}

func deployStop(ctx context.Context, id *SwoId) (*swyapi.GateErr) {
	dep, err := dbDeployGet(id)
	if err != nil {
		return GateErrD(err)
	}

	cerr := deployStopItems(ctx, dep, len(dep.Items))
	if cerr != nil {
		return cerr
	}

	err = dbDeployDel(dep)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func DeployInit(conf *YAMLConf) error {
	deps, err := dbDeployList()
	if err != nil {
		return err
	}

	ctx := context.Background()
	for _, dep := range deps {
		if dep.State == swy.DBDepStateIni {
			glog.Debugf("Will restart deploy %s in state %d", dep.SwoId.Str(), dep.State)
			deployStopItems(ctx, &dep, len(dep.Items))
			go deployStartItems(ctx, &dep)
		}
	}

	return nil
}
