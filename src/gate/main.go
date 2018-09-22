package main

import (
	"github.com/gorilla/mux"

	"encoding/hex"
	"net/http"
	"net/url"
	"flag"
	"strings"
	"context"
	"time"
	"fmt"
	"os"
	"gopkg.in/mgo.v2/bson"

	"../apis"
	"../common"
	"../common/http"
	"../common/xrest"
	"../common/keystone"
	"../common/secrets"
	"../common/xratelimit"
)

var SwyModeDevel bool
var SwdProxyOK bool
var gateSecrets map[string]string
var gateSecPas []byte

func isLite() bool { return Flavor == "lite" }

const (
	DefaultProject string			= "default"
	NoProject string			= "*"
	PodStartTmo time.Duration		= 120 * time.Second
	DepScaleupRelax time.Duration		= 16 * time.Second
	DepScaledownStep time.Duration		= 8 * time.Second
	TenantLimitsUpdPeriod time.Duration	= 120 * time.Second
	CloneDir				= "clone"
)

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
	http.MethodPut,
	http.MethodDelete,
}

/* These are headers and methods, that might come to /call call */
var CORS_Clnt_Headers = []string {
	"Content-Type",
	"Content-Length",
	"Authorization",
}

var CORS_Clnt_Methods = []string {
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodGet,
	http.MethodDelete,
	http.MethodHead,
}

func objFindForReq2(ctx context.Context, r *http.Request, n string, out interface{}, q bson.M) *xrest.ReqErr {
	return objFindId(ctx, mux.Vars(r)[n], out, q)
}

func objFindForReq(ctx context.Context, r *http.Request, n string, out interface{}) *xrest.ReqErr {
	return objFindForReq2(ctx, r, n, out, nil)
}

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest
	var td swyapi.UserToken

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	glog.Debugf("Trying to login user %s", params.UserName)

	token, td.Expires, err = xkst.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = conf.Daemon.Addr
	glog.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

	w.Header().Set("X-Subject-Token", token)
	err = xhttp.MarshalAndWrite(w, &td)
	if err != nil {
		resp = http.StatusInternalServerError
		goto out
	}

	return

out:
	http.Error(w, err.Error(), resp)
}

func handleProjectDel(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var par swyapi.ProjectDel
	var fns []*FunctionDesc
	var mws []*MwareDesc
	var id *SwoId
	var ferr *xrest.ReqErr

	err := xhttp.ReadAndUnmarshalReq(r, &par)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	id = ctxSwoId(ctx, par.Project, "")

	err = dbFindAll(ctx, listReq(ctx, par.Project, []string{}), &fns)
	if err != nil {
		return GateErrD(err)
	}
	for _, fn := range fns {
		id.Name = fn.SwoId.Name
		xerr := removeFunctionId(ctx, id)
		if xerr != nil {
			ctxlog(ctx).Error("Funciton removal failed: %s", xerr.Message)
			ferr = GateErrM(xerr.Code, "Cannot remove " + id.Name + " function: " + xerr.Message)
		}
	}

	err = dbFindAll(ctx, listReq(ctx, par.Project, []string{}), &mws)
	if err != nil {
		return GateErrD(err)
	}

	for _, mw := range mws {
		id.Name = mw.SwoId.Name
		xerr := mwareRemoveId(ctx, id)
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

func handleProjectList(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string

	projects := make(map[string]struct{})

	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	ctxlog(ctx).Debugf("List projects for %s", gctx(ctx).Tenant)
	fns, mws, err = dbProjectListAll(ctx, gctx(ctx).Tenant)
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

	return xrest.Respond(ctx, w, &result)
}

func handleFunctionAuthCtx(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var ac string
	return xrest.HandleProp(ctx, w, r, &fn, &FnAuthProp{}, &ac)
}

func handleFunctionEnv(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var env []string
	return xrest.HandleProp(ctx, w, r, &fn, &FnEnvProp{}, &env)
}

func handleFunctionSize(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var sz swyapi.FunctionSize
	return xrest.HandleProp(ctx, w, r, &fn, &FnSzProp{}, &sz)
}

func handleFunctionMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		return xrest.Respond(ctx, w, fn.listMware(ctx))

	case "POST":
		var mid string

		err := xhttp.ReadAndUnmarshalReq(r, &mid)
		if err != nil {
			return GateErrE(swyapi.GateBadRequest, err)
		}

		var mw MwareDesc

		cerr := objFindId(ctx, mid, &mw, bson.M{"project": fn.SwoId.Project})
		if cerr != nil {
			return cerr
		}

		err = fn.addMware(ctx, &mw)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var mw MwareDesc

	cerr = objFindForReq2(ctx, r, "mid", &mw, bson.M{"project": fn.SwoId.Project})
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "DELETE":
		err := fn.delMware(ctx, &mw)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionAccounts(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		return xrest.Respond(ctx, w, fn.listAccounts(ctx))

	case "POST":
		var aid string

		err := xhttp.ReadAndUnmarshalReq(r, &aid)
		if err != nil {
			return GateErrE(swyapi.GateBadRequest, err)
		}

		var acc AccDesc

		cerr := objFindId(ctx, aid, &acc, nil)
		if cerr != nil {
			return cerr
		}

		cerr = fn.addAccount(ctx, &acc)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionAccount(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	aid := mux.Vars(r)["aid"]

	switch r.Method {
	case "DELETE":
		var acc AccDesc

		cerr := objFindId(ctx, aid, &acc, nil)
		if cerr != nil {
			return cerr
		}

		cerr = fn.delAccount(ctx, &acc)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}


func handleFunctionS3Bs(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		return xrest.Respond(ctx, w, fn.S3Buckets)

	case "POST":
		var bname string
		err := xhttp.ReadAndUnmarshalReq(r, &bname)
		if err != nil {
			return GateErrE(swyapi.GateBadRequest, err)
		}
		err = fn.addS3Bucket(ctx, bname)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionS3B(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	bname := mux.Vars(r)["bname"]

	switch r.Method {
	case "DELETE":
		err := fn.delS3Bucket(ctx, bname)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionTriggers(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var evt swyapi.FunctionEvent
	return xrest.HandleMany(ctx, w, r, Triggers{&fn}, &evt)
}

func handleFunctionTrigger(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	eid := mux.Vars(r)["eid"]
	if !bson.IsObjectIdHex(eid) {
		return GateErrM(swyapi.GateBadRequest, "Bad event ID")
	}

	var ed FnEventDesc

	err := dbFind(ctx, bson.M{"_id": bson.ObjectIdHex(eid), "fnid": fn.Cookie}, &ed)
	if err != nil {
		return GateErrD(err)
	}

	return xrest.HandleOne(ctx, w, r, &Trigger{&ed, &fn}, nil)
}

func handleFunctionWait(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var wo swyapi.FunctionWait
	err := xhttp.ReadAndUnmarshalReq(r, &wo)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	timeout := time.Duration(wo.Timeout) * time.Millisecond
	var tmo bool

	if wo.Version != "" {
		ctxlog(ctx).Debugf("function/wait %s -> version >= %s, tmo %d", fn.SwoId.Str(), wo.Version, int(timeout))
		err, tmo = waitFunctionVersion(ctx, &fn, wo.Version, timeout)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	}

	if !tmo {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(xhttp.StatusTimeoutOccurred) /* CloudFlare's timeout */
	}

	return nil
}

func handleFunctionSources(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var src swyapi.FunctionSources
	return xrest.HandleProp(ctx, w, r, &fn, &FnSrcProp{}, &src)
}

func handleTenantStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	ten := gctx(ctx).Tenant
	ctxlog(ctx).Debugf("Get FN stats %s", ten)

	periods := reqPeriods(r.URL.Query())

	var resp swyapi.TenantStatsResp
	var cerr *xrest.ReqErr

	resp.Stats, cerr = getCallStats(ctx, ten, periods)
	if cerr != nil {
		return cerr
	}

	resp.Mware, cerr = getMwareStats(ctx, ten)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, resp)
}

func handleFunctionStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleProp(ctx, w, r, &fn, &FnStatsProp{ }, nil)
}

func handleFunctionLogs(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleProp(ctx, w, r, &fn, &FnLogsProp{}, nil)
}

func reqPath(r *http.Request) string {
	p := strings.SplitN(r.URL.Path, "/", 4)
	if len(p) >= 4 {
		return p[3]
	} else {
		empty := ""
		return empty
	}
}

var grl *xratelimit.RL

func handleCall(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Clnt_Methods, CORS_Clnt_Headers) { return }

	sopq := statsStart()

	ctx, done := mkContext2("::call", false)
	defer done(ctx)

	uid := mux.Vars(r)["urlid"]

	url, err := urlFind(ctx, uid)
	if err != nil {
		http.Error(w, "Error getting URL handler", http.StatusInternalServerError)
		return
	}

	if url == nil {
		http.Error(w, "No such URL", http.StatusServiceUnavailable)
		return
	}

	url.Handle(ctx, w, r, sopq)
}

func reqPeriods(q url.Values) int {
	periods, e := xhttp.ReqAtoi(q, "periods", 0)
	if e != nil {
		periods = -1
	}
	return periods
}

func handleFunctions(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.FunctionAdd
	return xrest.HandleMany(ctx, w, r, Functions{}, &params)
}

func handleFunction(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var upd swyapi.FunctionUpdate
	return xrest.HandleOne(ctx, w, r, &fn, &upd)
}

func handleFunctionMdat(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, fn.toMInfo(ctx))
}

func handleFunctionRun(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return cerr
	}

	var params swyapi.SwdFunctionRun
	var res *swyapi.SwdFunctionRunResult

	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	if fn.State != DBFuncStateRdy {
		return GateErrM(swyapi.GateNotAvail, "Function not ready (yet)")
	}

	suff := ""
	if params.Src != nil {
		td, err := tendatGet(ctx, gctx(ctx).Tenant)
		if err != nil {
			return GateErrD(err)
		}

		td.runlock.Lock()
		defer td.runlock.Unlock()

		if td.runrate == nil {
			td.runrate = xratelimit.MakeRL(0, 1) /* FIXME -- configurable */
		}

		if !td.runrate.Get() {
			http.Error(w, "Try-run is once per second", http.StatusTooManyRequests)
			return nil
		}

		ctxlog(ctx).Debugf("Asked for custom sources... oh, well...")
		suff, err = putTempSources(ctx, &fn, params.Src)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		err = tryBuildFunction(ctx, &fn, suff)
		if err != nil {
			return GateErrM(swyapi.GateGenErr, "Error building function")
		}

		params.Src = nil /* not to propagate to wdog */
	}

	conn, errc := balancerGetConnExact(ctx, fn.Cookie, fn.Src.Version)
	if errc != nil {
		return errc
	}

	res, err = doRunConn(ctx, conn, nil, fn.Cookie, suff, "run", &params)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	if fn.SwoId.Project == "test" {
		res.Stdout = xh.Fortune()
	}

	return xrest.Respond(ctx, w, res)
}

func handleRouters(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.RouterAdd
	return xrest.HandleMany(ctx, w, r, Routers{}, &params)
}

func handleRouter(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var rt RouterDesc

	/* FIXME -- omit table here */
	cerr := objFindForReq(ctx, r, "rid", &rt)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleOne(ctx, w, r, &rt, nil)
}

func handleRouterTable(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var rt RouterDesc

	cerr := objFindForReq(ctx, r, "rid", &rt)
	if cerr != nil {
		return cerr
	}

	var tbl []*swyapi.RouterEntry
	return xrest.HandleProp(ctx, w, r, &rt, &RtTblProp{}, &tbl)
}

func handleAccounts(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params map[string]string
	return xrest.HandleMany(ctx, w, r, Accounts{}, &params)
}

func handleAccount(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ac AccDesc

	cerr := objFindForReq(ctx, r, "aid", &ac)
	if cerr != nil {
		return cerr
	}

	var params map[string]string
	return xrest.HandleOne(ctx, w, r, &ac, &params)
}

func repoFindForReq(ctx context.Context, r *http.Request, user_action bool) (*RepoDesc, *xrest.ReqErr) {
	rid := mux.Vars(r)["rid"]
	if !bson.IsObjectIdHex(rid) {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad repo ID value")
	}

	var rd RepoDesc

	err := dbFind(ctx, ctxRepoId(ctx, rid), &rd)
	if err != nil {
		return nil, GateErrD(err)
	}

	if !user_action {
		gx := gctx(ctx)
		if !gx.Admin && rd.SwoId.Tennant != gx.Tenant {
			return nil, GateErrM(swyapi.GateNotAvail, "Shared repo")
		}
	}

	return &rd, nil
}

func handleRepos(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.RepoAdd
	return xrest.HandleMany(ctx, w, r, Repos{}, &params)
}

func handleRepo(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	rd, cerr := repoFindForReq(ctx, r, r.Method == "GET")
	if cerr != nil {
		return cerr
	}

	var ru swyapi.RepoUpdate
	return xrest.HandleOne(ctx, w, r, rd, &ru)
}

func handleRepoFiles(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	rd, cerr := repoFindForReq(ctx, r, true)
	if cerr != nil {
		return cerr
	}

	p := strings.SplitN(r.URL.Path, "/", 6)
	if len(p) < 5 {
		/* This is panic, actually */
		return GateErrM(swyapi.GateBadRequest, "Bad repo req")
	} else if len(p) == 5 {
		files, cerr := rd.listFiles(ctx)
		if cerr != nil {
			return cerr
		}

		return xrest.Respond(ctx, w, files)
	} else {
		cont, cerr := rd.readFile(ctx, p[5])
		if cerr != nil {
			return cerr
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(cont)
	}

	return nil
}

func handleRepoDesc(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	rd, cerr := repoFindForReq(ctx, r, true)
	if cerr != nil {
		return cerr
	}

	d, cerr := rd.description(ctx)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, d)
}

func handleRepoPull(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	rd, cerr := repoFindForReq(ctx, r, false)
	if cerr != nil {
		return cerr
	}

	cerr = rd.pull(ctx)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleLanguages(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ret []string

	for l, lh := range rt_handlers {
		if lh.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, l)
	}

	return xrest.Respond(ctx, w, ret)
}

func handleLanguage(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	lang := mux.Vars(r)["lang"]
	lh, ok := rt_handlers[lang]
	if !ok || (lh.Devel && !SwyModeDevel) {
		return GateErrM(swyapi.GateGenErr, "Language not supported")
	}

	return xrest.Respond(ctx, w, lh.info())
}

func handleMwareTypes(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ret []string

	for mw, mt := range mwareHandlers {
		if mt.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, mw)
	}

	return xrest.Respond(ctx, w, ret)
}

func handleS3Access(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.S3Access

	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	creds, cerr := s3GetCreds(ctx, &params)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, creds)
}

func handleDeployments(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ds swyapi.DeployStart
	return xrest.HandleMany(ctx, w, r, Deployments{}, &ds)
}

func handleDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var dd DeployDesc

	cerr := objFindForReq(ctx, r, "did", &dd)
	if cerr != nil {
		return cerr
	}

	return handleOneDeployment(ctx, w, r, &dd)
}

func handleOneDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request, dd *DeployDesc) *xrest.ReqErr {
	return xrest.HandleOne(ctx, w, r, dd, nil)
}

func handleAuths(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	switch r.Method {
	case "GET":
		var deps []*DeployDesc

		err := dbFindAll(ctx, listReq(ctx, project, []string{"auth"}), &deps)
		if err != nil {
			return GateErrD(err)
		}

		var auths []*swyapi.AuthInfo
		for _, d := range deps {
			auths = append(auths, &swyapi.AuthInfo{ Id: d.ObjID.Hex(), Name: d.SwoId.Name })
		}

		return xrest.Respond(ctx, w, auths)

	case "POST":
		var aa swyapi.AuthAdd

		err := xhttp.ReadAndUnmarshalReq(r, &aa)
		if err != nil {
			return GateErrE(swyapi.GateBadRequest, err)
		}

		if aa.Type != "" && aa.Type != "jwt" {
			return GateErrM(swyapi.GateBadRequest, "No such auth type")
		}

		if demoRep.ObjID == "" {
			return GateErrM(swyapi.GateGenErr, "AaaS configuration error")
		}

		dd := getDeployDesc(ctxSwoId(ctx, project, aa.Name))
		dd.Labels = []string{ "auth" }
		cerr := dd.getItemsParams(ctx, &swyapi.DeploySource{
			Type:	"repo",
			Repo:	demoRep.ObjID.Hex() + "/swy-aaas.yaml",
		}, []*DepParam { &DepParam{ name: "name", value: aa.Name } })
		if cerr != nil {
			ctxlog(ctx).Errorf("Error getting swy-aaas.yaml file")
			return cerr
		}

		cerr = dd.Start(ctx)
		if cerr != nil {
			return cerr
		}

		di, _ := dd.toInfo(ctx, false)
		return xrest.Respond(ctx, w, &di)
	}

	return nil
}

func handleAuth(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ad DeployDesc

	cerr := objFindForReq2(ctx, r, "aid", &ad, bson.M{"labels": "auth"})
	if cerr != nil {
		return cerr
	}

	return handleOneDeployment(ctx, w, r, &ad)
}

func handleMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.MwareAdd
	return xrest.HandleMany(ctx, w, r, Mwares{}, &params)
}

func handleMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var mw MwareDesc

	cerr := objFindForReq(ctx, r, "mid", &mw)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleOne(ctx, w, r, &mw, nil)
}

func getReqContext(w http.ResponseWriter, r *http.Request) (context.Context, func(context.Context)) {
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		http.Error(w, "Auth token not provided", http.StatusUnauthorized)
		return nil, nil
	}

	td, code := xkst.KeystoneGetTokenData(conf.Keystone.Addr, token)
	if code != 0 {
		http.Error(w, "Keystone authentication error", code)
		return nil, nil
	}

	/*
	 * Setting X-Relay-Tennant means that it's an admin
	 * coming to modify the user's setup. In this case we
	 * need the swifty.admin role. Otherwise it's the
	 * swifty.owner guy that can only work on his tennant.
	 */

	admin := false
	user := false
	for _, role := range td.Roles {
		if role.Name == xkst.SwyAdminRole {
			admin = true
		}
		if role.Name == xkst.SwyUserRole {
			user = true
		}
	}

	if !admin && !user {
		http.Error(w, "Keystone authentication error", http.StatusForbidden)
		return nil, nil
	}

	tenant := td.Project.Name
	if admin {
		rten := r.Header.Get("X-Relay-Tennant")
		if rten != "" {
			tenant = rten
		}
	}

	return mkContext2(tenant, admin)
}

type gateGenReq func(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr

func genReqHandler(cb gateGenReq) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) {
			return
		}

		ctx, done := getReqContext(w, r)
		if ctx == nil {
			return
		}

		defer done(ctx)

		traceRequest(ctx, r)

		cerr := cb(ctx, w, r)
		if cerr != nil {
			ctxlog(ctx).Errorf("Error in callback: %s", cerr.Message)
			http.Error(w, cerr.String(), http.StatusBadRequest)
			traceError(ctx, cerr)
		}
	})
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
		err := xh.ReadYamlConfig(config_path, &conf)
		if err != nil {
			fmt.Printf("Bad config: %s\n", err.Error())
			return
		}

		fmt.Printf("Validating config\n")
		err = conf.Validate()
		if err != nil {
			fmt.Printf("Error in config: %s\n", err.Error())
			return
		}

		setupLogger(&conf)
		setupMwareAddr(&conf)
	} else {
		setupLogger(nil)
		glog.Errorf("Provide config path")
		return
	}

	gateSecrets, err = xsecret.ReadSecrets("gate")
	if err != nil {
		glog.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	gateSecPas, err = hex.DecodeString(gateSecrets[conf.Mware.SecKey])
	if err != nil || len(gateSecPas) < 16 {
		glog.Errorf("Secrets pass should be decodable and at least 16 bytes long")
		return
	}

	if isLite() {
		grl = xratelimit.MakeRL(0, 1000)
	}

	glog.Debugf("Flavor: %s", Flavor)
	glog.Debugf("PROXY: %v", SwdProxyOK)

	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin).Methods("POST", "OPTIONS")
	r.Handle("/v1/stats",			genReqHandler(handleTenantStats)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/project/list",		genReqHandler(handleProjectList)).Methods("POST", "OPTIONS")
	r.Handle("/v1/project/del",		genReqHandler(handleProjectDel)).Methods("POST", "OPTIONS")

	r.Handle("/v1/functions",		genReqHandler(handleFunctions)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}",		genReqHandler(handleFunction)).Methods("GET", "PUT", "DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/run",	genReqHandler(handleFunctionRun)).Methods("POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/triggers",genReqHandler(handleFunctionTriggers)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/triggers/{eid}", genReqHandler(handleFunctionTrigger)).Methods("GET", "DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/logs",	genReqHandler(handleFunctionLogs)).Methods("GET", "OPTIONS")
	r.Handle("/v1/functions/{fid}/stats",	genReqHandler(handleFunctionStats)).Methods("GET", "OPTIONS")
	r.Handle("/v1/functions/{fid}/authctx",	genReqHandler(handleFunctionAuthCtx)).Methods("GET", "PUT", "OPTIONS")
	r.Handle("/v1/functions/{fid}/size",	genReqHandler(handleFunctionSize)).Methods("GET", "PUT", "OPTIONS")
	r.Handle("/v1/functions/{fid}/sources",	genReqHandler(handleFunctionSources)).Methods("GET", "PUT", "OPTIONS")
	r.Handle("/v1/functions/{fid}/env",	genReqHandler(handleFunctionEnv)).Methods("GET", "PUT", "OPTIONS")
	r.Handle("/v1/functions/{fid}/middleware", genReqHandler(handleFunctionMwares)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/middleware/{mid}", genReqHandler(handleFunctionMware)).Methods("DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/accounts", genReqHandler(handleFunctionAccounts)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/accounts/{aid}", genReqHandler(handleFunctionAccount)).Methods("DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/s3buckets",  genReqHandler(handleFunctionS3Bs)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/s3buckets/{bname}",  genReqHandler(handleFunctionS3B)).Methods("DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/wait",	genReqHandler(handleFunctionWait)).Methods("POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/mdat",	genReqHandler(handleFunctionMdat)).Methods("GET")

	r.Handle("/v1/middleware",		genReqHandler(handleMwares)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/middleware/{mid}",	genReqHandler(handleMware)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/repos",			genReqHandler(handleRepos)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/repos/{rid}",		genReqHandler(handleRepo)).Methods("GET", "PUT", "DELETE", "OPTIONS")
	r.PathPrefix("/v1/repos/{rid}/files").Methods("GET", "OPTIONS").Handler(genReqHandler(handleRepoFiles))
	r.Handle("/v1/repos/{rid}/pull",	genReqHandler(handleRepoPull)).Methods("POST", "OPTIONS")
	r.Handle("/v1/repos/{rid}/desc",	genReqHandler(handleRepoDesc)).Methods("GET", "OPTIONS")

	r.Handle("/v1/accounts",		genReqHandler(handleAccounts)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/accounts/{aid}",		genReqHandler(handleAccount)).Methods("GET", "PUT", "DELETE", "OPTIONS")

	r.Handle("/v1/s3/access",		genReqHandler(handleS3Access)).Methods("POST", "OPTIONS")

	r.Handle("/v1/auths",			genReqHandler(handleAuths)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/auths/{aid}",		genReqHandler(handleAuth)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/deployments",		genReqHandler(handleDeployments)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/deployments/{did}",	genReqHandler(handleDeployment)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/routers",			genReqHandler(handleRouters)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/routers/{rid}",		genReqHandler(handleRouter)).Methods("GET", "DELETE", "OPTIONS")
	r.Handle("/v1/routers/{rid}/table",	genReqHandler(handleRouterTable)).Methods("GET", "PUT", "OPTIONS")

	r.Handle("/v1/info/langs",		genReqHandler(handleLanguages)).Methods("GET", "OPTIONS")
	r.Handle("/v1/info/langs/{lang}",	genReqHandler(handleLanguage)).Methods("GET", "OPTIONS")
	r.Handle("/v1/info/mwares",		genReqHandler(handleMwareTypes)).Methods("GET", "OPTIONS")

	r.PathPrefix("/call/{urlid}").Methods("GET", "PUT", "POST", "DELETE", "PATCH", "HEAD", "OPTIONS").HandlerFunc(handleCall)

	RtInit()

	err = tracerInit()
	if err != nil {
		glog.Fatalf("Can't set up tracer")
	}

	err = dbConnect(&conf)
	if err != nil {
		glog.Fatalf("Can't setup mongo: %s",
				err.Error())
	}

	ctx, done := mkContext("::init")

	err = eventsInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't setup events: %s", err.Error())
	}

	err = statsInit(&conf)
	if err != nil {
		glog.Fatalf("Can't setup stats: %s", err.Error())
	}

	err = swk8sInit(ctx, &conf, config_path)
	if err != nil {
		glog.Fatalf("Can't setup connection to kubernetes: %s",
				err.Error())
	}

	err = BalancerInit(&conf)
	if err != nil {
		glog.Fatalf("Can't setup: %s", err.Error())
	}

	err = BuilderInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't set up builder: %s", err.Error())
	}

	err = DeployInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't set up deploys: %s", err.Error())
	}

	err = ReposInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't start repo syncer: %s", err.Error())
	}

	err = PrometheusInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't set up prometheus: %s", err.Error())
	}

	done(ctx)

	err = xhttp.ListenAndServe(
		&http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, SwyModeDevel || isLite(), func(s string) { glog.Debugf(s) })
	if err != nil {
		glog.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
