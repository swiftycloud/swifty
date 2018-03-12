package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/json"
	"encoding/hex"
	"net/http"
	"strings"
	"errors"
	"flag"
	"context"
	"sync/atomic"
	"time"
	"fmt"
	"net"
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
var gateSecrets map[string]string
var gateSecPas []byte

const (
	SwyDefaultProject string	= "default"
	SwyPodStartTmo time.Duration	= 120 * time.Second
	SwyBodyArg string		= "_SWY_BODY_"
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
}

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
}

type YAMLConfMWCreds struct {
	Addr		string			`yaml:"address"`
	Admin		string			`yaml:"admin"`
	Pass		string			`yaml:"password"`
}

type YAMLConfRabbit struct {
	YAMLConfMWCreds				`yaml:",inline"`
	AdminPort	string			`yaml:"admport"`
}

type YAMLConfMaria struct {
	YAMLConfMWCreds				`yaml:",inline"`
	QDB		string			`yaml:"quotdb"`
}

type YAMLConfMongo struct {
	YAMLConfMWCreds				`yaml:",inline"`
}

type YAMLConfPostgres struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	Token		string			`yaml:"token"`
}

type YAMLConfS3Notify struct {
	URL		string			`yaml:"url"`
	User		string			`yaml:"user"`
	Pass		string			`yaml:"password"`
}

type YAMLConfS3 struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	Token		string			`yaml:"token"`
	Notify		YAMLConfS3Notify	`yaml:"notify"`
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
	Images		map[string]string	`yaml:"images"`
}

type YAMLConfKuber struct {
	ConfigPath	string			`yaml:"config-path"`
	MaxReplicas	int			`yaml:"max-replicas"`
}

type YAMLConfDB struct {
	StateDB		string		`yaml:"state"`
	Addr		string		`yaml:"address"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"password"`
}

type YAMLConf struct {
	DB		YAMLConfDB		`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
	Mware		YAMLConfMw		`yaml:"middleware"`
	Runtime		YAMLConfRt		`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
	Kuber		YAMLConfKuber		`yaml:"kubernetes"`
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

	codeFile, err = fnCodePath(&conf, fn, params.Version)
	if err != nil {
		return GateErrE(swy.GateNotAvail, err)
	}

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

func handleFunctionStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionID
	var stats *FnStats
	var lcs string

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

	stats = statsGet(fn)
	if stats.Called != 0 {
		lcs = stats.LastCall.Format(time.RFC1123Z)
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionStats{
			Called:		stats.Called,
			Timeouts:	stats.Timeouts,
			Errors:		stats.Errors,
			LastCall:	lcs,
			Time:		uint64(stats.RunTime.Nanoseconds()/1000),
			GBS:		stats.GBS(),
		})
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
	var lcs string

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

	stats = statsGet(fn)
	if stats.Called != 0 {
		lcs = stats.LastCall.Format(time.RFC1123Z)
	}

	fv, err = dbBalancerRSListVersions(fn)
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
				LastCall:	lcs,
				Time:		uint64(stats.RunTime.Nanoseconds()/1000),
				GBS:		stats.GBS(),
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

func makeArgMap(r *http.Request) map[string]string {
	defer r.Body.Close()

	args := make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args[k] = v[0]
	}

	body, err := ioutil.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		args[SwyBodyArg] = string(body)
	}

	return args
}

func ratelimited(fmd *FnMemData) bool {
	/* Per-function RL first, as it's ... more likely to fail */
	if fmd.crl != nil && !fmd.crl.Get() {
		return true
	}

	if fmd.td.crl != nil && !fmd.td.crl.Get() {
		if fmd.crl != nil {
			fmd.crl.Put()
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
	var conn string

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ctx := context.Background()
	fnId := mux.Vars(r)["fnid"]

	fmd = memdGet(fnId)
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


	arg_map = makeArgMap(r)
	res, err = doRunConn(ctx, conn, fmd, fnId, "call", arg_map)
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
	return

out:
	http.Error(w, err.Error(), code)
}

func handleFunctionRun(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.FunctionRun
	var conn string
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

	res, err = doRunConn(ctx, conn, nil, fn.Cookie, "run", params.Args)
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
		var lcs string
		stats := statsGet(&v)
		if stats.Called != 0 {
			lcs = stats.LastCall.Format(time.RFC1123Z)
		}

		result = append(result,
			swyapi.FunctionItem{
				FuncName:	v.Name,
				State:		fnStates[v.State],
				Timeout:	v.Size.Tmo,
				LastCall:	lcs,
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
	// "ip:port"
	genAddrIP := func(addrip string) string {
		v := strings.Split(addrip, ":")
		if len(v) > 0 && net.ParseIP(v[0]) == nil {
			ips, err := net.LookupIP(v[0])
			if err == nil && len(ips) > 0 {
				if len(v) == 2 {
					return ips[0].String() + ":" + v[1]
				} else {
					return ips[0].String()
				}
			}
		}
		return addrip
	}

	conf.Mware.Rabbit.Addr	= genAddrIP(conf.Mware.Rabbit.Addr)
	conf.Mware.Maria.Addr	= genAddrIP(conf.Mware.Maria.Addr)
	conf.Mware.Mongo.Addr	= genAddrIP(conf.Mware.Mongo.Addr)
	conf.Mware.Postgres.Addr= genAddrIP(conf.Mware.Postgres.Addr)
	conf.Mware.S3.Addr	= genAddrIP(conf.Mware.S3.Addr)
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

	swy.InitLogger(glog)
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

	glog.Debugf("config: %v", &conf)

	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin).Methods("POST", "OPTIONS")
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

	err = swk8sInit(&conf)
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

	gatesrv = &http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
	}

	err = gatesrv.ListenAndServe()
	if err != nil {
		glog.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
