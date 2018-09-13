package main

import (
	"encoding/base64"
	"errors"
	"strings"
	"net/url"
	"fmt"
	"time"
	"context"
	"gopkg.in/mgo.v2/bson"

	"../apis"
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

func (c *FnCodeDesc)image() string {
	return rtLangImage(c.Lang)
}

type FnSrcDesc struct {
	Version		string		`bson:"version"` // Growing number, each deploy update (code push) bumps it
	Repo		string		`bson:"repo"`
	File		string		`bson:"file"`
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
	Accounts	[]string	`bson:"accounts"`
	Code		FnCodeDesc	`bson:"code"`
	Src		FnSrcDesc	`bson:"src"`
	Size		FnSizeDesc	`bson:"size"`
	AuthCtx		string		`bson:"authctx,omitempty"`
	UserData	string		`bson:"userdata,omitempty"`
}

type Functions struct {}

func (fn *FunctionDesc)isOneShot() bool {
	return false
}

func (fn *FunctionDesc)ToState(ctx context.Context, st, from int) error {
	q := bson.M{}
	if from != -1 {
		q["state"] = from
	}

	err := dbUpdatePart2(ctx, fn, q, bson.M{"state": st})
	if err == nil {
		fn.State = st
	}

	return err
}

func listFunctions(ctx context.Context, project, name string, labels []string) ([]*FunctionDesc, *swyapi.GateErr) {
	var fns []*FunctionDesc

	if name == "" {
		err := dbFindAll(ctx, listReq(ctx, project, labels), &fns)
		if err != nil {
			return nil, GateErrD(err)
		}
		glog.Debugf("Found %d fns", len(fns))
	} else {
		var fn FunctionDesc

		err := dbFind(ctx, cookieReq(ctx, project, name), &fn)
		if err != nil {
			return nil, GateErrD(err)
		}
		fns = append(fns, &fn)
	}

	return fns, nil
}

var zeroVersion = "0"

func (fn *FunctionDesc)getURL() string {
	return getURL(URLFunction, fn.Cookie)
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

func (_ Functions)iterate(ctx context.Context, q url.Values, cb func(context.Context, Obj) *swyapi.GateErr) *swyapi.GateErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	fname := q.Get("name")
	periods := reqPeriods(q)
	if periods < 0 {
		return GateErrC(swy.GateBadRequest)
	}

	fns, cerr := listFunctions(ctx, project, fname, q["label"])
	if cerr != nil {
		return cerr
	}

	for _, fn := range fns {
		cerr = cb(ctx, fn)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (_ Functions)create(ctx context.Context, p interface{}) (Obj, *swyapi.GateErr) {
	params := p.(*swyapi.FunctionAdd)
	if params.Name == "" {
		return nil, GateErrM(swy.GateBadRequest, "No function name")
	}
	if params.Code.Lang == "" {
		return nil, GateErrM(swy.GateBadRequest, "No language specified")
	}

	id := ctxSwoId(ctx, params.Project, params.Name)
	return getFunctionDesc(id, params)
}

func (fn *FunctionDesc)add(ctx context.Context, p interface{}) *swyapi.GateErr {
	params := p.(*swyapi.FunctionAdd)
	return fn.Add(ctx, &params.Sources)
}

func (fn *FunctionDesc)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	periods := 0
	if q != nil {
		periods = reqPeriods(q)
		if periods < 0 {
			return nil, GateErrC(swy.GateBadRequest)
		}
	}

	return fn.toInfo(ctx, details, periods)
}

func (fn *FunctionDesc)upd(ctx context.Context, upd interface{}) *swyapi.GateErr {
	fu := upd.(*swyapi.FunctionUpdate)

	if fu.UserData != nil {
		err := fn.setUserData(ctx, *fu.UserData)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
	}

	if fu.State != "" {
		cerr := fn.setState(ctx, fu.State)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (fn *FunctionDesc)del(ctx context.Context) *swyapi.GateErr {
	return fn.Remove(ctx)
}

func (fn *FunctionDesc)toInfo(ctx context.Context, details bool, periods int) (*swyapi.FunctionInfo, *swyapi.GateErr) {
	fi := &swyapi.FunctionInfo {
		Id:		fn.ObjID.Hex(),
		Name:		fn.SwoId.Name,
		Project:	fn.SwoId.Project,
		Labels:		fn.Labels,
		State:          fnStates[fn.State],
		Version:	fn.Src.Version,
	}

	if details {
		var err error
		var cerr *swyapi.GateErr

		if _, err = urlEvFind(ctx, fn.Cookie); err == nil {
			fi.URL = fn.getURL()
		}

		fi.Stats, cerr = fn.getStats(ctx, periods)
		if cerr != nil {
			return nil, cerr
		}

		fi.RdyVersions, err = dbBalancerListVersions(ctx, fn.Cookie)
		if err != nil {
			return nil, GateErrD(err)
		}

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

func getFunctionDesc(id *SwoId, p_add *swyapi.FunctionAdd) (*FunctionDesc, *swyapi.GateErr) {
	err := fnFixSize(&p_add.Size)
	if err != nil {
		return nil, GateErrE(swy.GateBadRequest, err)
	}

	if !rtLangEnabled(p_add.Code.Lang) {
		return nil, GateErrM(swy.GateBadRequest, "Unsupported language")
	}

	for _, env := range(p_add.Code.Env) {
		if strings.HasPrefix(env, "SWD_") {
			return nil, GateErrM(swy.GateBadRequest, "Environment var cannot start with SWD_")
		}
	}

	fn := &FunctionDesc {
		SwoId: *id,
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
	}

	fn.Cookie = fn.SwoId.Cookie()
	return fn, nil
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

func (fn *FunctionDesc)Add(ctx context.Context, src *swyapi.FunctionSources) *swyapi.GateErr {
	var err, erc error
	var build bool
	var bAddr string

	ctxlog(ctx).Debugf("function/add %s (cookie %s)", fn.SwoId.Str(), fn.Cookie[:32])

	fn.ObjID = bson.NewObjectId()
	fn.State = swy.DBFuncStateIni
	err = dbInsert(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't add function %s: %s", fn.SwoId.Str(), err.Error())
		return GateErrD(err)
	}

	err = checkCount(ctx, &fn.SwoId)
	if err != nil {
		goto out_clean_func
	}

	gateFunctions.Inc()

	err = putSources(ctx, fn, src)
	if err != nil {
		goto out_clean_func
	}

	fn.State = swy.DBFuncStateStr

	err = dbUpdatePart(ctx, fn, bson.M{
			"src": &fn.Src, "state": fn.State,
		})
	if err != nil {
		ctxlog(ctx).Errorf("Can't update added %s: %s", fn.SwoId.Str(), err.Error())
		err = errors.New("DB error")
		goto out_clean_repo
	}

	build, bAddr = rtNeedToBuild(&fn.Code)
	if build {
		go func() {
			ctx, done := mkContext("::build")
			defer done(ctx)

			err = buildFunction(ctx, bAddr, fn, "")
			if err != nil {
				goto bstalled
			}

			err = swk8sRun(ctx, &conf, fn)
			if err != nil {
				goto bstalled
			}

			return

		bstalled:
			fn.ToState(ctx, swy.DBFuncStateStl, -1)
		}()
	} else {
		err = swk8sRun(ctx, &conf, fn)
		if err != nil {
			goto out_clean_repo
		}
	}

	logSaveEvent(ctx, fn.Cookie, "registered")
	return nil

out_clean_repo:
	erc = removeSources(ctx, fn)
	if erc != nil {
		goto stalled
	}
out_clean_func:
	erc = dbRemove(ctx, fn)
	if erc != nil {
		goto stalled
	}

	gateFunctions.Dec()
out:
	return GateErrE(swy.GateGenErr, err)

stalled:
	fn.ToState(ctx, swy.DBFuncStateStl, -1)
	goto out
}

func fnFixSize(sz *swyapi.FunctionSize) error {
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
	err := dbUpdatePart(ctx, fn, bson.M{"userdata": ud})
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

	err = dbUpdatePart(ctx, fn, bson.M{"authctx": ac})
	if err == nil {
		fn.AuthCtx = ac
		fdm := memdGetCond(fn.Cookie)
		if fdm != nil {
			fdm.ac = nac
		}
	}

	return err
}

func (fn *FunctionDesc)setEnv(ctx context.Context, env []string) error {
	fn.Code.Env = env
	err := dbUpdatePart(ctx, fn, bson.M{"code.env": env})
	if err != nil {
		return err
	}

	swk8sUpdate(ctx, &conf, fn)
	return nil
}

func (fn *FunctionDesc)setSize(ctx context.Context, sz *swyapi.FunctionSize) error {
	update := make(bson.M)
	restart := false
	mfix := false
	rlfix := false

	err := fnFixSize(sz)
	if err != nil {
		return err
	}

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

	if len(update) == 0 {
		return nil
	}

	err = dbUpdatePart(ctx, fn, update)
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

func (fn *FunctionDesc)listMware(ctx context.Context) []*swyapi.MwareInfo {
	ret := []*swyapi.MwareInfo{}
	for _, mwn := range fn.Mware {
		id := fn.SwoId
		id.Name = mwn

		var mw MwareDesc
		var mi *swyapi.MwareInfo

		err := dbFind(ctx, id.dbReq(), &mw)

		if err == nil {
			mi = mw.toFnInfo(ctx)
		} else {
			mi = &swyapi.MwareInfo{Name: mwn}
		}
		ret = append(ret, mi)
	}

	return ret
}

func (fn *FunctionDesc)addAccount(ctx context.Context, ad *AccDesc) *swyapi.GateErr {
	aid := ad.ObjID.Hex()
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "accounts": bson.M{"$ne": aid}},
				bson.M{"$push": bson.M{"accounts":aid}})
	if err != nil {
		if dbNF(err) {
			return GateErrM(swy.GateBadRequest, "Account already attached")
		} else {
			return GateErrD(err)
		}
	}

	fn.Accounts = append(fn.Accounts, aid)
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)delAccountId(ctx context.Context, aid string) *swyapi.GateErr {
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "accounts": aid},
				bson.M{"$pull": bson.M{"accounts": aid}})
	if err != nil {
		if dbNF(err) {
			return GateErrM(swy.GateBadRequest, "Account not attached")
		} else {
			return GateErrD(err)
		}
	}

	for i, _ := range fn.Accounts {
		if fn.Accounts[i] == aid {
			fn.Accounts = append(fn.Accounts[:i], fn.Accounts[i+1:]...)
			break
		}
	}
	if fn.State == swy.DBFuncStateRdy {
		swk8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)listAccounts(ctx context.Context) *[]map[string]string {
	ret := []map[string]string{}
	for _, aid := range fn.Accounts {
		var ac AccDesc
		var ai map[string]string

		cerr := objFindId(ctx, aid, &ac, nil)
		if cerr == nil {
			ai = ac.toInfo(ctx, false)
		} else {
			ai = map[string]string{"id": aid }
		}

		ret = append(ret, ai)
	}

	return &ret
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

func (fn *FunctionDesc)getSources(ctx context.Context) (*swyapi.FunctionSources, *swyapi.GateErr) {
	fnCode, err := getSources(ctx, fn)
	if err != nil {
		return nil, GateErrC(swy.GateFsError)
	}

	fs := &swyapi.FunctionSources {
		Type: "code",
		Code: base64.StdEncoding.EncodeToString(fnCode),
	}

	if fn.Src.Repo != "" {
		fs.Sync = true
		fs.Repo = fn.Src.Repo + "/" + fn.Src.File
	}

	return fs, nil

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

	ctxlog(ctx).Debugf("Try build sources")
	err = tryBuildFunction(ctx, fn, "")
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	update["src"] = &fn.Src
	if olds == swy.DBFuncStateStl {
		fn.State = swy.DBFuncStateStr
		update["state"] = fn.State
	}

	err = dbUpdatePart(ctx, fn, update)
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
			fn.ToState(ctx, swy.DBFuncStateStl, -1)
			return GateErrE(swy.GateGenErr, err)
		}
	}

	GCOldSources(ctx, fn, oldver)
	logSaveEvent(ctx, fn.Cookie, fmt.Sprintf("updated to: %s", fn.Src.Version))
	return nil
}

func removeFunctionId(ctx context.Context, id *SwoId) *swyapi.GateErr {
	var fn FunctionDesc

	err := dbFind(ctx, id.dbReq(), &fn)
	if err != nil {
		return GateErrD(err)
	}

	return fn.Remove(ctx)
}

func (fn *FunctionDesc)Remove(ctx context.Context) *swyapi.GateErr {
	var err error
	var dea bool

	switch fn.State {
	case swy.DBFuncStateDea:
		dea = true
	case swy.DBFuncStateStr:
	case swy.DBFuncStateRdy:
	case swy.DBFuncStateStl:
	case swy.DBFuncStateTrm:
		;
	default:
		ctxlog(ctx).Errorf("Can't terminate %s function %s", fnStates[fn.State], fn.SwoId.Str())
		return GateErrM(swy.GateGenErr, "Cannot terminate fn")
	}

	ctxlog(ctx).Debugf("Forget function %s", fn.SwoId.Str())
	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = fn.ToState(ctx, swy.DBFuncStateTrm, fn.State)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate function %s: %s", fn.SwoId.Str(), err.Error())
		return GateErrM(swy.GateGenErr, "Cannot terminate fn")
	}

	if !fn.isOneShot() && !dea {
		ctxlog(ctx).Debugf("`- delete deploy")
		err = swk8sRemove(ctx, &conf, fn)
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
	err = removeSources(ctx, fn)
	if err != nil {
		goto later
	}

	ctxlog(ctx).Debugf("`- gone fdmd")
	memdGone(fn)

	ctxlog(ctx).Debugf("`- and ...")
	err = dbRemove(ctx, fn)
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
		var vers []string
		var ok bool

		ctxlog(ctx).Debugf("Check %s for %s", fn.SwoId.Str(), version)
		vers, err = dbBalancerListVersions(ctx, fn.Cookie)
		if err != nil {
			break
		}

		ctxlog(ctx).Debugf("Check %s for %s vs %v", fn.SwoId.Str(), version, vers)
		ok, err = checkVersion(ctx, fn, version, vers)
		if ok || err != nil {
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
	var fn FunctionDesc

	err := dbFind(ctx, pod.SwoId.dbReq(), &fn)
	if err != nil {
		goto out
	}

	if fn.State != swy.DBFuncStateRdy {
		fn.ToState(ctx, swy.DBFuncStateRdy, -1)
		if fn.isOneShot() {
			runFunctionOnce(ctx, &fn)
		}
	}

	return

out:
	ctxlog(ctx).Errorf("POD update notify: %s", err.Error())
}

func deactivateFunction(ctx context.Context, fn *FunctionDesc) *swyapi.GateErr {
	var err error

	if fn.State != swy.DBFuncStateRdy {
		return GateErrM(swy.GateGenErr, "Function is not ready")
	}

	err = fn.ToState(ctx, swy.DBFuncStateDea, fn.State)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	err = swk8sRemove(ctx, &conf, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove deployment: %s", err.Error())
		fn.ToState(ctx, swy.DBFuncStateRdy, -1)
		return GateErrM(swy.GateGenErr, "Cannot deactivate function")
	}

	return nil
}

func activateFunction(ctx context.Context, fn *FunctionDesc) *swyapi.GateErr {
	var err error

	if fn.State != swy.DBFuncStateDea {
		return GateErrM(swy.GateGenErr, "Function is not deactivated")
	}

	err = fn.ToState(ctx, swy.DBFuncStateStr, swy.DBFuncStateDea)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Cannot activate function")
	}

	err = swk8sRun(ctx, &conf, fn)
	if err != nil {
		fn.ToState(ctx, swy.DBFuncStateDea, -1)
		ctxlog(ctx).Errorf("Can't start deployment: %s", err.Error())
		return GateErrM(swy.GateGenErr, "Cannot activate FN")
	}

	return nil
}

func (fn *FunctionDesc)setState(ctx context.Context, st string) *swyapi.GateErr {
	switch st {
	case fnStates[swy.DBFuncStateDea]:
		return deactivateFunction(ctx, fn)
	case fnStates[swy.DBFuncStateRdy]:
		return activateFunction(ctx, fn)
	}

	return GateErrM(swy.GateNotAvail, "Cannot set this state")
}

type FnEnvProp struct {
	fn *FunctionDesc
}

func (e *FnEnvProp)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	return e.fn.Code.Env, nil
}

func (e *FnEnvProp)upd(ctx context.Context, p interface{}) *swyapi.GateErr {
	err := e.fn.setEnv(ctx, *p.(*[]string))
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	return nil
}

func (e *FnEnvProp)del(context.Context) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }
func (e *FnEnvProp)add(context.Context, interface{}) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }

type FnSzProp struct {
	fn *FunctionDesc
}

func (s *FnSzProp)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	return &swyapi.FunctionSize{
		Memory:		s.fn.Size.Mem,
		Timeout:	s.fn.Size.Tmo,
		Rate:		s.fn.Size.Rate,
		Burst:		s.fn.Size.Burst,
	}, nil
}

func (s *FnSzProp)upd(ctx context.Context, p interface{}) *swyapi.GateErr {
	err := s.fn.setSize(ctx, p.(*swyapi.FunctionSize))
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	return nil
}

func (s *FnSzProp)del(context.Context) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }
func (s *FnSzProp)add(context.Context, interface{}) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }

type FnSrcProp struct {
	fn *FunctionDesc
}

func (s *FnSrcProp)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	return s.fn.getSources(ctx)
}

func (s *FnSrcProp)upd(ctx context.Context, p interface{}) *swyapi.GateErr {
	return s.fn.updateSources(ctx, p.(*swyapi.FunctionSources))
}

func (s *FnSrcProp)del(context.Context) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }
func (s *FnSrcProp)add(context.Context, interface{}) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }
