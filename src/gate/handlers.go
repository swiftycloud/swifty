package main

import (
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2/bson"

	"net/http"
	"strings"
	"context"
	"time"

	"../apis"
	"../common"
	"../common/http"
	"../common/xrest"
	"../common/keystone"
)

type gateGenReq func(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr

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

	td.Endpoint = conf.Daemon.Addr
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
func handleProjectDel(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var par swyapi.ProjectDel
	var fns []*FunctionDesc
	var mws []*MwareDesc
	var id *SwoId
	var ferr *xrest.ReqErr

	err := xhttp.RReq(r, &par)
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
	return xrest.HandleProp(ctx, w, r, Functions{}, &FnLogsProp{}, nil)
}

func handleFunctions(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	var params swyapi.FunctionAdd
	return xrest.HandleMany(ctx, w, r, Functions{}, &params)
}

type FName struct {
	Name	string
	Kids	[]*FName
}

func handleFunctionsTree(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = DefaultProject
	}

	var fns []FName
	err := dbCol(ctx, DBColFunc).Find(listReq(ctx, project, []string{})).Select(bson.M{"name": 1}).All(&fns)
	if err != nil {
		return GateErrD(err)
	}

	root := FName{Name: "/", Kids: []*FName{}}
	for _, fn := range fns {
		n := &root
		path := strings.Split(fn.Name, ".")
		for _, p := range path {
			var tn *FName
			for _, c := range n.Kids {
				if c.Name == p {
					tn = c
					break
				}
			}

			if tn == nil {
				tn = &FName{Name: p, Kids: []*FName{}}
				n.Kids = append(n.Kids, tn)
			}

			n = tn
		}
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
	var params swyapi.SwdFunctionRun
	var res *swyapi.SwdFunctionRunResult

	err := xhttp.RReq(r, &params)
	if err != nil {
		return GateErrE(swyapi.GateBadRequest, err)
	}

	if fn.State != DBFuncStateRdy {
		return GateErrM(swyapi.GateNotAvail, "Function not ready (yet)")
	}

	suff := ""
	if params.Src != nil {
		suff, cerr = prepareTempRun(ctx, fn, params.Src, w)
		if suff == "" {
			return cerr
		}

		params.Src = nil /* not to propagate to wdog */
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

	cerr = rd.pull(ctx)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
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
	return handleOneDeployment(ctx, w, r, Deployments{true})
}

/******************************* MISC *****************************************/
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

func handleTenantStatsAll(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	ten := gctx(ctx).Tenant

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

	resp.S3, cerr = getS3Stats(ctx)
	if cerr != nil {
		return cerr
	}

	return xrest.Respond(ctx, w, resp)
}

func handleTenantStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *xrest.ReqErr {
	sub := mux.Vars(r)["sub"]
	ten := gctx(ctx).Tenant

	periods := reqPeriods(r.URL.Query())

	var cerr *xrest.ReqErr
	var resp interface{}

	switch sub {
	case "calls":
		resp, cerr = getCallStats(ctx, ten, periods)
	case "wmare":
		resp, cerr = getMwareStats(ctx, ten)
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
