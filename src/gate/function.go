package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
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
	swy.DBFuncStateDea: "deactivated",
	swy.DBFuncStateTrm: "terminating",
	swy.DBFuncStateStl: "stalled",
	swy.DBFuncStateNo:  "dead",
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

	swage		*swyapi.FunctionSwage	`bson:"-"` // This doesn't get to the database (should it?)
}

type FnSizeDesc struct {
	Replicas	int		`bson:"replicas"`
	Mem		uint64		`bson:"mem"`
	Tmo		uint64		`bson:"timeout"`
	Burst		uint		`bson:"burst"`
	Rate		uint		`bson:"rate"`
}

func (fn *FunctionDesc)DepName() string {
	return "swd-" + fn.Cookie[:32]
}

type FunctionDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`

	SwoId				`bson:",inline"`
	Labels		[]string	`bson:"labels,omitempty"`
	Cookie		string		`bson:"cookie"`		// Some "unique" identifier
	State		int		`bson:"state"`		// Function state
	Mware		[]string	`bson:"mware"`
	S3Buckets	[]string	`bson:"s3buckets"`
	Code		FnCodeDesc	`bson:"code"`
	Src		FnSrcDesc	`bson:"src"`
	Size		FnSizeDesc	`bson:"size"`
	AuthCtx		string		`bson:"authctx,omitempty"`
	UserData	string		`bson:"userdata,omitempty"`
	URL		bool		`bson:"url"`
}

func (fn *FunctionDesc)isURL() bool {
	return fn.URL
}

func (fn *FunctionDesc)isOneShot() bool {
	return false
}

var zeroVersion = "0"

func (fn *FunctionDesc)getURL() string {
	return conf.Daemon.Addr + "/call/" + fn.Cookie
}

func (fn *FunctionDesc)getURLEvt() *swyapi.FunctionEvent {
	return &swyapi.FunctionEvent {
		Id:	URLEventID,
		Name:	"Inalienable API",
		Source:	"url",
		URL:	fn.getURL(),
	}
}

func (fn *FunctionDesc)toMInfo(ctx context.Context) *swyapi.FunctionMdat {
	var fid swyapi.FunctionMdat
	fdm := memdGetCond(fn.Cookie)
	if fdm != nil {
		if fdm.crl != nil {
			fid.RL = fdm.crl.If()
		}

		fid.BR = []uint { uint(fdm.bd.rover[0]), uint(fdm.bd.rover[1]), uint(fdm.bd.goal) }
	}
	return &fid
}

func (fn *FunctionDesc)toInfo(ctx context.Context, details bool, periods int) (*swyapi.FunctionInfo, *swyapi.GateErr) {
	fi := &swyapi.FunctionInfo {
		Id:		fn.ObjID.Hex(),
		Name:		fn.SwoId.Name,
		Project:	fn.SwoId.Project,
		Labels:		fn.Labels,
		State:          fnStates[fn.State],
	}

	if details {
		var err error
		var cerr *swyapi.GateErr

		if fn.isURL() {
			fi.URL = fn.getURL()
		}

		fi.Stats, cerr = getFunctionStats(ctx, fn, periods)
		if err != nil {
			return nil, cerr
		}

		fi.RdyVersions, err = dbBalancerRSListVersions(ctx, fn.Cookie)
		if err != nil {
			return nil, GateErrD(err)
		}

		fi.Version = fn.Src.Version
		fi.AuthCtx = fn.AuthCtx
		fi.UserData = fn.UserData
		fi.Code = swyapi.FunctionCode{
			Lang:		fn.Code.Lang,
			Env:		fn.Code.Env,
		}
		fi.Size = swyapi.FunctionSize {
			Memory:		fn.Size.Mem,
			Timeout:	fn.Size.Tmo,
			Rate:		fn.Size.Rate,
			Burst:		fn.Size.Burst,
		}
	}

	return fi, nil
}

func getFunctionDesc(id *SwoId, p_add *swyapi.FunctionAdd) *FunctionDesc {
	fn := &FunctionDesc {
		SwoId: *id,
		Src:		FnSrcDesc {
			Type:		p_add.Sources.Type,
			Repo:		p_add.Sources.Repo,
			Code:		p_add.Sources.Code,
			swage:		p_add.Sources.Swage,
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
		Mware:		p_add.Mware,
		S3Buckets:	p_add.S3Buckets,
		AuthCtx:	p_add.AuthCtx,
		UserData:	p_add.UserData,
		URL:		p_add.Url,
	}

	fn.Cookie = fn.SwoId.Cookie()
	return fn
}

func validateFuncName(params *swyapi.FunctionAdd) error {
	return swy.CheckName(params.Name, 50)
}

func checkCount(ctx context.Context, id *SwoId) error {
	tmd, err := tendatGet(ctx, id.Tennant)
	if err != nil {
		return err
	}

	if tmd.fnlim != 0 {
		nr, err := dbFuncCountProj(ctx, id)
		if err != nil {
			return err
		}
		if uint(nr) > tmd.fnlim {
			return errors.New("Too many functions in project")
		}
	}

	return nil
}

func (fn *FunctionDesc)Add(ctx context.Context, conf *YAMLConf) (string, *swyapi.GateErr) {
	var err, erc error
	var build bool
	var bAddr string

	ctxlog(ctx).Debugf("function/add %s (cookie %s)", fn.SwoId.Str(), fn.Cookie[:32])

	fn.ObjID = bson.NewObjectId()
	fn.State = swy.DBFuncStateIni
	err = dbInsert(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't add function %s: %s", fn.SwoId.Str(), err.Error())
		return "", GateErrD(err)
	}

	err = checkCount(ctx, &fn.SwoId)
	if err != nil {
		goto out_clean_func
	}

	gateFunctions.Inc()

	err = getSources(ctx, fn)
	if err != nil {
		goto out_clean_func
	}

	fn.State = swy.DBFuncStateStr

	err = dbFuncUpdateAdded(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update added %s: %s", fn.SwoId.Str(), err.Error())
		err = errors.New("DB error")
		goto out_clean_repo
	}

	build, bAddr = RtNeedToBuild(&fn.Code)
	if build {
		go func() {
			ctx, done := mkContext("::build")
			defer done(ctx)

			err = buildFunction(ctx, conf, bAddr, fn)
			if err != nil {
				goto bstalled
			}

			err = swk8sRun(ctx, conf, fn)
			if err != nil {
				goto bstalled
			}

			return

		bstalled:
			dbFuncSetState(ctx, fn, swy.DBFuncStateStl)
		}()
	} else {
		err = swk8sRun(ctx, conf, fn)
		if err != nil {
			goto out_clean_repo
		}
	}

	logSaveEvent(ctx, fn.Cookie, "registered")
	return fn.ObjID.Hex(), nil

out_clean_repo:
	erc = cleanRepo(ctx, fn)
	if erc != nil {
		goto stalled
	}
out_clean_func:
	erc = dbRemoveId(ctx, &FunctionDesc{}, fn.ObjID)
	if erc != nil {
		goto stalled
	}

	gateFunctions.Dec()
out:
	return "", GateErrE(swy.GateGenErr, err)

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

	if isLite() && sz.Timeout > 3000 {
		sz.Timeout = 3000 /* Max 3 seconds */
	}

	if sz.Memory == 0 {
		sz.Memory = conf.Runtime.Memory.Def
	} else if sz.Memory > conf.Runtime.Memory.Max ||
			sz.Memory < conf.Runtime.Memory.Min {
		return errors.New("Too small/big memory size")
	}

	return nil
}

func (fn *FunctionDesc)setUserData(ctx context.Context, ud string) error {
	err := dbFuncUpdateOne(ctx, fn, bson.M{"userdata": ud})
	if err == nil {
		fn.UserData = ud
	}
	return err
}

func (fn *FunctionDesc)setAuthCtx(ctx context.Context, ac string) error {
	var nac *AuthCtx
	var err error

	if ac != "" {
		nac, err = authCtxGet(ctx, fn.SwoId, ac)
		if err != nil {
			return err
		}
	}

	err = dbFuncUpdateOne(ctx, fn, bson.M{"authctx": ac})
	if err == nil {
		fn.AuthCtx = ac
		fdm := memdGetCond(fn.Cookie)
		if fdm != nil {
			fdm.ac = nac
		}
	}

	return err
}

func (fn *FunctionDesc)setSize(ctx context.Context, sz *swyapi.FunctionSize) error {
	update := make(bson.M)
	restart := false
	mfix := false
	rlfix := false

	if fn.Size.Tmo != sz.Timeout {
		ctxlog(ctx).Debugf("Will update tmo for %s", fn.SwoId.Str())
		fn.Size.Tmo = sz.Timeout
		update["size.timeout"] = sz.Timeout
		restart = true
	}

	if fn.Size.Mem != sz.Memory {
		ctxlog(ctx).Debugf("Will update mem for %s", fn.SwoId.Str())
		fn.Size.Mem = sz.Memory
		update["size.mem"] = sz.Memory
		mfix = true
		restart = true
	}

	if sz.Rate != fn.Size.Rate || sz.Burst != fn.Size.Burst {
		ctxlog(ctx).Debugf("Will update ratelimit for %s", fn.SwoId.Str())
		fn.Size.Burst = sz.Burst
		fn.Size.Rate = sz.Rate
		update["size.rate"] = sz.Rate
		update["size.burst"] = sz.Burst
		rlfix = true
	}

	err := dbFuncUpdateOne(ctx, fn, update)
	if err != nil {
		return err
	}

	if rlfix || mfix {
		fdm := memdGetCond(fn.Cookie)
		if fdm == nil {
			goto skip
		}

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
	skip:
		;
	}

	if restart && fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}

	return nil
}

func (fn *FunctionDesc)addMware(ctx context.Context, mw *MwareDesc) error {
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "mware": bson.M{"$ne": mw.SwoId.Name}},
				bson.M{"$push": bson.M{"mware":mw.SwoId.Name}})
	if err != nil {
		if dbNF(err) {
			return fmt.Errorf("Mware %s already there", mw.SwoId.Name)
		} else {
			return err
		}
	}

	fn.Mware = append(fn.Mware, mw.SwoId.Name)
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)delMware(ctx context.Context, mw *MwareDesc) error {
	found := -1
	for i, mwn := range fn.Mware {
		if mwn == mw.SwoId.Name {
			found = i
			break
		}
	}

	if found == -1 {
		return errors.New("Mware not attached")
	}

	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID}, bson.M{"$pull": bson.M{"mware":fn.Mware[found]}})
	if err != nil {
		return err
	}

	fn.Mware = append(fn.Mware[:found], fn.Mware[found+1:]...)
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)addS3Bucket(ctx context.Context, b string) error {
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "s3buckets": bson.M{"$ne": b}},
				bson.M{"$push": bson.M{"s3buckets":b}})
	if err != nil {
		if dbNF(err) {
			return fmt.Errorf("Bucket already there")
		} else {
			return err
		}
	}

	fn.S3Buckets = append(fn.S3Buckets, b)
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)delS3Bucket(ctx context.Context, bn string) error {
	found := -1
	for i, b := range fn.S3Buckets {
		if b == bn {
			found = i
			break
		}
	}

	if found == -1 {
		return errors.New("Bucket not attached")
	}

	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID}, bson.M{"$pull": bson.M{"s3buckets":bn}})
	if err != nil {
		return err
	}

	fn.S3Buckets = append(fn.S3Buckets[:found], fn.S3Buckets[found+1:]...)
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)getSources() (*swyapi.FunctionSources, *swyapi.GateErr) {
	codeFile := fnCodeLatestPath(&conf, fn) + "/" + RtDefaultScriptName(&fn.Code)
	fnCode, err := ioutil.ReadFile(codeFile)
	if err != nil {
		return nil, GateErrC(swy.GateFsError)
	}

	return &swyapi.FunctionSources {
		Type: "code",
		Code: base64.StdEncoding.EncodeToString(fnCode),
	}, nil

}

func (fn *FunctionDesc)updateSources(ctx context.Context, src *swyapi.FunctionSources) *swyapi.GateErr {
	var err error

	update := make(bson.M)
	olds := fn.State
	oldver := fn.Src.Version

	if olds != swy.DBFuncStateRdy && olds != swy.DBFuncStateStl {
		return GateErrM(swy.GateGenErr, "Function should be running or stalled")
	}

	ctxlog(ctx).Debugf("Will update sources for %s", fn.SwoId.Str())
	err = updateSources(ctx, fn, src)
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	err = tryBuildFunction(ctx, &conf, fn)
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	update["src.version"] = fn.Src.Version
	if olds == swy.DBFuncStateStl {
		fn.State = swy.DBFuncStateStr
		update["state"] = fn.State
	}

	err = dbFuncUpdateOne(ctx, fn, update)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update pulled %s: %s", fn.Name, err.Error())
		return GateErrD(err)
	}

	if olds == swy.DBFuncStateRdy {
		ctxlog(ctx).Debugf("Updating deploy")
		swk8sUpdate(ctx, &conf, fn)
	} else {
		ctxlog(ctx).Debugf("Starting deploy")
		err = swk8sRun(ctx, &conf, fn)
		if err != nil {
			dbFuncSetState(ctx, fn, swy.DBFuncStateStl)
			return GateErrE(swy.GateGenErr, err)
		}
	}

	GCOldSources(ctx, fn, oldver)
	logSaveEvent(ctx, fn.Cookie, fmt.Sprintf("updated to: %s", fn.Src.Version))
	return nil
}

func removeFunctionId(ctx context.Context, conf *YAMLConf, id *SwoId) *swyapi.GateErr {
	fn, err := dbFuncFind(ctx, id)
	if err != nil {
		return GateErrD(err)
	}

	return fn.Remove(ctx, conf)
}

func (fn *FunctionDesc)Remove(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	var err error

	switch fn.State {
	case swy.DBFuncStateStr:
	case swy.DBFuncStateRdy:
	case swy.DBFuncStateStl:
	case swy.DBFuncStateDea:
	case swy.DBFuncStateTrm:
		;
	default:
		ctxlog(ctx).Errorf("Can't terminate %s function %s", fnStates[fn.State], fn.SwoId.Str())
		return GateErrM(swy.GateGenErr, "Cannot terminate fn")
	}

	ctxlog(ctx).Debugf("Forget function %s", fn.SwoId.Str())
	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(ctx, &fn.SwoId, swy.DBFuncStateTrm, fn.State)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate function %s: %s", fn.SwoId.Str(), err.Error())
		return GateErrM(swy.GateGenErr, "Cannot terminate fn")
	}

	if !fn.isOneShot() && (fn.State != swy.DBFuncStateDea) {
		ctxlog(ctx).Debugf("`- delete deploy")
		err = swk8sRemove(ctx, conf, fn)
		if err != nil {
			ctxlog(ctx).Errorf("remove deploy error: %s", err.Error())
			goto later
		}
	}

	ctxlog(ctx).Debugf("`- setdown events")
	err = clearAllEvents(ctx, fn)
	if err != nil {
		goto later
	}

	ctxlog(ctx).Debugf("`- drop stats")
	err = statsDrop(ctx, fn)
	if err != nil {
		goto later
	}

	ctxlog(ctx).Debugf("`- remove logs")
	err = logRemove(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("logs %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	ctxlog(ctx).Debugf("`- clean sources")
	err = cleanRepo(ctx, fn)
	if err != nil {
		goto later
	}

	ctxlog(ctx).Debugf("`- gone fdmd")
	memdGone(fn)

	ctxlog(ctx).Debugf("`- and ...")
	err = dbRemoveId(ctx, &FunctionDesc{}, fn.ObjID)
	if err != nil {
		goto later
	}

	gateFunctions.Dec()
	ctxlog(ctx).Debugf("Removed function %s!", fn.SwoId.Str())
	return nil

later:
	return GateErrE(swy.GateGenErr, err)
}

func waitFunctionVersion(ctx context.Context, fn *FunctionDesc, version string, tmo time.Duration) (error, bool) {
	var err error
	var timeout bool

	w := xwait.Prepare(fn.Cookie)

	for {
		ctxlog(ctx).Debugf("Check %s for %s", fn.SwoId.Str(), version)
		vers, err := dbBalancerRSListVersions(ctx, fn.Cookie)
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
	xwait.Event(cookie)
}

func notifyPodUp(ctx context.Context, pod *k8sPod) {
	fn, err := dbFuncFind(ctx, &pod.SwoId)
	if err != nil {
		goto out
	}

	if fn.State != swy.DBFuncStateRdy {
		dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
		if fn.isOneShot() {
			runFunctionOnce(ctx, fn)
		}
	}

	return

out:
	ctxlog(ctx).Errorf("POD update notify: %s", err.Error())
}

func deactivateFunction(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) *swyapi.GateErr {
	var err error

	err = dbFuncSetStateCond(ctx, &fn.SwoId, swy.DBFuncStateDea, swy.DBFuncStateRdy)
	if err != nil {
		ctxlog(ctx).Errorf("Can't deactivate function %s: %s", fn.SwoId.Name, err.Error())
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	err = swk8sRemove(ctx, conf, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove deployment: %s", err.Error())
		dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	return nil
}

func activateFunction(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) *swyapi.GateErr {
	var err error

	if fn.State != swy.DBFuncStateDea {
		return GateErrM(swy.GateGenErr, "Function is not deactivated")
	}

	dbFuncSetState(ctx, fn, swy.DBFuncStateStr)

	err = swk8sRun(ctx, conf, fn)
	if err != nil {
		dbFuncSetState(ctx, fn, swy.DBFuncStateDea)
		ctxlog(ctx).Errorf("Can't start deployment: %s", err.Error())
		return GateErrM(swy.GateGenErr, "Cannot activate FN")
	}

	return nil
}

func (fn *FunctionDesc)setState(ctx context.Context, conf *YAMLConf, st string) *swyapi.GateErr {
	switch st {
	case fnStates[swy.DBFuncStateDea]:
		return deactivateFunction(ctx, conf, fn)
	case fnStates[swy.DBFuncStateRdy]:
		return activateFunction(ctx, conf, fn)
	}

	return GateErrM(swy.GateNotAvail, "Cannot set this state")
}
