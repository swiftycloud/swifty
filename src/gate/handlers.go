/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"github.com/gorilla/websocket"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2/bson"

	"compress/gzip"
	"net/http"
	"net/url"
	"strings"
	"context"
	"time"
	"fmt"
	"io"

	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/xrest"
	"swifty/common/keystone"
)

type gateGenReq func(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr

var callCORS bool = true

func init() {
	addBoolSysctl("call_default_cors", &callCORS)
}

func handleCall(w http.ResponseWriter, r *http.Request) {
	if callCORS && xhttp.HandleCORS(w, r, CORS_Clnt_Methods, CORS_Clnt_Headers) {
		return
	}

	sopq := statsStart()

	ctx, done := mkContext2("::call", swyapi.NobodyRole)
	defer done(ctx)

	uid := mux.Vars(r)["urlid"]

	url, err := urlFind(ctx, uid)
	if err != nil {
		if dbNF(err) {
			http.Error(w, "No such URL", http.StatusServiceUnavailable)
		} else {
			http.Error(w, "Error getting URL handler", http.StatusInternalServerError)
		}
		return
	}

	url.Handle(ctx, w, r, sopq)
}

func apiGate() string {
	ag := conf.Daemon.ApiGate
	if ag == "" {
		ag = conf.Daemon.Addr
	}

	return xh.MakeEndpoint(ag)
}

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest
	var td swyapi.UserToken

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	glog.Debugf("Trying to login user %s", params.UserName)

	token, td.Expires, err = xkst.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = apiGate()
	glog.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

	w.Header().Set("X-Subject-Token", token)
	err = xhttp.Respond(w, &td)
	if err != nil {
		resp = http.StatusInternalServerError
		goto out
	}

	return

out:
	http.Error(w, err.Error(), resp)
}

/******************************* PROJECTS *************************************/
func delAll(ctx context.Context, q url.Values, f xrest.Factory) *xrest.ReqErr {
	var os []xrest.Obj

	xer := f.Iterate(ctx, q, func(c context.Context, o xrest.Obj) *xrest.ReqErr {
					os = append(os, o)
					return nil
				})
	if xer != nil {
		return xer
	}

	for _, o := range os {
		xer = o.Del(ctx)
		if xer != nil {
			return nil
		}
	}

	return nil
}

func handleProjectDel(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var par swyapi.ProjectDel

	err := xhttp.RReq(r, &par)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	q := url.Values{"project": []string{par.Project}}

	xer := delAll(ctx, q, Deployments{})
	if xer != nil {
		return xer
	}

	xer = delAll(ctx, q, Functions{})
	if xer != nil {
		return xer
	}

	xer = delAll(ctx, q, Mwares{})
	if xer != nil {
		return xer
	}

	xer = delAll(ctx, q, Routers{})
	if xer != nil {
		return xer
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func handleProjectList(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string

	projects := make(map[string]struct{})

	err := xhttp.RReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

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

/******************************* FUNCTIONS ************************************/
func handleFunctionAuthCtx(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ac string
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnAuthProp{}, &ac)
}

func handleFunctionEnv(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var env []string
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnEnvProp{}, &env)
}

func handleFunctionSize(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var sz swyapi.FunctionSize
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnSzProp{}, &sz)
}

func handleFunctionMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	var mid string
	return xrest.HandleMany(ctx, w, r, FnMwares{Fn: fn.(*FunctionDesc)}, &mid)
}

func handleFunctionMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleOne(ctx, w, r, FnMwares{Fn: fn.(*FunctionDesc)}, nil)
}

func handleFunctionAccounts(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	var aid string
	return xrest.HandleMany(ctx, w, r, FnAccounts{Fn: fn.(*FunctionDesc)}, &aid)
}

func handleFunctionAccount(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	return xrest.HandleOne(ctx, w, r, FnAccounts{Fn: fn.(*FunctionDesc)}, nil)
}

func handleFunctionS3Bs(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fo, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	fn := fo.(*FunctionDesc)

	switch r.Method {
	case "GET":
		return xrest.Respond(ctx, w, fn.S3Buckets)

	case "POST":
		var bname string
		err := xhttp.RReq(r, &bname)
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
	fo, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	fn := fo.(*FunctionDesc)
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
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	var evt swyapi.FunctionEvent
	return xrest.HandleMany(ctx, w, r, Triggers{fn.(*FunctionDesc)}, &evt)
}

func handleFunctionTrigger(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return xrest.HandleOne(ctx, w, r, Triggers{}, nil)
}

func handleFunctionWait(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fo, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	fn := fo.(*FunctionDesc)
	var wo swyapi.FunctionWait
	err := xhttp.RReq(r, &wo)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	timeout := time.Duration(wo.Timeout) * time.Millisecond
	var tmo bool

	if wo.Version != "" {
		err, tmo = waitFunctionVersion(ctx, fn, wo.Version, timeout)
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
	var src swyapi.FunctionSources
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnSrcProp{}, &src)
}

func handleFunctionStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnStatsProp{ }, nil)
}

func handleFunctionLogs(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fo, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	fn := fo.(*FunctionDesc)
	return handleLogsFor(ctx, fn.SwoId.Cookie(), w, r.URL.Query())
}

func handleFunctions(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.FunctionAdd
	return xrest.HandleMany(ctx, w, r, Functions{}, &params)
}

type FName struct {
	Name	string
	Path	string
	Kids	[]*FName
}

func handleFunctionsTree(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}
	leafs := (q.Get("leafs") != "")

	iter := dbCol(ctx, DBColFunc).Find(listReq(ctx, project, []string{})).Select(bson.M{"name": 1}).Iter()
	root := FName{Name: "/", Kids: []*FName{}}

	var fn FName
	for iter.Next(&fn) {
		n := &root
		path := strings.Split(fn.Name, ".")
		if !leafs {
			path = path[:len(path)-1]
		}
		for _, p := range path {
			var tn *FName
			for _, c := range n.Kids {
				if c.Name == p {
					tn = c
					break
				}
			}

			if tn == nil {
				tn = &FName{Name: p, Path: n.Path + p + ".", Kids: []*FName{}}
				n.Kids = append(n.Kids, tn)
			}

			n = tn
		}
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return xrest.Respond(ctx, w, root)
}

func handleFunction(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var upd swyapi.FunctionUpdate
	return xrest.HandleOne(ctx, w, r, Functions{}, &upd)
}

func handleFunctionMdat(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fn, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, fn.(*FunctionDesc).toMInfo(ctx))
}

func handleFunctionRun(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	fo, cerr := Functions{}.Get(ctx, r)
	if cerr != nil {
		return cerr
	}

	fn := fo.(*FunctionDesc)
	var params swyapi.FunctionRun
	var res *swyapi.WdogFunctionRunResult

	err := xhttp.RReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	if fn.State != DBFuncStateRdy {
		return GateErrM(swyapi.GateNotAvail, "Function not ready (yet)")
	}

	suff := ""
	if params.Src != nil {
		td, err := tendatGet(ctx)
		if err != nil {
			return GateErrD(err)
		}

		td.runlock.Lock()
		defer td.runlock.Unlock()

		suff, cerr = prepareTempRun(ctx, fn, td, params.Src, w)
		if suff == "" {
			return cerr
		}

		params.Src = nil /* not to propagate to wdog */
	}

	if params.Method == nil {
		params.Method = &r.Method /* POST */
	}

	conn, errc := balancerGetConnExact(ctx, fn.Cookie, fn.Src.Version)
	if errc != nil {
		return errc
	}

	res, err = conn.Run(ctx, nil, suff, "run", &params)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	if fn.SwoId.Project == "test" {
		res.Stdout = xh.Fortune()
	}

	return xrest.Respond(ctx, w, res)
}

/******************************* ROUTERS **************************************/
func handleRouters(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.RouterAdd
	return xrest.HandleMany(ctx, w, r, Routers{}, &params)
}

func handleRouter(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return xrest.HandleOne(ctx, w, r, Routers{}, nil)
}

func handleRouterTable(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var tbl []*swyapi.RouterEntry
	return xrest.HandleProp(ctx, w, r, Routers{}, &RtTblProp{}, &tbl)
}

/******************************* ACCOUNTS *************************************/
func handleAccounts(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params map[string]string
	return xrest.HandleMany(ctx, w, r, Accounts{}, &params)
}

func handleAccount(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params map[string]string
	return xrest.HandleOne(ctx, w, r, Accounts{}, &params)
}

/******************************* REPOS ****************************************/
func handleRepos(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.RepoAdd
	return xrest.HandleMany(ctx, w, r, Repos{}, &params)
}

func handleRepo(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ru swyapi.RepoUpdate
	return xrest.HandleOne(ctx, w, r, Repos{}, &ru)
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

	cerr = rd.pullManual(ctx)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

/******************************* PACKAGES *************************************/
func handlePackages(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	pstat, cerr := packagesStats(ctx, r)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, pstat)
}

func handlePackagesLang(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	if !ModeDevel {
		return GateErrC(swyapi.GateNotAvail)
	}

	var params swyapi.PkgAdd
	return xrest.HandleMany(ctx, w, r, Packages{mux.Vars(r)["lang"]}, &params)
}

func handlePackage(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	if !ModeDevel {
		return GateErrC(swyapi.GateNotAvail)
	}

	return xrest.HandleOne(ctx, w, r, Packages{mux.Vars(r)["lang"]}, nil)
}

/******************************* MWARES ***************************************/
func handleMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.MwareAdd
	return xrest.HandleMany(ctx, w, r, Mwares{}, &params)
}

func handleMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return xrest.HandleOne(ctx, w, r, Mwares{}, nil)
}

/******************************* DEPLOYMETS ***********************************/
func handleDeployments(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ds swyapi.DeployStart
	return xrest.HandleMany(ctx, w, r, Deployments{}, &ds)
}

func handleDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return handleOneDeployment(ctx, w, r, Deployments{})
}

func handleOneDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request, ds Deployments) *xrest.ReqErr {
	return xrest.HandleOne(ctx, w, r, ds, nil)
}

func handleAuths(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	switch r.Method {
	case "GET":
		return xrest.HandleGetList(ctx, w, r, Deployments{true})

	case "POST":
		var aa swyapi.AuthAdd

		err := xhttp.RReq(r, &aa)
		if err != nil {
			return GateErrE(swyapi.GateBadRequest, err)
		}

		if aa.Type != "" && aa.Type != "jwt" {
			return GateErrM(swyapi.GateBadRequest, "No such auth type")
		}

		if demoRep.ObjID == "" {
			return GateErrM(swyapi.GateGenErr, "AaaS configuration error")
		}

		dd := getDeployDesc(ctxSwoId(ctx, aa.Project, aa.Name))
		cerr := dd.getItems(ctx, &swyapi.DeployStart{
				From: swyapi.DeploySource{
					Repo: demoRep.ObjID.Hex() + "/" + conf.DemoRepo.AAASDep,
				}})
		if cerr != nil {
			ctxlog(ctx).Errorf("Error getting %s file", conf.DemoRepo.AAASDep)
			return cerr
		}

		cerr = dd.Start(ctx)
		if cerr != nil {
			return cerr
		}

		di, _ := dd.toInfo(ctx, false, false)
		return xrest.Respond(ctx, w, &di)
	}

	return nil
}

func handleAuth(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	return handleOneDeployment(ctx, w, r, Deployments{true})
}

/******************************* MISC *****************************************/
func handleLanguages(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ret []string

	for l, lh := range rt_handlers {
		if lh.Disabled {
			continue
		}

		ret = append(ret, l)
	}

	return xrest.Respond(ctx, w, ret)
}

func handleLanguage(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	lang := mux.Vars(r)["lang"]
	lh, ok := rt_handlers[lang]
	if !ok || lh.Disabled {
		return GateErrM(swyapi.GateGenErr, "Language not supported")
	}

	return xrest.Respond(ctx, w, lh.info())
}

func handleMwareTypes(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var ret []string

	for mw, mt := range mwareHandlers {
		if mt.Disabled {
			continue
		}

		ret = append(ret, mw)
	}

	return xrest.Respond(ctx, w, ret)
}

func handleMwareType(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	mtyp := mux.Vars(r)["mtyp"]
	ret, cerr := mwareGetInfo(ctx, mtyp)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, ret)
}

func handleS3Access(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.S3Access

	err := xhttp.RReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	creds, cerr := s3GetCreds(ctx, &params)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, creds)
}

func writeLogs(w io.Writer, logs []DBLogRec) {
	for _, loge := range logs {
		fmt.Fprintf(w, "%s%12s: %s\n",
			loge.Time.String(), loge.Event, loge.Text)
	}
}

func gzipLogs(ctx context.Context, w http.ResponseWriter, logs []DBLogRec) *xrest.ReqErr {
	w.Header().Set("Content-Type", "application/gzip")
	w.WriteHeader(http.StatusOK)
	gw := gzip.NewWriter(w)
	writeLogs(gw, logs)
	gw.Close()
	return nil
}

func textLogs(ctx context.Context, w http.ResponseWriter, logs []DBLogRec) *xrest.ReqErr {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	writeLogs(w, logs)
	return nil
}

func handleLogsFor(ctx context.Context, cookie string, w http.ResponseWriter, q url.Values) *xrest.ReqErr {
	since, cerr := getSince(q)
	if cerr != nil {
		return cerr
	}

	logs, err := logGetFor(ctx, cookie, since)
	if err != nil {
		return GateErrD(err)
	}

	fmt := q.Get("as")
	switch fmt {
	case "", "json":
		var resp []*swyapi.LogEntry
		for _, loge := range logs {
			resp = append(resp, &swyapi.LogEntry{
				Event:	loge.Event,
				Ts:	loge.Time.Format(time.RFC1123Z),
				Text:	loge.Text,
			})
		}

		return xrest.Respond(ctx, w, resp)
	case "gzip":
		return gzipLogs(ctx, w, logs)
	case "text":
		return textLogs(ctx, w, logs)
	default:
		return GateErrM(swyapi.GateBadRequest, "Bad format")
	}
}

func handleLogs(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	id := ctxSwoId(ctx, NoProject, "")
	return handleLogsFor(ctx, id.PCookie(), w, q)
}

func handleTenantStatsAll(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	periods := reqPeriods(r.URL.Query())

	var resp swyapi.TenantStatsResp
	var cerr *xrest.ReqErr

	resp.Stats, cerr = getCallStats(ctx, periods)
	if cerr != nil {
		return cerr
	}

	resp.Mware, cerr = getMwareStats(ctx)
	if cerr != nil {
		return cerr
	}

	resp.S3, cerr = getS3Stats(ctx)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, resp)
}

func handleTenantStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	sub := mux.Vars(r)["sub"]

	periods := reqPeriods(r.URL.Query())

	var cerr *xrest.ReqErr
	var resp interface{}

	switch sub {
	case "calls":
		resp, cerr = getCallStats(ctx, periods)
	case "mware":
		resp, cerr = getMwareStats(ctx)
	case "s3":
		resp, cerr = getS3Stats(ctx)
	default:
		cerr = GateErrM(swyapi.GateBadRequest, "Bad stats type")
	}

	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, resp)
}

func handleGithubEvent(w http.ResponseWriter, r *http.Request) {
	ev := r.Header.Get("X-Github-Event")
	switch ev {
	case "push":
		go func() {
			ctx, done := mkContext("::gh-push")
			githubRepoUpdated(ctx, r)
			done(ctx)
		}()
	}

	w.WriteHeader(http.StatusOK)
}

var wsupgrader = websocket.Upgrader{}

func handleWebSocketClient(w http.ResponseWriter, r *http.Request) {
	ws := mux.Vars(r)["ws"]
	var wsmw MwareDesc

	ctx, done := mkContext2("::ws", swyapi.NobodyRole)
	err := dbFind(ctx, bson.M{"cookie": ws, "mwaretype": "websocket", "state": DBMwareStateRdy}, &wsmw)
	if err != nil {
		done(ctx)
		http.Error(w, "No such websocket", http.StatusNotFound)
		return
	}

	var claims map[string]interface{}

	if wsmw.HDat != nil {
		aid, ok := wsmw.HDat["authctx"]
		if !ok {
			done(ctx)
			http.Error(w, "Authorization error", http.StatusInternalServerError)
			return
		}

		/* FIXME -- makes sence to cache the guy on wsConnMap */
		ac, err := authCtxGet(ctx, wsmw.SwoId, aid)
		if err != nil {
			done(ctx)
			http.Error(w, "Authorization error", http.StatusInternalServerError)
			return
		}

		claims, err = ac.Verify(r)
		if err != nil {
			done(ctx)
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}
	}

	done(ctx)


	c, err := wsupgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	wsClientReq(&wsmw, c, claims)
}

func handleWebSocketsMw(w http.ResponseWriter, r *http.Request) {
	ctx, done := mkContext2("::ws", swyapi.UserRole)
	defer done(ctx)

	var wsmw MwareDesc
	ws := mux.Vars(r)["ws"]

	err := dbFind(ctx, bson.M{"cookie": ws, "mwaretype": "websocket", "state": DBMwareStateRdy}, &wsmw)
	if err != nil {
		http.Error(w, "No such websocket", http.StatusNotFound)
		return
	}

	sec := r.Header.Get("X-WS-Token")
	if sec != wsmw.Secret {
		http.Error(w, "Not authorized", http.StatusUnauthorized)
		return
	}

	path := strings.SplitN(r.URL.Path, "/", 5)
	cid := ""
	if len(path) > 4 {
		cid = path[4]
	}

	cerr := wsFunctionReq(ctx, &wsmw, cid, w, r)
	if cerr != nil {
		http.Error(w, cerr.String(), http.StatusBadRequest)
	}
}

func handleSysctls(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	if !gctx(ctx).Admin() {
		return GateErrC(swyapi.GateNotAvail)
	}
	return xrest.HandleMany(ctx, w, r, Sysctls{}, nil)
}

func handleSysctl(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	if !gctx(ctx).Admin() {
		return GateErrC(swyapi.GateNotAvail)
	}
	var upd string
	return xrest.HandleOne(ctx, w, r, Sysctls{}, &upd)
}
