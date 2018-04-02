package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/json"
	"encoding/hex"
	"net/http"
	"errors"
	"flag"
	"strings"
	"context"
	"sync/atomic"
	"time"
	"fmt"
	"os"
	"io/ioutil"
	"encoding/base64"

	"../apis/apps"
	"../common"
	"../common/http"
	"../common/keystone"
	"../common/secrets"
)

var SwyModeDevel bool
var SwdProxyOK bool
var gateSecrets map[string]string
var gateSecPas []byte

const (
	SwyDefaultProject string		= "default"
	SwyPodStartTmo time.Duration		= 120 * time.Second
	SwyBodyArg string			= "_SWY_BODY_"
	SwyDepScaleupRelax time.Duration	= 16 * time.Second
	SwyDepScaledownStep time.Duration	= 8 * time.Second
	SwyTenantLimitsUpdPeriod time.Duration	= 120 * time.Second
)

var glog *zap.SugaredLogger

type YAMLConfSwd struct {
	Port		int			`yaml:"port"`
}

type YAMLConfSources struct {
	Share		string			`yaml:"share"`
	Clone		string			`yaml:"clone"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	Sources		YAMLConfSources		`yaml:"sources"`
	LogLevel	string			`yaml:"loglevel"`
	Prometheus	string			`yaml:"prometheus"`
	HTTPS		*swyhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
}

type YAMLConfRabbit struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*swy.XCreds
}

type YAMLConfMaria struct {
	Creds		string			`yaml:"creds"`
	QDB		string			`yaml:"quotdb"`
	c		*swy.XCreds
}

type YAMLConfMongo struct {
	Creds		string			`yaml:"creds"`
	c		*swy.XCreds
}

type YAMLConfPostgres struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*swy.XCreds
}

type YAMLConfS3 struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	Notify		string			`yaml:"notify"`
	c		*swy.XCreds
	cn		*swy.XCreds
}

type YAMLConfMw struct {
	SecKey		string			`yaml:"mwseckey"`
	Rabbit		YAMLConfRabbit		`yaml:"rabbit"`
	Maria		YAMLConfMaria		`yaml:"maria"`
	Mongo		YAMLConfMongo		`yaml:"mongo"`
	Postgres	YAMLConfPostgres	`yaml:"postgres"`
	S3		YAMLConfS3		`yaml:"s3"`
}

type YAMLConfRange struct {
	Min		uint64			`yaml:"min"`
	Max		uint64			`yaml:"max"`
	Def		uint64			`yaml:"def"`
}

type YAMLConfRt struct {
	Timeout		YAMLConfRange		`yaml:"timeout"`
	Memory		YAMLConfRange		`yaml:"memory"`
	MaxReplicas	uint32			`yaml:"max-replicas"`
}

type YAMLConf struct {
	DB		string			`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
	Mware		YAMLConfMw		`yaml:"middleware"`
	Runtime		YAMLConfRt		`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
}

var conf YAMLConf
var gatesrv *http.Server

var CORS_Headers = []string {
	"Content-Type",
	"Content-Length",
	"X-Relay-Tennant",
	"X-Subject-Token",
	"X-Auth-Token",
}

var CORS_Methods = []string {
	http.MethodPost,
	http.MethodGet,
}

type gateContext struct {
	context.Context
	Tenant	string
	ReqId	uint64
}

var reqIds uint64

func mkContext(parent context.Context, tenant string) context.Context {
	return &gateContext{parent, tenant, atomic.AddUint64(&reqIds, 1)}
}

func fromContext(ctx context.Context) *gateContext {
	return ctx.(*gateContext)
}

func ctxlog(ctx context.Context) *zap.SugaredLogger {
	if gctx, ok := ctx.(*gateContext); ok {
		return glog.With(zap.Int64("req", int64(gctx.ReqId)))
	}

	return glog
}

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest
	var td swyapi.UserToken

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	glog.Debugf("Trying to login user %s", params.UserName)

	token, td.Expires, err = swyks.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = conf.Daemon.Addr
	glog.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

	w.Header().Set("X-Subject-Token", token)
	err = swyhttp.MarshalAndWrite(w, &td)
	if err != nil {
		resp = http.StatusInternalServerError
		goto out
	}

	return

out:
	http.Error(w, err.Error(), resp)
}

func handleProjectDel(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var par swyapi.ProjectDel
	var fns []FunctionDesc
	var mws []MwareDesc
	var id *SwoId
	var ferr *swyapi.GateErr

	err := swyhttp.ReadAndUnmarshalReq(r, &par)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id = makeSwoId(fromContext(ctx).Tenant, par.Project, "")

	fns, err = dbFuncListProj(id)
	if err != nil {
		return GateErrD(err)
	}
	for _, fn := range fns {
		id.Name = fn.SwoId.Name
		xerr := removeFunction(ctx, &conf, id)
		if xerr != nil {
			ctxlog(ctx).Error("Funciton removal failed: %s", xerr.Message)
			ferr = GateErrM(xerr.Code, "Cannot remove " + id.Name + " function: " + xerr.Message)
		}
	}

	mws, err = dbMwareGetAll(id)
	if err != nil {
		return GateErrD(err)
	}

	for _, mw := range mws {
		id.Name = mw.SwoId.Name
		xerr := mwareRemove(ctx, &conf.Mware, id)
		if xerr != nil {
			ctxlog(ctx).Error("Mware removal failed: %s", xerr.Message)
			ferr = GateErrM(xerr.Code, "Cannot remove " + id.Name + " mware: " + xerr.Message)
		}
	}

	if ferr != nil {
		return ferr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleProjectList(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string

	projects := make(map[string]struct{})

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	ctxlog(ctx).Debugf("List projects for %s", fromContext(ctx).Tenant)
	fns, mws, err = dbProjectListAll(fromContext(ctx).Tenant)
	if err != nil {
		return GateErrD(err)
	}

	for _, v := range fns {
		projects[v] = struct{}{}
		result = append(result, swyapi.ProjectItem{ Project: v })
	}
	for _, v := range mws {
		_, ok := projects[v]
		if !ok {
			result = append(result, swyapi.ProjectItem{ Project: v})
		}
	}

	err = swyhttp.MarshalAndWrite(w, &result)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionAdd(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionAdd

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	if params.Project == "" {
		params.Project = SwyDefaultProject
	}

	err = swyFixSize(&params.Size, &conf)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	if params.FuncName == "" {
		return GateErrM(swy.GateBadRequest, "No function name")
	}
	if params.Code.Lang == "" {
		return GateErrM(swy.GateBadRequest, "No language specified")
	}

	err = validateProjectAndFuncName(&params)
	if err != nil {
		return GateErrM(swy.GateBadRequest, "Bad project/function name")
	}

	if !RtLangEnabled(params.Code.Lang) {
		return GateErrM(swy.GateBadRequest, "Unsupported language")
	}

	for _, env := range(params.Code.Env) {
		if strings.HasPrefix(env, "SWD_") {
			return GateErrM(swy.GateBadRequest, "Environment var cannot start with SWD_")
		}
	}

	cerr := addFunction(ctx, &conf, fromContext(ctx).Tenant, &params)
	if cerr != nil {
		return cerr

	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleFunctionWait(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var wi swyapi.FunctionWait
	var err error
	var tmo bool

	err = swyhttp.ReadAndUnmarshalReq(r, &wi)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, wi.Project, wi.FuncName)
	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	if wi.Version != "" {
		ctxlog(ctx).Debugf("function/wait %s -> version >= %s", id.Str(), wi.Version)
		err, tmo = waitFunctionVersion(ctx, fn, wi.Version,
				time.Duration(wi.Timeout) * time.Millisecond)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
	}

	if !tmo {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(swyhttp.StatusTimeoutOccurred) /* CloudFlare's timeout */
	}

	return nil
}

func handleFunctionState(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionState

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("function/state %s -> %s", id.Str(), params.State)

	cerr := setFunctionState(ctx, &conf, id, &params)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleFunctionUpdate(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionUpdate

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("function/update %s", id.Str())

	cerr := updateFunction(ctx, &conf, id, &params)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleFunctionRemove(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionRemove

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("function/remove %s", id.Str())

	cerr := removeFunction(ctx, &conf, id)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleFunctionCode(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionXID
	var codeFile string
	var fnCode []byte

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("Get FN code %s:%s", id.Str(), params.Version)

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	if params.Version == "" {
		params.Version = fn.Src.Version
	}

	if fn.Src.Type != "code" {
		return GateErrM(swy.GateNotAvail, "No single file for sources")
	}

	codeFile = fnCodeVersionPath(&conf, fn, params.Version) + "/" + RtDefaultScriptName(&fn.Code)
	fnCode, err = ioutil.ReadFile(codeFile)
	if err != nil {
		ctxlog(ctx).Errorf("Can't read file with code: %s", err.Error())
		return GateErrC(swy.GateFsError)
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionSources {
			Type: "code",
			Code: base64.StdEncoding.EncodeToString(fnCode),
		})
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleTenantStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.TenantStatsReq

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	ten := fromContext(ctx).Tenant
	ctxlog(ctx).Debugf("Get FN stats %s", ten)

	td, err := tendatGet(ten)
	if err != nil {
		return GateErrD(err)
	}

	var resp swyapi.TenantStatsResp
	prev := &td.stats

	if params.Periods > 0 {
		var atst []TenStats

		atst, err = dbTenStatsGetArch(ten, params.Periods)
		if err != nil {
			return GateErrD(err)
		}

		for i := 0; i < params.Periods && i < len(atst); i++ {
			cur := &atst[i]
			resp.Stats = append(resp.Stats, swyapi.TenantStats{
				Called:		prev.Called - cur.Called,
				GBS:		prev.GBS() - cur.GBS(),
				BytesOut:	prev.BytesOut - cur.BytesOut,
				Till:		prev.TillS(),
			})
			prev = cur
		}
	}

	resp.Stats = append(resp.Stats, swyapi.TenantStats{
		Called:		prev.Called,
		GBS:		prev.GBS(),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	err = swyhttp.MarshalAndWrite(w, resp)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionStatsReq
	var prev *FnStats

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("Get FN stats %s", id.Str())

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	prev, err = statsGet(fn)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Error getting stats")
	}

	var resp swyapi.FunctionStatsResp
	if params.Periods > 0 {
		var afst []FnStats

		afst, err = dbFnStatsGetArch(fn.Cookie, params.Periods)
		if err != nil {
			return GateErrD(err)
		}

		for i := 0; i < params.Periods && i < len(afst); i++ {
			cur := &afst[i]
			resp.Stats = append(resp.Stats, swyapi.FunctionStats{
				Called:		prev.Called - cur.Called,
				Timeouts:	prev.Timeouts - cur.Timeouts,
				Errors:		prev.Errors - cur.Errors,
				LastCall:	prev.LastCallS(),
				Time:		prev.RunTimeUsec() - cur.RunTimeUsec(),
				GBS:		prev.GBS() - cur.GBS(),
				BytesOut:	prev.BytesOut - cur.BytesOut,
				Till:		prev.TillS(),
			})
			prev = cur
		}
	}

	resp.Stats = append(resp.Stats, swyapi.FunctionStats{
		Called:		prev.Called,
		Timeouts:	prev.Timeouts,
		Errors:		prev.Errors,
		LastCall:	prev.LastCallS(),
		Time:		prev.RunTimeUsec(),
		GBS:		prev.GBS(),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	err = swyhttp.MarshalAndWrite(w, resp)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionInfo(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionID
	var fv []string
	var url = ""
	var stats *FnStats

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("Get FN Info %s", id.Str())

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}

	if (fn.URLCall) {
		url = conf.Daemon.Addr + "/call/" + fn.Cookie
	}

	stats, err = statsGet(fn)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Error getting stats")
	}

	fv, err = dbBalancerRSListVersions(fn.Cookie)
	if err != nil {
		return GateErrD(err)
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionInfo{
			State:          fnStates[fn.State],
			Mware:          fn.Mware,
			S3Buckets:	fn.S3Buckets,
			Version:        fn.Src.Version,
			RdyVersions:    fv,
			URL:		url,
			Code:		swyapi.FunctionCode{
				Lang:		fn.Code.Lang,
				Env:		fn.Code.Env,
			},
			Event:		swyapi.FunctionEvent{
				Source:		fn.Event.Source,
				CronTab:	fn.Event.CronTab,
				MwareId:	fn.Event.MwareId,
				MQueue:		fn.Event.MQueue,
				S3Bucket:	fn.Event.S3Bucket,
			},
			Stats:		swyapi.FunctionStats {
				Called:		stats.Called,
				Timeouts:	stats.Timeouts,
				Errors:		stats.Errors,
				LastCall:	stats.LastCallS(),
				Time:		stats.RunTimeUsec(),
				GBS:		stats.GBS(),
				BytesOut:	stats.BytesOut,
			},
			Size:		swyapi.FunctionSize {
				Memory:		fn.Size.Mem,
				Timeout:	fn.Size.Tmo,
				Rate:		fn.Size.Rate,
				Burst:		fn.Size.Burst,
			},
			UserData:	fn.UserData,
		})
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionLogs(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionID
	var resp []swyapi.FunctionLogEntry
	var logs []DBLogRec

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("Get logs for %s", fromContext(ctx).Tenant)

	logs, err = logGetFor(id)
	if err != nil {
		return GateErrD(err)
	}

	for _, loge := range logs {
		resp = append(resp, swyapi.FunctionLogEntry{
				Event:	loge.Event,
				Ts:	loge.Time.Format(time.UnixDate),
				Text:	loge.Text,
			})
	}

	err = swyhttp.MarshalAndWrite(w, resp)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func fnCallable(fn *FunctionDesc) bool {
	return fn.URLCall && (fn.State == swy.DBFuncStateRdy)
}

func makeArgMap(sopq *statsOpaque, r *http.Request) map[string]string {
	defer r.Body.Close()

	args := make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args[k] = v[0]
		sopq.argsSz += len(k) + len(v[0])
	}

	body, err := ioutil.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		args[SwyBodyArg] = string(body)
		sopq.bodySz = len(body)
	}

	return args
}

func ratelimited(fmd *FnMemData) bool {
	/* Per-function RL first, as it's ... more likely to fail */
	frl := fmd.crl
	if frl != nil && !frl.Get() {
		return true
	}

	trl := fmd.td.crl
	if trl != nil && !trl.Get() {
		if frl != nil {
			frl.Put()
		}
		return true
	}

	return false
}

func handleFunctionCall(w http.ResponseWriter, r *http.Request) {
	var arg_map map[string]string
	var res *swyapi.SwdFunctionRunResult
	var err error
	var code int
	var fmd *FnMemData
	var conn *podConn

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	sopq := statsStart()

	ctx := context.Background()
	fnId := mux.Vars(r)["fnid"]

	fmd, err = memdGet(fnId)
	if err != nil {
		code = http.StatusInternalServerError
		err = errors.New("Error getting function")
		goto out
	}

	if fmd == nil || !fmd.public {
		code = http.StatusServiceUnavailable
		err = errors.New("No such function")
		goto out
	}

	if ratelimited(fmd) {
		code = http.StatusTooManyRequests
		err = errors.New("Ratelimited")
		goto out
	}

	conn, err = balancerGetConnAny(ctx, fnId, fmd)
	if err != nil {
		code = http.StatusInternalServerError
		err = errors.New("DB error")
		goto out
	}


	arg_map = makeArgMap(sopq, r)
	res, err = doRunConn(ctx, conn, fnId, "call", arg_map)
	if err != nil {
		code = http.StatusInternalServerError
		goto out
	}

	if res.Code != 0 {
		code = res.Code
		err = errors.New(res.Return)
		goto out
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(res.Return))

	statsUpdate(fmd, sopq, res)

	return

out:
	http.Error(w, err.Error(), code)
}

func handleFunctionRun(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionRun
	var conn *podConn
	var res *swyapi.SwdFunctionRunResult

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.FuncName)
	ctxlog(ctx).Debugf("function/run %s", id.Str())

	fn, err := dbFuncFind(id)
	if err != nil {
		return GateErrD(err)
	}
	if fn.State != swy.DBFuncStateRdy {
		return GateErrM(swy.GateNotAvail, "Function not ready (yet)")
	}

	conn, errc := balancerGetConnExact(ctx, fn.Cookie, fn.Src.Version)
	if errc != nil {
		return errc
	}

	res, err = doRunConn(ctx, conn, fn.Cookie, "run", params.Args)
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	err = swyhttp.MarshalAndWrite(w, swyapi.FunctionRunResult{
		Code:		res.Code,
		Return:		res.Return,
		Stdout:		res.Stdout,
		Stderr:		res.Stderr,
	})
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionList(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var recs []FunctionDesc
	var result []swyapi.FunctionItem
	var params swyapi.FunctionList

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, "")
	recs, err = dbFuncListProj(id)
	if err != nil {
		return GateErrD(err)
	}

	for _, v := range recs {
		stats, err := statsGet(&v)
		if err != nil {
			return GateErrM(swy.GateGenErr, "Error getting stats")
		}

		result = append(result,
			swyapi.FunctionItem{
				FuncName:	v.Name,
				State:		fnStates[v.State],
				Timeout:	v.Size.Tmo,
				LastCall:	stats.LastCallS(),
		})
	}

	err = swyhttp.MarshalAndWrite(w, &result)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleMwareAdd(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.MwareAdd

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.ID)
	ctxlog(ctx).Debugf("mware/add: %s params %v", fromContext(ctx).Tenant, params)

	cerr := mwareSetup(ctx, &conf.Mware, id, params.Type)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleLanguages(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var ret []string

	for l, lh := range rt_handlers {
		if lh.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, l)
	}

	err := swyhttp.MarshalAndWrite(w, ret)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleMwareTypes(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var ret []string

	for mw, mt := range mwareHandlers {
		if mt.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, mw)
	}

	err := swyhttp.MarshalAndWrite(w, ret)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleMwareList(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var result []swyapi.MwareItem
	var params swyapi.MwareList
	var mwares []MwareDesc

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, "")
	ctxlog(ctx).Debugf("list mware for %s", fromContext(ctx).Tenant)

	mwares, err = dbMwareGetAll(id)
	if err != nil {
		return GateErrD(err)
	}

	for _, mware := range mwares {
		result = append(result,
			swyapi.MwareItem{
				ID:	   mware.Name,
				Type:	   mware.MwareType,
			})
	}

	err = swyhttp.MarshalAndWrite(w, &result)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleMwareRemove(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.MwareRemove

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.ID)
	ctxlog(ctx).Debugf("mware/remove: %s params %v", fromContext(ctx).Tenant, params)

	cerr := mwareRemove(ctx, &conf.Mware, id)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleMwareS3Access(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.MwareS3Access

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	creds, cerr := mwareGetS3Creds(ctx, &conf, &params)
	if cerr != nil {
		return cerr
	}

	err = swyhttp.MarshalAndWrite(w, creds)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleMwareInfo(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.MwareID
	var resp swyapi.MwareInfo

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id := makeSwoId(fromContext(ctx).Tenant, params.Project, params.ID)
	ctxlog(ctx).Debugf("mware/info: %s params %v", fromContext(ctx).Tenant, params)

	cerr := mwareInfo(&conf.Mware, id, &params, &resp)
	if cerr != nil {
		return cerr
	}

	err = swyhttp.MarshalAndWrite(w, &resp)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}
	return nil
}

func handleGenericReq(ctx context.Context, r *http.Request) (string, int, error) {
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return "", http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	td, code := swyks.KeystoneGetTokenData(conf.Keystone.Addr, token)
	if code != 0 {
		return "", code, fmt.Errorf("Keystone authentication error")
	}

	/*
	 * Setting X-Relay-Tennant means that it's an admin
	 * coming to modify the user's setup. In this case we
	 * need the swifty.admin role. Otherwise it's the
	 * swifty.owner guy that can only work on his tennant.
	 */

	var role string

	tennant := r.Header.Get("X-Relay-Tennant")
	if tennant == "" {
		role = swyks.SwyUserRole
		tennant = td.Project.Name
	} else {
		role = swyks.SwyAdminRole
	}

	if !swyks.KeystoneRoleHas(td, role) {
		return "", http.StatusForbidden, fmt.Errorf("Keystone authentication error")
	}

	return tennant, 0, nil
}

func genReqHandler(cb func(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		var cancel context.CancelFunc

		if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()

		tennant, code, err := handleGenericReq(ctx, r)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}

		ctx = mkContext(ctx, tennant)
		cerr := cb(ctx, w, r)
		if cerr != nil {
			ctxlog(ctx).Errorf("Error in callback: %s", cerr.Message)

			jdata, err := json.Marshal(cerr)
			if err != nil {
				ctxlog(ctx).Errorf("Can't marshal back gate error: %s", err.Error())
				jdata = []byte("")
			}

			http.Error(w, string(jdata), http.StatusBadRequest)
		}
	})
}

func setupMwareAddr(conf *YAMLConf) {
	conf.Mware.Maria.c = swy.ParseXCreds(conf.Mware.Maria.Creds)
	conf.Mware.Maria.c.Resolve()

	conf.Mware.Rabbit.c = swy.ParseXCreds(conf.Mware.Rabbit.Creds)
	conf.Mware.Rabbit.c.Resolve()

	conf.Mware.Mongo.c = swy.ParseXCreds(conf.Mware.Mongo.Creds)
	conf.Mware.Mongo.c.Resolve()

	conf.Mware.Postgres.c = swy.ParseXCreds(conf.Mware.Postgres.Creds)
	conf.Mware.Postgres.c.Resolve()

	conf.Mware.S3.c = swy.ParseXCreds(conf.Mware.S3.Creds)
	conf.Mware.S3.c.Resolve()

	conf.Mware.S3.cn = swy.ParseXCreds(conf.Mware.S3.Notify)
	conf.Mware.S3.cn.Resolve()
}

func setupLogger(conf *YAMLConf) {
	lvl := zap.WarnLevel

	if conf != nil {
		switch conf.Daemon.LogLevel {
		case "debug":
			lvl = zap.DebugLevel
			break
		case "info":
			lvl = zap.InfoLevel
			break
		case "warn":
			lvl = zap.WarnLevel
			break
		case "error":
			lvl = zap.ErrorLevel
			break
		}
	}

	zcfg := zap.Config {
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      true,
		DisableStacktrace:true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, _ := zcfg.Build()
	glog = logger.Sugar()
}

func main() {
	var config_path string
	var showVersion bool
	var err error

	flag.StringVar(&config_path,
			"conf",
				"/etc/swifty/conf/gate.yaml",
				"path to a config file")
	flag.BoolVar(&SwyModeDevel, "devel", false, "launch in development mode")
	flag.BoolVar(&SwdProxyOK, "proxy", false, "use wdog proxy")
	flag.BoolVar(&showVersion, "version", false, "show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("Version %s\n", Version)
		return
	}

	if _, err := os.Stat(config_path); err == nil {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
		setupMwareAddr(&conf)
	} else {
		setupLogger(nil)
		glog.Errorf("Provide config path")
		return
	}

	gateSecrets, err = swysec.ReadSecrets("gate")
	if err != nil {
		glog.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	gateSecPas, err = hex.DecodeString(gateSecrets[conf.Mware.SecKey])
	if err != nil || len(gateSecPas) < 16 {
		glog.Errorf("Secrets pass should be decodable and at least 16 bytes long")
		return
	}

	glog.Debugf("PROXY: %v", SwdProxyOK)

	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin).Methods("POST", "OPTIONS")
	r.Handle("/v1/stats",			genReqHandler(handleTenantStats)).Methods("POST", "OPTIONS")
	r.Handle("/v1/project/list",		genReqHandler(handleProjectList)).Methods("POST", "OPTIONS")
	r.Handle("/v1/project/del",		genReqHandler(handleProjectDel)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/add",		genReqHandler(handleFunctionAdd)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/update",		genReqHandler(handleFunctionUpdate)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/remove",		genReqHandler(handleFunctionRemove)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/run",		genReqHandler(handleFunctionRun)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/list",		genReqHandler(handleFunctionList)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/info",		genReqHandler(handleFunctionInfo)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/stats",		genReqHandler(handleFunctionStats)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/code",		genReqHandler(handleFunctionCode)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/logs",		genReqHandler(handleFunctionLogs)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/state",		genReqHandler(handleFunctionState)).Methods("POST", "OPTIONS")
	r.Handle("/v1/function/wait",		genReqHandler(handleFunctionWait)).Methods("POST", "OPTIONS")
	r.Handle("/v1/mware/add",		genReqHandler(handleMwareAdd)).Methods("POST", "OPTIONS")
	r.Handle("/v1/mware/info",		genReqHandler(handleMwareInfo)).Methods("POST", "OPTIONS")
	r.Handle("/v1/mware/list",		genReqHandler(handleMwareList)).Methods("POST", "OPTIONS")
	r.Handle("/v1/mware/remove",		genReqHandler(handleMwareRemove)).Methods("POST", "OPTIONS")
	r.Handle("/v1/mware/access/s3",		genReqHandler(handleMwareS3Access)).Methods("POST", "OPTIONS")

	r.Handle("/v1/info/langs",		genReqHandler(handleLanguages)).Methods("POST", "OPTIONS")
	r.Handle("/v1/info/mwares",		genReqHandler(handleMwareTypes)).Methods("POST", "OPTIONS")

	r.HandleFunc("/call/{fnid}",			handleFunctionCall).Methods("POST", "OPTIONS")

	err = dbConnect(&conf)
	if err != nil {
		glog.Fatalf("Can't setup mongo: %s",
				err.Error())
	}

	err = eventsInit(&conf)
	if err != nil {
		glog.Fatalf("Can't setup events: %s", err.Error())
	}

	err = statsInit(&conf)
	if err != nil {
		glog.Fatalf("Can't setup stats: %s", err.Error())
	}

	err = swk8sInit(&conf, config_path)
	if err != nil {
		glog.Fatalf("Can't setup connection to kubernetes: %s",
				err.Error())
	}

	err = BalancerInit(&conf)
	if err != nil {
		glog.Fatalf("Can't setup: %s", err.Error())
	}

	err = BuilderInit(&conf)
	if err != nil {
		glog.Fatalf("Can't set up builder: %s", err.Error())
	}

	err = PrometheusInit(&conf)
	if err != nil {
		glog.Fatalf("Can't set up prometheus: %s", err.Error())
	}

	err = swyhttp.ListenAndServe(
		&http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, SwyModeDevel, func(s string) { glog.Debugf(s) })
	if err != nil {
		glog.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
