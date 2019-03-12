/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"github.com/gorilla/mux"

	"net/http"
	"net/url"
	"flag"
	"strings"
	"context"
	"time"
	"sync"
	"fmt"

	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/secrets"
	"swifty/common/ratelimit"
	"swifty/common/xrest/sysctl"
)

var ModeDevel bool
var gateSecrets xsecret.Store
var gateSecPas []byte

func isLite() bool { return Flavor == "lite" }

const (
	DefaultProject string			= "default"
	NoProject string			= "*"
	CloneDir				= "clone"
	FunctionsSubdir				= "functions"
	PackagesSubdir				= "packages"
	RunDir string				= "/var/run/swifty"
)

var (
	PodStartBase time.Duration		= 100 * time.Millisecond
	PodStartGain time.Duration		= 50 * time.Millisecond
	PodStartTmo time.Duration		= 120 * time.Second
	DepScaleupRelax time.Duration		= 16 * time.Second
	DepScaledownStep time.Duration		= 8 * time.Second
	TenantLimitsUpdPeriod time.Duration	= 120 * time.Second
	TokenCacheExpires			= 60 * time.Second
	PodTokenLen int				= 64
)

func init() {
	sysctl.AddTimeSysctl("pod_start_tmo",		&PodStartTmo)
	sysctl.AddTimeSysctl("pod_start_relax",	&PodStartBase)
	sysctl.AddTimeSysctl("pod_start_gain",		&PodStartGain)
	sysctl.AddTimeSysctl("dep_scaleup_relax",	&DepScaleupRelax)
	sysctl.AddTimeSysctl("dep_scaledown_step",	&DepScaledownStep)
	sysctl.AddTimeSysctl("limits_update_period",	&TenantLimitsUpdPeriod)

	sysctl.AddTimeSysctl("token_cache_exp",		&TokenCacheExpires)
	sysctl.AddIntSysctl("pod_token_len",		&PodTokenLen)

	sysctl.AddRoSysctl("gate_mode", func() string {
		ret := "mode:"
		if ModeDevel {
			ret += "devel"
		} else {
			ret += "prod"
		}

		ret += ", flavor:" + Flavor

		return ret
	})

	sysctl.AddRoSysctl("gate_version", func() string { return Version })
}

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

func reqPath(r *http.Request) string {
	p := strings.SplitN(r.URL.Path, "/", 4)
	if len(p) >= 4 {
		return p[3]
	} else {
		empty := ""
		return empty
	}
}

var grl *xrl.RL

func reqPeriods(q url.Values) int {
	periods, e := xhttp.ReqAtoi(q, "periods", 0)
	if e != nil {
		periods = -1
	}
	return periods
}

func makeContextFor(r *http.Request, tenant, role string) (context.Context, func(context.Context)) {
	if role == swyapi.AdminRole {
		/*
		 * Setting X-Relay-Tennant means that it's an admin
		 * coming to modify the user's setup. In this case we
		 * need the swifty.admin role. Otherwise it's the
		 * swifty.owner guy that can only work on his tennant.
		 */

		rten := r.Header.Get("X-Relay-Tennant")
		if rten != "" {
			tenant = rten
		}
	}

	return mkContext3("::r", tenant, role)
}

var tdCache sync.Map

func admdValidateToken(token string) (*swyapi.TokenData, int) {
	td, ok := tdCache.Load(token)
	if ok {
		return td.(*swyapi.TokenData), 0
	}

	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Method: "GET",
				Address: conf.Admd.Addr + "/v1/token",
				Timeout: uint(conf.Runtime.Timeout.Max),
				Headers: map[string]string {
					"X-Subject-Token": token,
				},
			}, nil)
	if err != nil {
		if resp == nil {
			return nil, http.StatusInternalServerError
		} else {
			return nil, resp.StatusCode
		}
	}

	var res swyapi.TokenData

	err = xhttp.RResp(resp, &res)
	if err != nil {
		return nil, http.StatusInternalServerError
	}

	td, ok = tdCache.LoadOrStore(token, &res)
	if !ok {
		time.AfterFunc(TokenCacheExpires, func() { tdCache.Delete(token) })
	}

	return td.(*swyapi.TokenData), 0
}

func getReqContext(w http.ResponseWriter, r *http.Request) (context.Context, func(context.Context)) {
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		http.Error(w, "Auth token not provided", http.StatusUnauthorized)
		return nil, nil
	}

	td, code := admdValidateToken(token)
	if td == nil {
		http.Error(w, "Token validation error", code)
		return nil, nil
	}

	return makeContextFor(r, td.Tenant, td.Role)
}

func genReqHandler(cb gateGenReq) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) {
			return
		}

		ctx, done := getReqContext(w, r)
		if ctx == nil {
			return
		}

		ctxlog(ctx).Debugf("REQ %s %s.%s from %s", gctx(ctx).Tenant, r.Method, r.URL.Path, r.RemoteAddr)
		defer done(ctx)

		traceRequest(ctx, r)

		cerr := cb(ctx, w, r)
		if cerr != nil {
			ctxlog(ctx).Errorf("Error: %s", cerr.Message)
			http.Error(w, cerr.String(), http.StatusBadRequest)
			traceError(ctx, cerr)
		}
	})
}

var clientMethods = []string { "GET", "PUT", "POST", "DELETE", "PATCH", "HEAD", "OPTIONS" }

func methodNr(m string) uint {
	for i, cm := range clientMethods {
		if m == cm {
			return uint(i)
		}
	}

	return 31
}

func getHandlers() http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin).Methods("POST", "OPTIONS")
	r.HandleFunc("/github",			handleGithubEvent).Methods("POST")

	r.Handle("/v1/sysctl",			genReqHandler(handleSysctls)).Methods("GET", "OPTIONS")
	r.Handle("/v1/sysctl/{name}",		genReqHandler(handleSysctl)).Methods("GET", "PUT", "OPTIONS")

	r.Handle("/v1/stats",			genReqHandler(handleTenantStatsAll)).Methods("GET", "OPTIONS")
	r.Handle("/v1/stats/{sub}",		genReqHandler(handleTenantStats)).Methods("GET", "OPTIONS")
	r.Handle("/v1/logs",			genReqHandler(handleLogs)).Methods("GET", "OPTIONS")
	r.Handle("/v1/project/list",		genReqHandler(handleProjectList)).Methods("POST", "OPTIONS")
	r.Handle("/v1/project/del",		genReqHandler(handleProjectDel)).Methods("POST", "OPTIONS")

	r.Handle("/v1/functions",		genReqHandler(handleFunctions)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/tree",		genReqHandler(handleFunctionsTree)).Methods("GET", "OPTIONS")
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

	r.Handle("/v1/packages",		genReqHandler(handlePackages)).Methods("GET", "OPTIONS")
	r.Handle("/v1/packages/{lang}",		genReqHandler(handlePackagesLang)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/packages/{lang}/{pkgid:[a-zA-Z0-9./_-]+}",
						genReqHandler(handlePackage)).Methods("GET", "DELETE", "OPTIONS")

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
	r.Handle("/v1/info/mwares/{mtyp}",	genReqHandler(handleMwareType)).Methods("GET", "OPTIONS")

	r.PathPrefix("/call/{urlid}").Methods(clientMethods...).HandlerFunc(handleCall)

	r.HandleFunc("/websockets/{ws}", handleWebSocketClient)
	r.PathPrefix("/websockets/{ws}/conns").Methods("POST").HandlerFunc(handleWebSocketsMw)

	return r
}

func main() {
	var config_path string
	var showVersion bool
	var err error

	flag.StringVar(&config_path,
			"conf",
				"/etc/swifty/conf/gate.yaml",
				"path to a config file")
	flag.BoolVar(&ModeDevel, "devel", false, "launch in development mode")
	flag.BoolVar(&showVersion, "version", false, "show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("Version %s\n", Version)
		return
	}

	gateSecrets, err = xsecret.Init("gate")
	if err != nil {
		fmt.Printf("Can't read gate secrets: %s", err.Error())
		return
	}

	err = xh.ReadYamlConfig(config_path, &conf)
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

	err = setupMwareAddr(&conf)
	if err != nil {
		glog.Errorf("Bad mware configuration: %s", err.Error())
		return
	}

	if isLite() {
		grl = xrl.MakeRL(0, 1000)
	}

	glog.Debugf("Flavor: %s", Flavor)
	glog.Debugf("Proxy: %v", conf.Wdog.Proxy != 0)

	err = tracerInit()
	if err != nil {
		glog.Fatalf("Can't set up tracer")
	}

	err = dbConnect()
	if err != nil {
		glog.Fatalf("Can't setup mongo: %s",
				err.Error())
	}

	ctx, done := mkContext("::init")

	err = eventsInit(ctx)
	if err != nil {
		glog.Fatalf("Can't setup events: %s", err.Error())
	}

	err = statsInit()
	if err != nil {
		glog.Fatalf("Can't setup stats: %s", err.Error())
	}

	err = k8sInit(ctx, config_path)
	if err != nil {
		glog.Fatalf("Can't setup connection to kubernetes: %s",
				err.Error())
	}

	err = BalancerInit()
	if err != nil {
		glog.Fatalf("Can't setup: %s", err.Error())
	}

	err = DeployInit(ctx)
	if err != nil {
		glog.Fatalf("Can't set up deploys: %s", err.Error())
	}

	err = ReposInit(ctx)
	if err != nil {
		glog.Fatalf("Can't start repo syncer: %s", err.Error())
	}

	err = PrometheusInit(ctx)
	if err != nil {
		glog.Fatalf("Can't set up prometheus: %s", err.Error())
	}

	MwInit()
	RtInit()
	done(ctx)

	err = xhttp.ListenAndServe(
		&http.Server{
			Handler:      getHandlers(),
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, ModeDevel || isLite(), func(s string) { glog.Debugf(s) })
	if err != nil {
		glog.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
