package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/hex"
	"net/http"
	"strings"
	"errors"
	"flag"
	"time"
	"fmt"
	"net"
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
)

var log *zap.SugaredLogger

type YAMLConfSwd struct {
	Port		int			`yaml:"port"`
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

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("Trying to login user %s", params.UserName)

	token, err = swyks.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
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

func handleProjectDel(w http.ResponseWriter, r *http.Request, tennant string) error {
	var par swyapi.ProjectDel
	var fns []FunctionDesc
	var mws []MwareDesc
	var id *SwoId
	var ferr error

	err := swyhttp.ReadAndUnmarshalReq(r, &par)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, par.Project, "")

	fns, err = dbFuncListProj(id)
	if err != nil {
		ferr = err
		goto out
	}
	for _, fn := range fns {
		id.Name = fn.SwoId.Name
		err = removeFunction(&conf, id)
		if err != nil {
			log.Error("Funciton removal failed: %s", err.Error())
			ferr = err
		}
	}

	mws, err = dbMwareGetAll(id)
	if err != nil {
		ferr = err
		goto out
	}

	for _, mw := range mws {
		id.Name = mw.SwoId.Name
		err = mwareRemove(&conf.Mware, id)
		if err != nil {
			log.Error("Mware removal failed: %s", err.Error())
			ferr = err
		}
	}

	if ferr != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
out:
	return ferr
}

func handleProjectList(w http.ResponseWriter, r *http.Request, tennant string) error {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string

	projects := make(map[string]struct{})

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

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

	err = swyhttp.MarshalAndWrite(w, &result)
out:
	return err
}

func handleFunctionAdd(w http.ResponseWriter, r *http.Request, tennant string) error {
	var params swyapi.FunctionAdd

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if params.Project == "" {
		params.Project = SwyDefaultProject
	}

	err = swyFixSize(&params.Size, &conf)
	if err != nil {
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
out:
	return err
}

func handleFunctionUpdate(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionUpdate

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("function/update %s", id.Str())

	err = updateFunction(&conf, id, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
out:
	return err
}

func handleFunctionRemove(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionRemove

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("function/remove %s", id.Str())

	err = removeFunction(&conf, id)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
out:
	return err
}

func handleFunctionCode(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionXID
	var fn FunctionDesc
	var codeFile string
	var fnCode []byte

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if params.Version == "" {
		params.Version = fn.Src.Version
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("Get FN code %s:%s", id.Str(), params.Version)

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	codeFile, err = fnCodePath(&conf, &fn, params.Version)
	if err != nil {
		goto out
	}

	fnCode, err = ioutil.ReadFile(codeFile)
	if err != nil {
		err = fmt.Errorf("Can't read file with code: %s", err.Error())
		goto out
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionSources {
			Type: "code",
			Code: base64.StdEncoding.EncodeToString(fnCode),
		})
out:
	return err
}

func handleFunctionStats(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionID
	var fn FunctionDesc
	var stats *FnStats
	var lcs string

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("Get FN stats %s", id.Str())

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	stats = statsGet(&fn)
	if stats.Called != 0 {
		lcs = stats.LastCall.Format(time.UnixDate)
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionStats{
			Called:		stats.Called,
			LastCall:	lcs,
		})
out:
	return err
}

func handleFunctionInfo(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionID
	var fn FunctionDesc
	var url = ""
	var stats *FnStats
	var lcs string

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

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
	if stats.Called != 0 {
		lcs = stats.LastCall.Format(time.UnixDate)
	}

	err = swyhttp.MarshalAndWrite(w,  swyapi.FunctionInfo{
			State:          fnStates[fn.State],
			Mware:          fn.Mware,
			Version:         fn.Src.Version,
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
				LastCall:	lcs,
			},
			Size:		swyapi.FunctionSize {
				Memory:		fn.Size.Mem,
				Timeout:	fn.Size.Tmo,
				Rate:		fn.Size.Rate,
				Burst:		fn.Size.Burst,
			},
		})
out:
	return err
}

func handleFunctionLogs(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionID
	var resp []swyapi.FunctionLogEntry
	var logs []DBLogRec

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("Get logs for %s", tennant)

	logs, err = logGetFor(id)
	if err != nil {
		goto out
	}

	for _, log := range logs {
		resp = append(resp, swyapi.FunctionLogEntry{
				Event:	log.Event,
				Ts:	log.Time.Format(time.UnixDate),
				Text:	log.Text,
			})
	}

	err = swyhttp.MarshalAndWrite(w, resp)
out:
	return err
}

func fnCallable(fn *FunctionDesc) bool {
	return fn.URLCall && (fn.State == swy.DBFuncStateRdy || fn.State == swy.DBFuncStateUpd)
}

func makeArgMap(r *http.Request) map[string]string {
	args := make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args[k] = v[0]
	}

	return args
}

func handleFunctionCall(w http.ResponseWriter, r *http.Request) {
	var arg_map map[string]string
	var res *swyapi.SwdFunctionRunResult
	var err error
	var fmd *FnMemData

	fnId := mux.Vars(r)["fnid"]

	code := http.StatusServiceUnavailable
	link := dbBalancerLinkFindByCookie(fnId)
	if link == nil {
		err = errors.New("No such function")
		goto out
	}
	if !link.Public {
		err = errors.New("No API for function")
		goto out
	}

	fmd = memdGet(fnId)
	if fmd.crl != nil {
		if !fmd.crl.Get() {
			code = http.StatusTooManyRequests
			err = errors.New("Ratelimited")
			goto out
		}
	}

	arg_map = makeArgMap(r)

	code = http.StatusInternalServerError
	res, err = talkToLink(link, fmd, fnId, "run", arg_map)
	if err != nil {
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

func handleFunctionRun(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.FunctionRun
	var fn FunctionDesc
	var res *swyapi.SwdFunctionRunResult

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.FuncName)
	log.Debugf("function/run %s", id.Str())

	fn, err = dbFuncFindStates(id, []int{swy.DBFuncStateRdy, swy.DBFuncStateUpd})
	if err != nil {
		err = errors.New("No such function")
		goto out
	}

	res, err = doRun(fn.Cookie, "run", params.Args)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, swyapi.FunctionRunResult{
		Return:		res.Return,
		Stdout:		res.Stdout,
		Stderr:		res.Stderr,
	})
out:
	return err
}

func handleFunctionList(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var recs []FunctionDesc
	var result []swyapi.FunctionItem
	var params swyapi.FunctionList

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

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

	err = swyhttp.MarshalAndWrite(w, &result)
out:
	return err
}

func handleMwareAdd(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.MwareAdd

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.ID)
	log.Debugf("mware/add: %s params %v", tennant, params)

	err = mwareSetup(&conf.Mware, id, params.Type)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
out:
	return err
}

func handleLanguages(w http.ResponseWriter, r *http.Request, tennant string) error {
	var ret []string

	for l, lh := range rt_handlers {
		if lh.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, l)
	}

	return swyhttp.MarshalAndWrite(w, ret)
}

func handleMwareTypes(w http.ResponseWriter, r *http.Request, tennant string) error {
	var ret []string

	for mw, mt := range mwareHandlers {
		if mt.Devel && !SwyModeDevel {
			continue
		}

		ret = append(ret, mw)
	}

	return swyhttp.MarshalAndWrite(w, ret)
}

func handleMwareList(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var result []swyapi.MwareItem
	var params swyapi.MwareList
	var mwares []MwareDesc

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, "")
	log.Debugf("list mware for %s", tennant)

	mwares, err = dbMwareGetAll(id)
	if err != nil {
		goto out
	}

	for _, mware := range mwares {
		result = append(result,
			swyapi.MwareItem{
				ID:	   mware.Name,
				Type:	   mware.MwareType,
			})
	}

	err = swyhttp.MarshalAndWrite(w, &result)
out:
	return err
}

func handleMwareRemove(w http.ResponseWriter, r *http.Request, tennant string) error {
	var id *SwoId
	var params swyapi.MwareRemove

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	id = makeSwoId(tennant, params.Project, params.ID)
	log.Debugf("mware/remove: %s params %v", tennant, params)

	err = mwareRemove(&conf.Mware, id)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
out:
	return err
}

func handleGenericReq(r *http.Request) (string, int, error) {
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

func genReqHandler(cb func(w http.ResponseWriter, r *http.Request, tennant string) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tennant, code, err := handleGenericReq(r)
		if err == nil {
			code = http.StatusBadRequest
			err = cb(w, r, tennant)
		}
		if err != nil {
			log.Errorf("Error in callback: %s", err.Error())
			http.Error(w, err.Error(), code)
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
	log = logger.Sugar()

	swy.InitLogger(log)
}

func main() {
	var config_path string
	var err error

	flag.StringVar(&config_path,
			"conf",
				"",
				"path to a config file")
	flag.BoolVar(&SwyModeDevel, "devel", false, "launch in development mode")
	flag.Parse()

	if config_path != "" {
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
		setupMwareAddr(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	gateSecrets, err = swysec.ReadSecrets("gate")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	gateSecPas, err = hex.DecodeString(gateSecrets[conf.Mware.SecKey])
	if err != nil || len(gateSecPas) < 16 {
		log.Errorf("Secrets pass should be decodable and at least 16 bytes long")
		return
	}

	log.Debugf("config: %v", &conf)

	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin)
	r.Handle("/v1/project/list",		genReqHandler(handleProjectList))
	r.Handle("/v1/project/del",		genReqHandler(handleProjectDel))
	r.Handle("/v1/function/add",		genReqHandler(handleFunctionAdd))
	r.Handle("/v1/function/update",		genReqHandler(handleFunctionUpdate))
	r.Handle("/v1/function/remove",		genReqHandler(handleFunctionRemove))
	r.Handle("/v1/function/run",		genReqHandler(handleFunctionRun))
	r.Handle("/v1/function/list",		genReqHandler(handleFunctionList))
	r.Handle("/v1/function/info",		genReqHandler(handleFunctionInfo))
	r.Handle("/v1/function/stats",		genReqHandler(handleFunctionStats))
	r.Handle("/v1/function/code",		genReqHandler(handleFunctionCode))
	r.Handle("/v1/function/logs",		genReqHandler(handleFunctionLogs))
	r.Handle("/v1/mware/add",		genReqHandler(handleMwareAdd))
	r.Handle("/v1/mware/list",		genReqHandler(handleMwareList))
	r.Handle("/v1/mware/remove",		genReqHandler(handleMwareRemove))

	r.Handle("/v1/info/langs",		genReqHandler(handleLanguages))
	r.Handle("/v1/info/mwares",		genReqHandler(handleMwareTypes))

	r.HandleFunc("/call/{fnid}",			handleFunctionCall)

	err = dbConnect(&conf)
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
