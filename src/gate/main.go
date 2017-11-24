package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/json"
	"net/http"
	"errors"
	"flag"
	"time"
	"fmt"

	"../apis/apps"
	"../common"
)

var SwyModeDevel bool

const (
	SwyDefaultProject string	= "default"
)

var log *zap.SugaredLogger

type YAMLConfSwd struct {
	CtPath		string			`yaml:"ct-path"`
	Addr		string			`yaml:"address"`
}

type YAMLConfSources struct {
	Share		string			`yaml:"share"`
	Clone		string			`yaml:"clone"`
}

type YAMLConfBalancerIPS struct {
	IP		string			`yaml:"ip"`
	Ports		string			`yaml:"ports"`
}

type YAMLConfBalancer struct {
	LocalIps	[]YAMLConfBalancerIPS	`yaml:"localips"`
}

type YAMLConfDaemon struct {
	Addr		string			`yaml:"address"`
	ViewDir		string			`yaml:"view"`
	Sources		YAMLConfSources		`yaml:"sources"`
	LogLevel	string			`yaml:"loglevel"`
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
}

type YAMLConfMongo struct {
	YAMLConfMWCreds				`yaml:",inline"`
}

type YAMLConfPostgres struct {
	Addr		string			`yaml:"address"`
	AdminPort	string			`yaml:"admport"`
	Token		string			`yaml:"token"`
}

type YAMLConfMw struct {
	Rabbit		YAMLConfRabbit		`yaml:"rabbit"`
	Maria		YAMLConfMaria		`yaml:"maria"`
	Mongo		YAMLConfMongo		`yaml:"mongo"`
	Postgres	YAMLConfPostgres	`yaml:"postgres"`
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

type YAMLConf struct {
	DB		swy.YAMLConfDB		`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
	Balancer	YAMLConfBalancer	`yaml:"balancer"`
	Mware		YAMLConfMw		`yaml:"middleware"`
	Runtime		YAMLConfRt		`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
	Kuber		YAMLConfKuber		`yaml:"kubernetes"`
}

var conf YAMLConf
var gatesrv *http.Server

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("Try to login user %s", params.UserName)

	token, err = swy.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	log.Debugf("Login passed, token %s", token[:16])

	w.Header().Set("X-Subject-Token", token)
	w.WriteHeader(http.StatusOK)

	return

out:
	http.Error(w, err.Error(), resp)
}

func handleGenericReq(r *http.Request, params interface{}) (string, int, error) {
	err := swy.HTTPReadAndUnmarshal(r, params)
	if err != nil {
		return "", http.StatusBadRequest, err
	}
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return "", http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	td, code := swy.KeystoneGetTokenData(conf.Keystone.Addr, token)
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
		role = swy.SwyUserRole
		tennant = td.Project.Name
	} else {
		role = swy.SwyAdminRole
	}

	if !swy.KeystoneRoleHas(td, role) {
		return "", http.StatusForbidden, fmt.Errorf("Keystone authentication error")
	}

	return tennant, 0, nil
}

func handleProjectList(w http.ResponseWriter, r *http.Request) {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string
	var code int

	projects := make(map[string]struct{})

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	log.Debugf("List projects for %s", tennant)
	fns, mws, err = dbProjectListAll(tennant)
	if err != nil {
		goto out
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

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleFunctionAdd(w http.ResponseWriter, r *http.Request) {
	var params swyapi.FunctionAdd
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest

	if params.Project == "" {
		params.Project = SwyDefaultProject
	}

	if params.Size.Timeout == 0 {
		params.Size.Timeout = conf.Runtime.Timeout.Def * 1000
	} else if params.Size.Timeout > conf.Runtime.Timeout.Max * 1000 {
		err = errors.New("Too big timeout")
		goto out
	}

	if params.Size.Memory == 0 {
		params.Size.Memory = conf.Runtime.Memory.Def
	} else if params.Size.Memory > conf.Runtime.Memory.Max ||
			params.Size.Memory < conf.Runtime.Memory.Min {
		err = errors.New("Too small/big memory size")
		goto out
	}

	if params.FuncName == "" || params.Code.Lang == "" {
		err = errors.New("Parameters are missed")
		goto out
	}

	err = addFunction(&conf, tennant, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/add error %s", err.Error())
}

func handleFunctionUpdate(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionUpdate
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("function/update %s", id.Str())

	err = updateFunction(&conf, id)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/update error %s", err.Error())
}

func handleFunctionRemove(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionRemove
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("function/remove %s", id.Str())

	err = removeFunction(&conf, id)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/remove error %s", err.Error())
}

func handleFunctionInfo(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionID
	var fn FunctionDesc
	var url = ""
	var code int
	var stats *FnStats

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("Get FN Info %s", id.Str())

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	if (fn.URLCall) {
		url = "/call/" + fn.Cookie
	}

	stats = statsGet(&fn)

	err = swy.HTTPMarshalAndWrite(w,  swyapi.FunctionInfo{
			State:          fnStates[fn.State],
			Mware:          fn.Mware,
			Commit:         fn.Src.Commit,
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
			},
			Stats:		swyapi.FunctionStats {
				Called:		stats.Called,
			},
		})
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("logs error %s", err.Error())
}
func handleFunctionLogs(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionID
	var resp []swyapi.FunctionLogEntry
	var logs []DBLogRec
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("Get logs for %s", tennant)

	logs, err = logGetFor(id)
	if err != nil {
		goto out
	}

	for _, log := range logs {
		resp = append(resp, swyapi.FunctionLogEntry{
				Commit: log.Commit,
				Event: log.Event,
				Ts: log.Time.String(),
				Text: log.Text,
			})
	}

	err = swy.HTTPMarshalAndWrite(w, resp)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("logs error %s", err.Error())
}

func fnCallable(fn *FunctionDesc) bool {
	return fn.URLCall && (fn.State == swy.DBFuncStateRdy || fn.State == swy.DBFuncStateUpd)
}

func makeArgMap(r *http.Request) string {
	args := make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args[k] = v[0]
	}

	ret, _ := json.Marshal(args)
	return string(ret)
}

func handleFunctionCall(w http.ResponseWriter, r *http.Request) {
	var arg_map string
	var retjson string

	vars := mux.Vars(r)
	fnId := vars["fnid"]

	fn, err := dbFuncFindByCookie(fnId)
	if err != nil {
		goto out
	}

	if !fnCallable(&fn) {
		err = errors.New("Function is not ready")
		goto out
	}

	arg_map = makeArgMap(r)

	_, retjson, err = doRun(fn.Inst(), "run", append(RtRunCmd(&fn.Code), arg_map))
	if err != nil {
		goto out
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(retjson))
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleFunctionRun(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionRun
	var fn FunctionDesc
	var stdout, stderr, retjson string
	var code, fn_code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("function/run %s", id.Str())

	fn, err = dbFuncFindStates(id, []int{swy.DBFuncStateRdy, swy.DBFuncStateUpd})
	if err != nil {
		err = errors.New("No such function")
		goto out
	}

	fn_code, retjson, err = doRun(fn.Inst(), "run", append(RtRunCmd(&fn.Code), params.Args...))
	if err != nil {
		goto out
	}

	err = swy.HTTPMarshalAndWrite(w, swyapi.FunctionRunResult{
		Return:		retjson,
		Code:		fn_code,
		Stdout:		stdout,
		Stderr:		stderr,
	})
	if err == nil {
		return
	}

out:
	http.Error(w, err.Error(), code)
	log.Errorf("handleFunctionRun: error: %s", err.Error())
}

func handleFunctionList(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var recs []FunctionDesc
	var result []swyapi.FunctionItem
	var params swyapi.FunctionList
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, "")

	// List all but terminating
	recs, err = dbFuncListStates(id, []int{
				swy.DBFuncStateQue,
				swy.DBFuncStateBld,
				swy.DBFuncStateStl,
				swy.DBFuncStateBlt,
				swy.DBFuncStatePrt,
				swy.DBFuncStateRdy,
				swy.DBFuncStateUpd})
	if err != nil {
		goto out
	}

	for _, v := range recs {
		result = append(result,
			swyapi.FunctionItem{
				FuncName:	v.Name,
				State:		fnStates[v.State],
				Timeout:	v.Size.Tmo,
		})
	}

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleMwareAdd(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.MwareAdd
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, "")

	log.Debugf("mware/add: %s params %v", tennant, params)

	err = mwareSetup(&conf, *id, params.Mware)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("mware/add error: %s", err.Error())
}

func handleMwareList(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var result []swyapi.MwareGetItem
	var params swyapi.MwareList
	var mwares []MwareDesc
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, "")

	log.Debugf("list mware for %s", tennant)

	mwares, err = dbMwareGetAll(id)
	if err != nil {
		goto out
	}

	for _, mware := range mwares {
		result = append(result,
			swyapi.MwareGetItem{
				MwareItem: swyapi.MwareItem {
					ID:	mware.Name,
					Type:	mware.MwareType,
				},
				JSettings: mware.JSettings,
			})
	}

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("mware/get error: %s", err.Error())
}

func handleMwareRemove(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.MwareRemove
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, "")

	log.Debugf("mware/remove: %s params %v", tennant, params)
	err = mwareRemove(&conf, *id, params.MwareIDs)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("mware/remove error: %s", err.Error())
}

func handleMwareCinfo(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.MwareCinfo
	var envs []string
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.MwId)

	envs, err = mwareGetEnv(&conf, id)
	if err != nil {
		goto out
	}

	err = swy.HTTPMarshalAndWrite(w, &swyapi.MwareCinfoResp{ Envs: envs })
	if err != nil {
		goto out
	}
	return

out:
	http.Error(w, err.Error(), code)
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
	log = logger.Sugar()

	swy.InitLogger(log)
}

func main() {
	var config_path string

	flag.StringVar(&config_path,
			"conf",
				"",
				"path to a config file")
	flag.BoolVar(&SwyModeDevel, "devel", false, "launch in development mode")
	flag.Parse()

	if config_path != "" {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	log.Debugf("config: %v", &conf)

	r := mux.NewRouter()
	r.HandleFunc("/v1/user/login",			handleUserLogin)
	r.HandleFunc("/v1/project/list",		handleProjectList)

	r.HandleFunc("/v1/function/add",		handleFunctionAdd)
	r.HandleFunc("/v1/function/update",		handleFunctionUpdate)
	r.HandleFunc("/v1/function/remove",		handleFunctionRemove)
	r.HandleFunc("/v1/function/run",		handleFunctionRun)
	r.HandleFunc("/v1/function/list",		handleFunctionList)
	r.HandleFunc("/v1/function/info",		handleFunctionInfo)
	r.HandleFunc("/v1/function/logs",		handleFunctionLogs)
	r.HandleFunc("/call/{fnid}",			handleFunctionCall)

	r.HandleFunc("/v1/mware/add",			handleMwareAdd)
	r.HandleFunc("/v1/mware/list",			handleMwareList)
	r.HandleFunc("/v1/mware/remove",		handleMwareRemove)
	if SwyModeDevel {
		r.HandleFunc("/v1/mware/cinfo",		handleMwareCinfo)
	}

	err := dbConnect(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to backend: %s",
				err.Error())
	}

	err = eventsInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup events: %s", err.Error())
	}

	err = statsInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup stats: %s", err.Error())
	}

	err = swk8sInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup connection to kubernetes: %s",
				err.Error())
	}

	err = BalancerInit(&conf)
	if err != nil {
		log.Fatalf("Can't setup: %s", err.Error())
	}

	gatesrv = &http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
	}

	err = gatesrv.ListenAndServe()
	if err != nil {
		log.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
