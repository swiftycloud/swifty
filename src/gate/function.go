/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"encoding/base64"
	"errors"
	"strings"
	"net/url"
	"net/http"
	"fmt"
	"time"
	"context"
	"gopkg.in/mgo.v2/bson"

	"swifty/apis"
	"swifty/common/xratelimit"
	"swifty/common/xwait"
	"swifty/common/xrest"
)

const (
	DBFuncStateIni	int = 1		// Initializing for add -> Bld/Str
	DBFuncStateStr	int = 2		// Starting -> Rdy
	DBFuncStateRdy	int = 3		// Ready

	DBFuncStateTrm	int = 6		// Terminating
	DBFuncStateStl	int = 7		// Stalled
	DBFuncStateDea	int = 8		// Deactivated

	DBFuncStateNo	int = -1	// Doesn't exists :)
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
	DBFuncStateIni: "initializing",
	DBFuncStateStr: "starting",
	DBFuncStateRdy: "ready",
	DBFuncStateDea: "deactivated",
	DBFuncStateTrm: "terminating",
	DBFuncStateStl: "stalled",
	DBFuncStateNo:  "dead",
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
	Mem		uint		`bson:"mem"`
	Tmo		uint		`bson:"timeout"`
	Burst		uint		`bson:"burst"`
	Rate		uint		`bson:"rate"`
}

func (fn *FunctionDesc)k8sId() string {
	return fn.Cookie[:32]
}

func (fn *FunctionDesc)DepName() string {
	return "swd-" + fn.k8sId()
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
	fid.Cookie = fn.Cookie

	if gctx(ctx).Admin() {
		pcs := podsFindAll(ctx, fn.Cookie)
		if pcs != nil {
			for _, pc := range pcs {
				fid.Hosts = append(fid.Hosts, pc.Host)
				fid.IPs = append(fid.IPs, pc.Addr)
			}
		}

		fid.Dep = fn.DepName()
	}

	return &fid
}

func (_ Functions)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return nil, cerr
	}

	return &fn, nil
}

func (_ Functions)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	fname := q.Get("name")

	var fn FunctionDesc

	if fname != "" {
		err := dbFind(ctx, cookieReq(ctx, project, fname), &fn)
		if err != nil {
			return GateErrD(err)
		}

		return cb(ctx, &fn)
	}

	pref := q.Get("prefix")

	iter := dbIterAll(ctx, listReq(ctx, project, q["label"]), &fn)
	defer iter.Close()

	for iter.Next(&fn) {
		if pref != "" && !strings.HasPrefix(fn.SwoId.Name, pref) {
			continue /* XXX -- tune request? */
		}

		cerr := cb(ctx, &fn)
		if cerr != nil {
			return cerr
		}
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (_ Functions)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.FunctionAdd)
	id := ctxSwoId(ctx, params.Project, params.Name)
	return getFunctionDesc(id, params)
}

func (fn *FunctionDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	periods := 0
	if q != nil {
		periods = reqPeriods(q)
		if periods < 0 {
			return nil, GateErrC(swyapi.GateBadRequest)
		}
	}

	return fn.toInfo(ctx, details, periods)
}

func (fn *FunctionDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	fu := upd.(*swyapi.FunctionUpdate)

	if fu.UserData != nil {
		err := fn.setUserData(ctx, *fu.UserData)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
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

func (fn *FunctionDesc)toInfo(ctx context.Context, details bool, periods int) (*swyapi.FunctionInfo, *xrest.ReqErr) {
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
		var cerr *xrest.ReqErr

		if _, err = urlEvFind(ctx, fn.Cookie); err == nil {
			fi.URL = fn.getURL()
		}

		fi.Stats, cerr = fn.getStats(ctx, periods)
		if cerr != nil {
			return nil, cerr
		}

		fi.RdyVersions = podsListVersions(ctx, fn.Cookie)
		fi.AuthCtx = fn.AuthCtx
		fi.UserData = fn.UserData
		fi.Code = &swyapi.FunctionCode{
			Lang:		fn.Code.Lang,
			Env:		fn.Code.Env,
		}
		fi.Size = &swyapi.FunctionSize {
			Memory:		fn.Size.Mem,
			Timeout:	fn.Size.Tmo,
			Rate:		fn.Size.Rate,
			Burst:		fn.Size.Burst,
		}
	}

	return fi, nil
}

func guessLang(p *swyapi.FunctionAdd) bool {
	var fn string

	if p.Sources == nil {
		return false
	}

	switch {
	case p.Sources.Repo != "":
		fn = p.Sources.Repo
	case p.Sources.URL != "":
		fn = p.Sources.URL
	default:
		return false
	}

	lng := rtLangDetect(fn)
	if lng == "" {
		return false
	}

	p.Code.Lang = lng
	return true
}

func getFunctionDesc(id *SwoId, p_add *swyapi.FunctionAdd) (*FunctionDesc, *xrest.ReqErr) {
	if p_add.Name == "" {
		return nil, GateErrM(swyapi.GateBadRequest, "No function name")
	}
	if p_add.Code.Lang == "" {
		if !guessLang(p_add) {
			return nil, GateErrM(swyapi.GateBadRequest, "No language specified")
		}
	}
	if !id.NameOK() {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad function name")
	}

	err := fnFixSize(&p_add.Size)
	if err != nil {
		return nil, GateErrE(swyapi.GateBadRequest, err)
	}

	if !rtLangEnabled(p_add.Code.Lang) {
		return nil, GateErrM(swyapi.GateBadRequest, "Unsupported language")
	}

	for _, env := range(p_add.Code.Env) {
		if strings.HasPrefix(env, "SWD_") {
			return nil, GateErrM(swyapi.GateBadRequest, "Environment var cannot start with SWD_")
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
		Accounts:	p_add.Accounts,
		AuthCtx:	p_add.AuthCtx,
		UserData:	p_add.UserData,
	}

	fn.Cookie = fn.SwoId.Cookie()
	return fn, nil
}

func checkFnCount(ctx context.Context, id *SwoId) error {
	tmd, err := tendatGet(ctx)
	if err != nil {
		return err
	}

	if tmd.fnlim != 0 {
		nr, err := dbFuncCountTen(ctx)
		if err != nil {
			return err
		}
		if uint(nr) > tmd.fnlim {
			return errors.New("Too many functions in project")
		}
	}

	return nil
}

func (fn *FunctionDesc)Add(ctx context.Context, p interface{}) *xrest.ReqErr {
	var err, erc error
	var cerr *xrest.ReqErr

	src := p.(*swyapi.FunctionAdd).Sources
	if src == nil {
		/* Lang must have been set, otherwise getFunctionDesc
		 * wouldn't guess one and fail
		 */
		src = &swyapi.FunctionSources {
			Repo: demoRep.ObjID.Hex() + "/" + conf.DemoRepo.EmptySources + "/" + rtScriptName(&fn.Code, ""),
		}
	}

	fn.ObjID = bson.NewObjectId()
	fn.State = DBFuncStateIni
	err = dbInsert(ctx, fn)
	if err != nil {
		cerr = GateErrD(err)
		goto out
	}

	err = checkFnCount(ctx, &fn.SwoId)
	if err != nil {
		cerr = GateErrC(swyapi.GateLimitHit)
		goto out_clean_func
	}

	gateFunctions.Inc()

	err = putSources(ctx, fn, src)
	if err != nil {
		goto out_clean_func
	}

	fn.State = DBFuncStateStr

	err = dbUpdatePart(ctx, fn, bson.M{
			"src": &fn.Src, "state": fn.State,
		})
	if err != nil {
		ctxlog(ctx).Errorf("Can't update added %s: %s", fn.SwoId.Str(), err.Error())
		cerr = GateErrD(err)
		goto out_clean_repo
	}

	go func() {
		ctx, done := mkContext("::start")
		defer done(ctx)
		gctx(ctx).tpush(fn.SwoId.Tennant)
		err := fn.Start(ctx)
		if err != nil {
			ctxlog(ctx).Errorf("Cannot start fn %s: %s", fn.SwoId.Str(), err.Error())
			fn.ToState(ctx, DBFuncStateStl, -1)
		}
	}()

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
	if cerr == nil {
		cerr = GateErrE(swyapi.GateGenErr, err)
	}
	return cerr

stalled:
	fn.ToState(ctx, DBFuncStateStl, -1)
	goto out
}

func (fn *FunctionDesc)Start(ctx context.Context) error {
	var err error

	build, rh := rtNeedToBuild(&fn.Code)
	if build {

		err = buildFunction(ctx, rh, fn, "")
		if err != nil {
			return err
		}
	}

	err = k8sRun(ctx, &conf, fn)
	if err != nil {
		return err
	}

	return nil
}

func fnFixSize(sz *swyapi.FunctionSize) error {
	if sz.Timeout == 0 {
		sz.Timeout = uint(conf.Runtime.Timeout.Def * 1000)
	} else if sz.Timeout > uint(conf.Runtime.Timeout.Max * 1000) {
		return errors.New("Too big timeout")
	}

	if isLite() && sz.Timeout > 3000 {
		sz.Timeout = 3000 /* Max 3 seconds */
	}

	if sz.Memory == 0 {
		sz.Memory = uint(conf.Runtime.Memory.Def)
	} else if sz.Memory > uint(conf.Runtime.Memory.Max) ||
			sz.Memory < uint(conf.Runtime.Memory.Min) {
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

func (fn *FunctionDesc)setAuthCtx(ctx context.Context, ac string) *xrest.ReqErr {
	var nac *AuthCtx
	var err error

	if ac != "" {
		nac, err = authCtxGet(ctx, fn.SwoId, ac)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	}

	err = dbUpdatePart(ctx, fn, bson.M{"authctx": ac})
	if err != nil {
		return GateErrD(err)
	}

	fn.AuthCtx = ac
	fdm := memdGetCond(fn.Cookie)
	if fdm != nil {
		fdm.ac = nac
	}

	return nil
}

func (fn *FunctionDesc)setEnv(ctx context.Context, env []string) *xrest.ReqErr {
	fn.Code.Env = env
	err := dbUpdatePart(ctx, fn, bson.M{"code.env": env})
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	k8sUpdate(ctx, &conf, fn)
	return nil
}

func (fn *FunctionDesc)setSize(ctx context.Context, sz *swyapi.FunctionSize) *xrest.ReqErr {
	update := make(bson.M)
	restart := false
	mfix := false
	rlfix := false

	err := fnFixSize(sz)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	if fn.Size.Tmo != sz.Timeout {
		fn.Size.Tmo = sz.Timeout
		update["size.timeout"] = sz.Timeout
		restart = true
	}

	if fn.Size.Mem != sz.Memory {
		fn.Size.Mem = sz.Memory
		update["size.mem"] = sz.Memory
		mfix = true
		restart = true
	}

	if sz.Rate != fn.Size.Rate || sz.Burst != fn.Size.Burst {
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
		return GateErrD(err)
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

	if restart && fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	}

	return nil
}

func (fn *FunctionDesc)addMware(ctx context.Context, mw *MwareDesc) *xrest.ReqErr {
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "mware": bson.M{"$ne": mw.SwoId.Name}},
				bson.M{"$push": bson.M{"mware":mw.SwoId.Name}})
	if err != nil {
		if dbNF(err) {
			return GateErrM(swyapi.GateDuplicate, "Mware %s already there")
		} else {
			return GateErrD(err)
		}
	}

	fn.Mware = append(fn.Mware, mw.SwoId.Name)
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)delMware(ctx context.Context, mw *MwareDesc) *xrest.ReqErr {
	found := -1
	for i, mwn := range fn.Mware {
		if mwn == mw.SwoId.Name {
			found = i
			break
		}
	}

	if found == -1 {
		return GateErrM(swyapi.GateNotFound, "Mware not attached")
	}

	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID}, bson.M{"$pull": bson.M{"mware":fn.Mware[found]}})
	if err != nil {
		return GateErrD(err)
	}

	fn.Mware = append(fn.Mware[:found], fn.Mware[found+1:]...)
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)addAccount(ctx context.Context, ad *AccDesc) *xrest.ReqErr {
	aid := ad.ID()
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "accounts": bson.M{"$ne": aid}},
				bson.M{"$push": bson.M{"accounts":aid}})
	if err != nil {
		if dbNF(err) {
			return GateErrM(swyapi.GateBadRequest, "Account already attached")
		} else {
			return GateErrD(err)
		}
	}

	fn.Accounts = append(fn.Accounts, aid)
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)delAccount(ctx context.Context, acc *AccDesc) *xrest.ReqErr {
	aid := acc.ID()
	err := dbFuncUpdate(ctx, bson.M{"_id": fn.ObjID, "accounts": aid},
				bson.M{"$pull": bson.M{"accounts": aid}})
	if err != nil {
		if dbNF(err) {
			return GateErrM(swyapi.GateBadRequest, "Account not attached")
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
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
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
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
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
	if fn.State == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	}
	return nil
}

func (fn *FunctionDesc)getSources(ctx context.Context) (*swyapi.FunctionSources, *xrest.ReqErr) {
	fnCode, err := getSources(ctx, fn)
	if err != nil {
		return nil, GateErrC(swyapi.GateFsError)
	}

	fs := &swyapi.FunctionSources {
		Code: base64.StdEncoding.EncodeToString(fnCode),
	}

	if fn.Src.Repo != "" {
		fs.Sync = true
		fs.Repo = fn.Src.Repo + "/" + fn.Src.File
	}

	return fs, nil

}

func (fn *FunctionDesc)updateSources(ctx context.Context, src *swyapi.FunctionSources) *xrest.ReqErr {
	var err error

	update := make(bson.M)
	olds := fn.State
	oldver := fn.Src.Version

	if olds != DBFuncStateRdy && olds != DBFuncStateStl {
		return GateErrM(swyapi.GateGenErr, "Function should be running or stalled")
	}

	err = updateSources(ctx, fn, src)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	err = tryBuildFunction(ctx, fn, "")
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	update["src"] = &fn.Src
	if olds == DBFuncStateStl {
		fn.State = DBFuncStateStr
		update["state"] = fn.State
	}

	err = dbUpdatePart(ctx, fn, update)
	if err != nil {
		ctxlog(ctx).Errorf("Can't update pulled %s: %s", fn.Name, err.Error())
		return GateErrD(err)
	}

	if olds == DBFuncStateRdy {
		k8sUpdate(ctx, &conf, fn)
	} else {
		err = k8sRun(ctx, &conf, fn)
		if err != nil {
			fn.ToState(ctx, DBFuncStateStl, -1)
			return GateErrE(swyapi.GateGenErr, err)
		}
	}

	GCOldSources(ctx, fn, oldver)
	logSaveEvent(ctx, fn.Cookie, fmt.Sprintf("updated to: %s", fn.Src.Version))
	return nil
}

func removeFunctionId(ctx context.Context, id *SwoId) *xrest.ReqErr {
	var fn FunctionDesc

	err := dbFind(ctx, id.dbReq(), &fn)
	if err != nil {
		return GateErrD(err)
	}

	return fn.Del(ctx)
}

func (fn *FunctionDesc)Del(ctx context.Context) *xrest.ReqErr {
	var err error
	var dea bool

	switch fn.State {
	case DBFuncStateDea:
		dea = true
	case DBFuncStateStr:
	case DBFuncStateRdy:
	case DBFuncStateStl:
	case DBFuncStateTrm:
		;
	default:
		ctxlog(ctx).Errorf("Can't terminate %s function %s", fnStates[fn.State], fn.SwoId.Str())
		return GateErrM(swyapi.GateGenErr, "Cannot terminate fn")
	}

	ctxlog(ctx).Debugf("Forget function %s", fn.SwoId.Str())
	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = fn.ToState(ctx, DBFuncStateTrm, fn.State)
	if err != nil {
		ctxlog(ctx).Errorf("Can't terminate function %s: %s", fn.SwoId.Str(), err.Error())
		return GateErrM(swyapi.GateGenErr, "Cannot terminate fn")
	}

	if !dea {
		err = k8sRemove(ctx, &conf, fn)
		if err != nil {
			ctxlog(ctx).Errorf("remove deploy error: %s", err.Error())
			goto later
		}
	}

	err = clearAllEvents(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("events %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	err = statsDrop(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("stats %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	err = logRemove(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("logs %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	err = removeSources(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("sources %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	memdGone(fn)

	err = dbRemove(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("db %s remove error: %s", fn.SwoId.Str(), err.Error())
		goto later
	}

	gateFunctions.Dec()
	ctxlog(ctx).Debugf("Removed function %s!", fn.SwoId.Str())
	return nil

later:
	return GateErrE(swyapi.GateGenErr, err)
}

func waitFunctionVersion(ctx context.Context, fn *FunctionDesc, version string, tmo time.Duration) (error, bool) {
	var err error
	var timeout bool

	w := xwait.Prepare(fn.Cookie)

	for {
		var vers []string
		var ok bool

		vers = podsListVersions(ctx, fn.Cookie)
		ok, err = checkVersion(ctx, fn, version, vers)
		if ok || err != nil {
			break
		}

		if w.Wait(&tmo) {
			ctxlog(ctx).Debugf("Timeout waiting FN version %s", fn.SwoId.Str())
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

	if fn.State != DBFuncStateRdy {
		fn.ToState(ctx, DBFuncStateRdy, -1)
	}

	fnWaiterKick(fn.Cookie)
	return

out:
	ctxlog(ctx).Errorf("POD update notify: %s", err.Error())
}

func notifyPodDown(ctx context.Context, pod *k8sPod) {
	fnWaiterKick(pod.FnId)
}

func deactivateFunction(ctx context.Context, fn *FunctionDesc) *xrest.ReqErr {
	var err error

	if fn.State != DBFuncStateRdy {
		return GateErrM(swyapi.GateGenErr, "Function is not ready")
	}

	err = fn.ToState(ctx, DBFuncStateDea, fn.State)
	if err != nil {
		return GateErrM(swyapi.GateGenErr, "Cannot deactivate function")
	}

	err = k8sRemove(ctx, &conf, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove deployment: %s", err.Error())
		fn.ToState(ctx, DBFuncStateRdy, -1)
		return GateErrM(swyapi.GateGenErr, "Cannot deactivate function")
	}

	return nil
}

func activateFunction(ctx context.Context, fn *FunctionDesc) *xrest.ReqErr {
	var err error

	if fn.State != DBFuncStateDea {
		return GateErrM(swyapi.GateGenErr, "Function is not deactivated")
	}

	err = fn.ToState(ctx, DBFuncStateStr, DBFuncStateDea)
	if err != nil {
		return GateErrM(swyapi.GateGenErr, "Cannot activate function")
	}

	err = k8sRun(ctx, &conf, fn)
	if err != nil {
		fn.ToState(ctx, DBFuncStateDea, -1)
		ctxlog(ctx).Errorf("Can't start deployment: %s", err.Error())
		return GateErrM(swyapi.GateGenErr, "Cannot activate FN")
	}

	return nil
}

func (fn *FunctionDesc)setState(ctx context.Context, st string) *xrest.ReqErr {
	switch st {
	case fnStates[DBFuncStateDea]:
		return deactivateFunction(ctx, fn)
	case fnStates[DBFuncStateRdy]:
		return activateFunction(ctx, fn)
	}

	return GateErrM(swyapi.GateNotAvail, "Cannot set this state")
}

type FnEnvProp struct { }

func (_ *FnEnvProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	fn := o.(*FunctionDesc)
	return fn.Code.Env, nil
}

func (_ *FnEnvProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	fn := o.(*FunctionDesc)
	return fn.setEnv(ctx, *p.(*[]string))
}

type FnSzProp struct { }

func (_ *FnSzProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	fn := o.(*FunctionDesc)
	return &swyapi.FunctionSize{
		Memory:		fn.Size.Mem,
		Timeout:	uint(fn.Size.Tmo),
		Rate:		fn.Size.Rate,
		Burst:		fn.Size.Burst,
	}, nil
}

func (_ *FnSzProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	return o.(*FunctionDesc).setSize(ctx, p.(*swyapi.FunctionSize))
}

type FnSrcProp struct { }

func (_ *FnSrcProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	return o.(*FunctionDesc).getSources(ctx)
}

func (_ *FnSrcProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	return o.(*FunctionDesc).updateSources(ctx, p.(*swyapi.FunctionSources))
}

type FnAuthProp struct { }

func (_ *FnAuthProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	return o.(*FunctionDesc).AuthCtx, nil
}

func (_ *FnAuthProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	return o.(*FunctionDesc).setAuthCtx(ctx, *p.(*string))
}

type FnStatsProp struct { }

func (_ *FnStatsProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	periods := reqPeriods(q)
	if periods < 0 {
		return nil, GateErrC(swyapi.GateBadRequest)
	}

	stats, cerr := o.(*FunctionDesc).getStats(ctx, periods)
	if cerr != nil {
		return nil, cerr
	}

	return &swyapi.FunctionStatsResp{ Stats: stats }, nil
}

func (_ *FnStatsProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}

type FnLogsProp struct { }

func getSince(q url.Values) (*time.Time, *xrest.ReqErr) {
	s := q.Get("last")
	if s == "" {
		return nil, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, GateErrE(swyapi.GateBadRequest, err)
	}

	t := time.Now().Add(-d)
	return &t, nil
}

func (_ *FnLogsProp)Info(ctx context.Context, o xrest.Obj, q url.Values) (interface{}, *xrest.ReqErr) {
	fn := o.(*FunctionDesc)
	return handleLogsFor(ctx, fn.SwoId.Cookie(), q)
}

func (_ *FnLogsProp)Upd(ctx context.Context, o xrest.Obj, p interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}
