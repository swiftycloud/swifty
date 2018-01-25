package main

import (
	"errors"
	"fmt"
	"time"
	"context"
	"gopkg.in/mgo.v2/bson"

	"../apis/apps"
	"../common"
	"../common/xratelimit"
	"../common/xwait"
)

/*
 * On function states:
 *
 * Que: PODs are on their way
 * Bld: building is in progress (POD is starting or build cmd is running)
 * Blt: build completed, PODs are on their way
 * Rdy: ready to run (including rolling update in progress)
 * Upd: ready, but new build is coming (Rdy + Bld)
 * Stl: stalled -- first build failed. Only update or remove is possible
 *
 * handleFunctionAdd:
 *      if build -> Bld
 *      else     -> Que
 *      start PODs
 *
 * handleFunctionUpdate:
 *      if build -> Upd
 *               start PODs
 *      else     updatePods
 *
 * notifyPodUpdate:
 *      if Bld   doRun(build)
 *               if err   -> Stl
 *               else     -> Blt
 *                           restartPods
 *      elif Upd doRun(build)
 *               if OK    updatePODs
 *               -> Rdy
 *      else     -> Rdy
 *
 */
var fnStates = map[int]string {
	swy.DBFuncStateIni: "initializing",
	swy.DBFuncStateStr: "starting",
	swy.DBFuncStateRdy: "ready",
	swy.DBFuncStateBld: "building",
	swy.DBFuncStateUpd: "updating",
	swy.DBFuncStateDea: "deactivated",
	swy.DBFuncStateTrm: "terminating",
	swy.DBFuncStateStl: "stalled",
}

type FnCodeDesc struct {
	Lang		string		`bson:"lang"`
	Env		[]string	`bson:"env"`
}

type FnSrcDesc struct {
	Type		string		`bson:"type"`
	Repo		string		`bson:"repo,omitempty"`
	Version		string		`bson:"version"`		// Top commit in the repo
	Code		string		`bson:"-"`
}

type FnEventDesc struct {
	Source		string		`bson:"source"`
	CronTab		string		`bson:"crontab,omitempty"`
	MwareId		string		`bson:"mwid,omitempty"`
	MQueue		string		`bson:"mqueue,omitempty"`
	S3Bucket	string		`bson:"s3bucket,omitempty"`
}

type FnSizeDesc struct {
	Replicas	int		`bson:"replicas"`
	Mem		uint64		`bson:"mem"`
	Tmo		uint64		`bson:"timeout"`
	Burst		uint		`bson:"burst"`
	Rate		uint		`bson:"rate"`
}

type FunctionDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`		// Some "unique" identifier
	State		int		`bson:"state"`		// Function state
	CronID		int		`bson:"cronid"`		// ID of cron trigger (if present)
	URLCall		bool		`bson:"urlcall"`	// Function is callable via direct URL
	Event		FnEventDesc	`bson:"event"`
	Mware		[]string	`bson:"mware"`
	Code		FnCodeDesc	`bson:"code"`
	Src		FnSrcDesc	`bson:"src"`
	Size		FnSizeDesc	`bson:"size"`
	OneShot		bool		`bson:"oneshot"`
	UserData	string		`bson:"userdata,omitempty"`
}

var zeroVersion = "0"

func (fi *FnInst)DepName() string {
	dn := "swd-" + fi.fn.Cookie[:32]
	if fi.Build {
		dn += "-bld"
	}
	return dn
}

func (fi *FnInst)Str() string {
	if !fi.Build {
		return swy.SwyPodInstRun
	} else {
		return swy.SwyPodInstBld
	}
}

func (fi *FnInst)Replicas() int32 {
	if fi.Build {
		return 1
	} else {
		return int32(fi.fn.Size.Replicas)
	}
}

/*
 * We may have several instances of Fn running
 * Regular -- this one is up-n-running with the fn ready to run
 * Build -- this is a single replica deployment building the fn
 * Old -- this is Regular, but with the sources of previous version.
 *        In parallel to the Old one we may have one Build instance
 *        running building an Fn from new sources.
 * At some point in time the Old instance gets replaced with the
 * new Regular one.
 */
type FnInst struct {
	Build		bool
	fn		*FunctionDesc
}

func (fn *FunctionDesc) Inst() *FnInst {
	return &FnInst { Build: false, fn: fn }
}

func (fn *FunctionDesc) InstBuild() *FnInst {
	return &FnInst { Build: true, fn: fn }
}

func getFunctionDesc(tennant string, p_add *swyapi.FunctionAdd) *FunctionDesc {
	fn := &FunctionDesc {
		SwoId: SwoId {
			Tennant: tennant,
			Project: p_add.Project,
			Name:	 p_add.FuncName,
		},
		Event:		FnEventDesc {
			Source:		p_add.Event.Source,
			CronTab:	p_add.Event.CronTab,
			MwareId:	p_add.Event.MwareId,
			MQueue:		p_add.Event.MQueue,
			S3Bucket:	p_add.Event.S3Bucket,
		},
		Src:		FnSrcDesc {
			Type:		p_add.Sources.Type,
			Repo:		p_add.Sources.Repo,
			Code:		p_add.Sources.Code,
		},
		Size:		FnSizeDesc {
			Replicas:	1,
			Mem:		p_add.Size.Memory,
			Tmo:		p_add.Size.Timeout,
			Rate:		p_add.Size.Rate,
			Burst:		p_add.Size.Burst,
		},
		Code:		FnCodeDesc {
			Lang:		p_add.Code.Lang,
			Env:		p_add.Code.Env,
		},
		Mware:	p_add.Mware,
		UserData:	p_add.UserData,
	}

	fn.Cookie = fn.SwoId.Cookie()
	return fn
}

func validateProjectAndFuncName(params *swyapi.FunctionAdd) error {
	var err error

	err = swy.CheckName(params.Project, 64)
	if err == nil {
		err = swy.CheckName(params.FuncName, 50)
	}

	return err
}

func addFunction(ctx context.Context, conf *YAMLConf, tennant string, params *swyapi.FunctionAdd) *swyapi.GateErr {
	var err, erc error
	var fn *FunctionDesc
	var fi *FnInst

	err = validateProjectAndFuncName(params)
	if err != nil {
		goto out
	}

	if !RtLangEnabled(params.Code.Lang) {
		err = errors.New("Unsupported language")
		goto out
	}

	fn = getFunctionDesc(tennant, params)

	ctxlog(ctx).Debugf("function/add %s (cookie %s)", fn.SwoId.Str(), fn.Cookie[:32])

	fn.State = swy.DBFuncStateIni
	err = dbFuncAdd(fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't add function %s: %s", fn.SwoId.Str(), err.Error())
		return GateErrD(err)
	}

	err = eventSetup(ctx, conf, fn, true)
	if err != nil {
		goto out_clean_func
	}

	err = getSources(ctx, fn)
	if err != nil {
		goto out_clean_evt
	}

	if RtBuilding(&fn.Code) {
		fn.State = swy.DBFuncStateBld
		fi = fn.InstBuild()
	} else {
		fn.State = swy.DBFuncStateStr
		fi = fn.Inst()
	}

	err = dbFuncUpdateAdded(fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update added %s: %s", fn.SwoId.Str(), err.Error())
		err = errors.New("DB error")
		goto out_clean_repo
	}

	err = swk8sRun(ctx, conf, fn, fi)
	if err != nil {
		goto out_clean_repo
	}

	logSaveEvent(fn, "registered", "")
	return nil

out_clean_repo:
	erc = cleanRepo(fn)
	if erc != nil {
		goto stalled
	}
out_clean_evt:
	erc = eventSetup(ctx, conf, fn, false)
	if erc != nil {
		goto stalled
	}
out_clean_func:
	erc = dbFuncRemove(fn)
	if erc != nil {
		goto stalled
	}
out:
	return GateErrE(swy.GateGenErr, err)

stalled:
	dbFuncSetState(ctx, fn, swy.DBFuncStateStl)
	goto out
}

func swyFixSize(sz *swyapi.FunctionSize, conf *YAMLConf) error {
	if sz.Timeout == 0 {
		sz.Timeout = conf.Runtime.Timeout.Def * 1000
	} else if sz.Timeout > conf.Runtime.Timeout.Max * 1000 {
		return errors.New("Too big timeout")
	}

	if sz.Memory == 0 {
		sz.Memory = conf.Runtime.Memory.Def
	} else if sz.Memory > conf.Runtime.Memory.Max ||
			sz.Memory < conf.Runtime.Memory.Min {
		return errors.New("Too small/big memory size")
	}

	return nil
}

func updateFunction(ctx context.Context, conf *YAMLConf, id *SwoId, params *swyapi.FunctionUpdate) *swyapi.GateErr {
	var err error
	var restart bool
	var rebuild bool
	var mfix, rlfix bool

	update := make(bson.M)

	fn, err := dbFuncFindStates(id, []int{swy.DBFuncStateRdy, swy.DBFuncStateStl})
	if err != nil {
		goto out
	}

	// FIXME -- lock other requests :\

	if params.Code != "" {
		ctxlog(ctx).Debugf("Will update sources for %s", fn.SwoId.Str())
		err = updateSources(ctx, fn, params)
		if err != nil {
			goto out
		}

		update["src.version"] = fn.Src.Version
		rebuild = RtBuilding(&fn.Code)
		restart = true
	}

	if params.Size != nil {
		err = swyFixSize(params.Size, conf)
		if err != nil {
			goto out
		}

		if fn.Size.Tmo != params.Size.Timeout {
			ctxlog(ctx).Debugf("Will update tmo for %s", fn.SwoId.Str())
			fn.Size.Tmo = params.Size.Timeout
			update["size.timeout"] = params.Size.Timeout
		}

		if fn.Size.Mem != params.Size.Memory {
			ctxlog(ctx).Debugf("Will update mem for %s", fn.SwoId.Str())
			fn.Size.Mem = params.Size.Memory
			update["size.mem"] = params.Size.Memory
			mfix = true
		}

		if params.Size.Rate != 0 && (params.Size.Rate != fn.Size.Rate ||
						params.Size.Burst != fn.Size.Burst) {
			ctxlog(ctx).Debugf("Will update ratelimit for %s", fn.SwoId.Str())
			fn.Size.Burst = params.Size.Burst
			fn.Size.Rate = params.Size.Rate
			update["size.rate"] = params.Size.Rate
			update["size.burst"] = params.Size.Burst
			rlfix = true
		}

		restart = true
	}

	if params.Mware != nil {
		fn.Mware = *params.Mware
		update["mware"] = fn.Mware
		restart = true
	}

	if params.UserData != "" {
		fn.UserData = params.UserData
		update["userdata"] = fn.UserData
	}

	if len(update) == 0 {
		ctxlog(ctx).Debugf("Nothing to update for %s", fn.SwoId.Str())
		goto out
	}

	if rebuild {
		if fn.State == swy.DBFuncStateRdy {
			fn.State = swy.DBFuncStateUpd
		} else {
			fn.State = swy.DBFuncStateBld
		}
	}

	update["state"] = fn.State

	err = dbFuncUpdatePulled(fn, update)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update pulled %s: %s", fn.Name, err.Error())
		err = errors.New("DB error")
		goto out
	}

	if rlfix || mfix {
		fdm := memdGetFn(fn)
		if mfix {
			fdm.mem = fn.Size.Mem
		}

		if rlfix {
			if fn.Size.Rate != 0 {
				if fdm.crl != nil {
					/* Update */
					fdm.crl.Update(fn.Size.Burst, fn.Size.Rate)
				} else {
					/* Create */
					fdm.crl = xratelimit.MakeRL(fn.Size.Burst, fn.Size.Rate)
				}
			} else {
				/* Remove */
				fdm.crl = nil
			}
		}
	}

	if restart {
		if rebuild {
			ctxlog(ctx).Debugf("Starting build dep")
			err = swk8sRun(ctx, conf, fn, fn.InstBuild())
		} else {
			ctxlog(ctx).Debugf("Updating deploy")
			err = swk8sUpdate(ctx, conf, fn)
		}
	}

	if err != nil {
		/* FIXME -- stalled? */
		goto out
	}

	logSaveEvent(fn, "updated", fmt.Sprintf("to: %s", fn.Src.Version))
out:
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	return nil
}

func removeFunction(ctx context.Context, conf *YAMLConf, id *SwoId) *swyapi.GateErr {
	var err error

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(id, swy.DBFuncStateTrm, []int{
			swy.DBFuncStateRdy, swy.DBFuncStateStl, swy.DBFuncStateDea})
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate function %s: %s", id.Name, err.Error())
		return GateErrM(swy.GateGenErr, "Cannot terminate fn")
	}

	ctxlog(ctx).Debugf("Forget function %s", fn.SwoId.Str())

	if !fn.OneShot && (fn.State != swy.DBFuncStateDea) {
		err = swk8sRemove(ctx, conf, fn, fn.Inst())
		if err != nil {
			ctxlog(ctx).Errorf("remove deploy error: %s", err.Error())
			goto stalled
		}
	}


	err = eventSetup(ctx, conf, fn, false)
	if err != nil {
		goto stalled
	}

	err = statsDrop(fn)
	if err != nil {
		goto stalled
	}

	err = logRemove(fn)
	if err != nil {
		ctxlog(ctx).Errorf("logs %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto stalled
	}

	err = cleanRepo(fn)
	if err != nil {
		goto stalled
	}

	memdGone(fn)

	err = dbFuncRemove(fn)
	if err != nil {
		goto stalled
	}

	ctxlog(ctx).Debugf("Removed function %s", fn.SwoId.Str())
	return nil

stalled:
	dbFuncSetState(ctx, fn, swy.DBFuncStateStl)
	return GateErrE(swy.GateGenErr, err)
}

func waitFunctionVersion(ctx context.Context, fn *FunctionDesc, version string, tmo time.Duration) (error, bool) {
	var err error
	var timeout bool

	w := xwait.Prepare(fn.Cookie)

	for {
		ctxlog(ctx).Debugf("Check %s for %s", fn.SwoId.Str(), version)
		vers, err := dbBalancerRSListVersions(fn)
		if err != nil {
			break
		}

		ctxlog(ctx).Debugf("Check %s for %s vs %v", fn.SwoId.Str(), version, vers)
		if checkVersion(ctx, fn, version, vers) {
			break
		}

		ctxlog(ctx).Debugf("Wait %s %s (%v)", fn.SwoId.Str(), fn.Cookie, tmo)
		if w.Wait(&tmo) {
			ctxlog(ctx).Debugf(" `- Timeout %s", fn.SwoId.Str())
			timeout = true
			break
		}
	}

	w.Done()

	return err, timeout
}

func fnWaiterKick(cookie string) {
	glog.Debugf("FnWaiter kick %s", cookie)
	xwait.Event(cookie)
}

func notifyPodTmo(ctx context.Context, cookie, inst string) {
	fn, err := dbFuncFindByCookie(cookie)
	if err != nil {
		ctxlog(ctx).Errorf("POD timeout %s.%s error: %s", cookie, inst, err.Error())
		return
	}

	var fi *FnInst
	if inst == swy.SwyPodInstRun {
		fi = fn.Inst()
	} else {
		fi = fn.InstBuild()
	}

	logSaveEvent(fn, "POD", "Start timeout")
	swk8sRemove(ctx, &conf, fn, fi)
	dbFuncSetState(ctx, fn, swy.DBFuncStateStl)
}

func notifyPodUp(ctx context.Context, pod *k8sPod) {
	fn, err := dbFuncFind(&pod.SwoId)
	if err != nil {
		goto out
	}

	logSaveEvent(fn, "POD", fmt.Sprintf("state: %s", fnStates[fn.State]))
	if pod.Instance == swy.SwyPodInstBld {
		err = buildFunction(ctx, fn)
		if err != nil {
			goto out
		}
	} else {
		dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
		if fn.OneShot {
			runFunctionOnce(ctx, fn)
		}
	}

	return

out:
	ctxlog(ctx).Errorf("POD update notify: %s", err.Error())
}

func deactivateFunction(ctx context.Context, conf *YAMLConf, id *SwoId) *swyapi.GateErr {
	var err error

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	err = dbFuncSetStateCond(id, swy.DBFuncStateDea, []int{swy.DBFuncStateRdy})
	if err != nil {
		ctxlog(ctx).Errorf("Can't deactivate function %s: %s", id.Name, err.Error())
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	err = swk8sRemove(ctx, conf, fn, fn.Inst())
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove deployment: %s", err.Error())
		dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	return nil
}

func activateFunction(ctx context.Context, conf *YAMLConf, id *SwoId) *swyapi.GateErr {
	var err error

	fn, err := dbFuncFindStates(id, []int{swy.DBFuncStateDea})
	if err != nil {
		return GateErrD(err)
	}

	dbFuncSetState(ctx, fn, swy.DBFuncStateStr)

	err = swk8sRun(ctx, conf, fn, fn.Inst())
	if err != nil {
		dbFuncSetState(ctx, fn, swy.DBFuncStateDea)
		ctxlog(ctx).Errorf("Can't start deployment: %s", err.Error())
		return GateErrM(swy.GateGenErr, "Cannot activate FN")
	}

	return nil
}

func setFunctionState(ctx context.Context, conf *YAMLConf, id *SwoId, st *swyapi.FunctionState) *swyapi.GateErr {
	switch st.State {
	case fnStates[swy.DBFuncStateDea]:
		return deactivateFunction(ctx, conf, id)
	case fnStates[swy.DBFuncStateRdy]:
		return activateFunction(ctx, conf, id)
	}

	return GateErrM(swy.GateWrongType, "Cannot set this state")
}
