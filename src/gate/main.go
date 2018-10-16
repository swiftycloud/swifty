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

	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/keystone"
	"swifty/common/secrets"
	"swifty/common/xratelimit"
)

var ModeDevel bool
var gateSecrets map[string]string
var gateSecPas []byte

func isLite() bool { return Flavor == "lite" }

const (
	DefaultProject string			= "default"
	NoProject string			= "*"
	CloneDir				= "clone"
	RunDir string				= "/var/run/swifty"
)

var (
	PodStartTmo time.Duration		= 120 * time.Second
	DepScaleupRelax time.Duration		= 16 * time.Second
	DepScaledownStep time.Duration		= 8 * time.Second
	TenantLimitsUpdPeriod time.Duration	= 120 * time.Second
)

func init() {
	addTimeSysctl("pod_start_tmo",		&PodStartTmo)
	addTimeSysctl("dep_scaleup_relax",	&DepScaleupRelax)
	addTimeSysctl("dep_scaledown_step",	&DepScaledownStep)
	addTimeSysctl("limits_update_period",	&TenantLimitsUpdPeriod)
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

var grl *xratelimit.RL

func reqPeriods(q url.Values) int {
	periods, e := xhttp.ReqAtoi(q, "periods", 0)
	if e != nil {
		periods = -1
	}
	return periods
}

func makeContextFor(r *http.Request, tenant string, admin bool) (context.Context, func(context.Context)) {
	if admin {
		rten := r.Header.Get("X-Relay-Tennant")
		if rten != "" {
			tenant = rten
		}
	}

	return mkContext3("::r", tenant, admin)
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
		if role.Name == swyapi.AdminRole {
			admin = true
		}
		if role.Name == swyapi.UserRole {
			user = true
		}
	}

	if !admin && !user {
		http.Error(w, "Keystone authentication error", http.StatusForbidden)
		return nil, nil
	}

	return makeContextFor(r, td.Project.Name, admin)
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

		ctxlog(ctx).Debugf("REQ %s %s.%s", gctx(ctx).Tenant, r.Method, r.URL.Path)
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

	r.Handle("/v1/stats",			genReqHandler(handleTenantStatsAll)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/stats/{sub}",		genReqHandler(handleTenantStats)).Methods("GET", "POST", "OPTIONS")
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
	glog.Debugf("Proxy: %v", conf.Wdog.Proxy != 0)
	RtInit()

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

	err = BuilderInit(ctx)
	if err != nil {
		glog.Fatalf("Can't set up builder: %s", err.Error())
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
