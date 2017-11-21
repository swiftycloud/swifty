package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"errors"
	"flag"
	"time"
	"fmt"
	"gopkg.in/mgo.v2/bson"

	"../apis/apps"
	"../common"
)

type FnScriptDesc struct {
	Lang		string		`bson:"lang"`
	Run		string		`bson:"run"`
	Env		[]string	`bson:"env"`
}

type FnSrcDesc struct {
	Type		string		`bson:"type"`
	Repo		string		`bson:"repo,omitempty"`
	Commit		string		`bson:"commit"`		// Top commit in the repo
	Code		string		`bson:"-"`
}

type FnEventDesc struct {
	Source		string		`bson:"source"`
	CronTab		string		`bson:"crontab"`
	MwareId		string		`bson:"mwid"`
	MQueue		string		`bson:"mqueue"`
}

type FnSizeDesc struct {
	Replicas	int		`bson:"replicas"`
	Mem		string		`bson:"mem"`
}

type SwoId struct {
	Tennant		string		`bson:"tennant"`
	Project		string		`bson:"project"`
	Name		string		`bson:"name"`
}

func makeSwoId(tennant, project, name string) *SwoId {
	return &SwoId{Tennant: tennant, Project: project, Name: name}
}

func (id *SwoId) Str () string {
	rv := id.Tennant
	if id.Project != "" {
		rv += "/" + id.Project
		if id.Name != "" {
			rv += "/" + id.Name
		}
	}
	return rv
}

type FunctionDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Index		string		`bson:"index"`		// Project + FuncName

	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`		// Some "unique" identifier
	State		int		`bson:"state"`		// Function state
	CronID		int		`bson:"cronid"`		// ID of cron trigger (if present)
	URLCall		bool		`bson:"urlcall"`	// Funciton is callable via direct URL
	Event		FnEventDesc	`bson:"event"`
	Mware		[]string	`bson:"mware"`
	Script		FnScriptDesc	`bson:"script"`
	Src		FnSrcDesc	`bson:"src"`
	Size		FnSizeDesc	`bson:"size"`
	OneShot		bool		`bson:"oneshot"`
}

var noCommit = "00000000"

func (fi *FnInst)DepName() string {
	dn := "swd-" + fi.fn.Cookie[:32]
	if fi.Build {
		dn += "-bld"
	}
	return dn
}

func (fi *FnInst)Replicas() int32 {
	if fi.Build {
		return 1
	} else {
		return int32(fi.fn.Size.Replicas)
	}
}

/*
 * We may have several instances of Fn running
 * Regular -- this one is up-n-running with the fn ready to run
 * Build -- this is a single replica deployment building the fn
 * Old -- this is Regular, but with the sources of previous version.
 *        In parallel to the Old one we may have one Build instance
 *        running building an Fn from new sources.
 * At some point in time the Old instance gets replaced with the
 * new Regular one.
 */
type FnInst struct {
	Build		bool
	fn		*FunctionDesc
}

func (fn *FunctionDesc) Inst() *FnInst {
	return &FnInst { Build: false, fn: fn }
}

func (fn *FunctionDesc) InstBuild() *FnInst {
	return &FnInst { Build: true, fn: fn }
}

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

type YAMLConfMQ struct {
	YAMLConfMWCreds				`yaml:",inline"`
	AdminPort	string			`yaml:"admport"`
}

type YAMLConfSQL struct {
	YAMLConfMWCreds				`yaml:",inline"`
}

type YAMLConfMongo struct {
	YAMLConfMWCreds				`yaml:",inline"`
}

type YAMLConfMw struct {
	MQ		YAMLConfMQ		`yaml:"mq"`
	SQL		YAMLConfSQL		`yaml:"sql"`
	MGO		YAMLConfMongo		`yaml:"mongo"`
}

type YAMLConfRt struct {
	Image		string			`yaml:"image"`
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
	Runtime		map[string]YAMLConfRt	`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
	Kuber		YAMLConfKuber		`yaml:"kubernetes"`
}

var conf YAMLConf
var gatesrv *http.Server

func genFunctionDescJSON(conf *YAMLConf, fn *FunctionDesc, fi *FnInst) string {
	var run []string
	var jdata []byte
	var err error

	if fi.Build {
		// Build run.The rest of the fn.Build will be passed
		// as arguments for doRun further.
		run = strings.Split(RtBuildCmd(fn.Script.Lang), " ")[:1]
	} else {
		// Classical run or after-the-build run.
		run = RtRunCmd(fn)
	}

	jdata, err = json.Marshal(&swyapi.SwdFunctionDesc{
				Run:		run,
				Dir:		RtGetWdogPath(fn),
				Stats:		statsPodPath,
				PodToken:	fn.Cookie,
				URLCall:	fn.URLCall,
			})
	if err != nil {
		log.Errorf("marshal error: %s", err.Error())
		return ""
	}

	return string(jdata[:])
}

func runFunctionOnce(fn *FunctionDesc) {
	log.Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(fn.Inst(), "oneshot", []string{})
	log.Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(&conf, fn, fn.Inst())
	dbFuncSetState(fn, swy.DBFuncStateStl);
}

func notifyPodUpdate(pod *BalancerPod) {
	var err error = nil

	if pod.State == swy.DBPodStateRdy {
		fn, err2 := dbFuncFind(&pod.SwoId)
		if err2 != nil {
			err = err2
			goto out
		}

		logSaveEvent(&fn, "POD", fmt.Sprintf("state: %s", fnStates[fn.State]))
		if fn.State == swy.DBFuncStateBld || fn.State == swy.DBFuncStateUpd {
			err = buildFunction(&fn)
			if err != nil {
				goto out
			}
		} else if fn.State == swy.DBFuncStateBlt || fn.State == swy.DBFuncStateQue {
			dbFuncSetState(&fn, swy.DBFuncStateRdy)
			if fn.OneShot {
				runFunctionOnce(&fn)
			}
		}
	}

	return

out:
	log.Errorf("POD update notify: %s", err.Error())
}

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("Try to login user %s", params.UserName)

	token, err = swy.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain,
				params.UserName, params.Password)
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

	var role string

	/*
	 * Setting X-Relay-Tennant means that it's an admin
	 * coming to modify the user's setup. In this case we
	 * need the swifty.admin role. Otherwise it's the
	 * swifty.owner guy that can only work on his tennant.
	 */
	tennant := r.Header.Get("X-Relay-Tennant")
	if tennant == "" {
		role = "swifty.owner"
	} else {
		role = "swifty.admin"
	}

	reqTen, code := swy.KeystoneVerify(conf.Keystone.Addr, token, role)
	if reqTen == "" {
		return "", code, fmt.Errorf("Keystone authentication error")
	}

	if tennant == "" {
		tennant = reqTen
	}

	return tennant, 0, nil
}

func handleProjectList(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
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
	id = makeSwoId(tennant, "", "")

	log.Debugf("List projects for %s", id.Str())

	fns, mws, err = dbProjectListAll(id)
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

func genCookie(fn *FunctionDesc) string {
	h := sha256.New()
	h.Write([]byte(fn.Tennant + "/" + fn.Project + "/" + fn.Name))

	return hex.EncodeToString(h.Sum(nil))
}

func getFunctionDesc(tennant string, p_add *swyapi.FunctionAdd) *FunctionDesc {
	fn := &FunctionDesc {
		SwoId: SwoId {
			Tennant: tennant,
			Project: p_add.Project,
			Name:	 p_add.FuncName,
		},
		Event:		FnEventDesc {
			Source:		p_add.Event.Source,
			CronTab:	p_add.Event.CronTab,
			MwareId:	p_add.Event.MwareId,
			MQueue:		p_add.Event.MQueue,
		},
		Src:		FnSrcDesc {
			Type:		p_add.Sources.Type,
			Repo:		p_add.Sources.Repo,
			Code:		p_add.Sources.Code,
		},
		Size:		FnSizeDesc {
			Replicas:	p_add.Size.Replicas,
			Mem:		p_add.Size.Memory,
		},
		Script:		FnScriptDesc {
			Lang:		p_add.Script.Lang,
			Run:		p_add.Script.Run,
			Env:		p_add.Script.Env,
		},
	}

	fn.Cookie = genCookie(fn)
	return fn
}

func handleFunctionAdd(w http.ResponseWriter, r *http.Request) {
	var params swyapi.FunctionAdd
	var fn *FunctionDesc
	var fi *FnInst
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest

	if params.Size.Replicas < 1 {
		params.Size.Replicas = 1
	} else if params.Size.Replicas > conf.Kuber.MaxReplicas {
		err = errors.New("Too big replicas value")
		goto out
	}

	if params.Project == "" || params.FuncName == "" ||
			params.Script.Lang == "" {
		err = errors.New("Parameters are missed")
		goto out
	}

	err = swy.ValidateProjectAndFuncName(params.Project, params.FuncName)
	if err != nil {
		goto out
	}

	fn = getFunctionDesc(tennant, &params)
	if RtBuilding(fn.Script.Lang) {
		fn.State = swy.DBFuncStateBld
	} else {
		fn.State = swy.DBFuncStateQue
	}

	log.Debugf("function/add %s (cookie %s)", fn.SwoId.Str(), fn.Cookie[:32])

	err = dbFuncAdd(fn)
	if err != nil {
		goto out
	}

	// FIXME -- move to /built handler
	err = mwareSetup(&conf, fn.SwoId, params.Mware, fn)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out_clean_func
	}

	if fn.Event.Source != "" {
		err = eventSetup(&conf, fn, true)
		if err != nil {
			err = fmt.Errorf("Unable to setup even %s: %s", fn.Event, err.Error())
			goto out_clean_func
		}
	}

	err = getSources(fn)
	if err != nil {
		goto out_clean_mware
	}

	statsStartCollect(&conf, fn)

	err = dbFuncUpdateAdded(fn)
	if err != nil {
		goto out_clean_repo
	}

	if RtBuilding(fn.Script.Lang) {
		fi = fn.InstBuild()
	} else {
		fi = fn.Inst()
	}

	err = swk8sRun(&conf, fn, fi)
	if err != nil {
		goto out_clean_repo
	}

	logSaveEvent(fn, "registered", "")
	w.WriteHeader(http.StatusOK)
	return

out_clean_repo:
	cleanRepo(fn)
out_clean_mware:
	mwareRemove(&conf, fn.SwoId, fn.Mware)
out_clean_func:
	dbFuncRemove(fn)
out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/add error %s", err.Error())
}

func handleFunctionUpdate(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var fn FunctionDesc
	var params swyapi.FunctionUpdate
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("function/update %s", id.Str())

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	// FIXME -- lock other requests :\
	if fn.State != swy.DBFuncStateRdy && fn.State != swy.DBFuncStateStl {
		err = fmt.Errorf("function %s is not running", fn.SwoId.Str())
		goto out
	}

	err = updateSources(&fn)
	if err != nil {
		goto out
	}

	if RtBuilding(fn.Script.Lang) {
		if fn.State == swy.DBFuncStateRdy {
			fn.State = swy.DBFuncStateUpd
		} else {
			fn.State = swy.DBFuncStateBld
		}
	}

	err = dbFuncUpdatePulled(&fn)
	if err != nil {
		goto out
	}

	if RtBuilding(fn.Script.Lang) {
		log.Debugf("Starting build dep")
		err = swk8sRun(&conf, &fn, fn.InstBuild())
	} else {
		log.Debugf("Updating deploy")
		err = swk8sUpdate(&conf, &fn)
	}

	if err != nil {
		goto out
	}

	logSaveEvent(&fn, "updated", fmt.Sprintf("to: %s", fn.Src.Commit))
	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/update error %s", err.Error())
}

func handleFunctionRemove(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var fn FunctionDesc
	var params swyapi.FunctionRemove
	var code int

	tennant, code, err := handleGenericReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	id = makeSwoId(tennant, params.Project, params.FuncName)

	log.Debugf("function/remove %s", id.Str())

	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(id, swy.DBFuncStateTrm,
					[]int{swy.DBFuncStateRdy, swy.DBFuncStateStl})
	if err != nil {
		goto out
	}

	fn, err = dbFuncFind(id)
	if err != nil {
		goto out
	}

	if !fn.OneShot {
		err = swk8sRemove(&conf, &fn, fn.Inst())
		if err != nil {
			log.Errorf("remove deploy error: %s", err.Error())
			goto out
		}
	}

	forgetFunction(&fn)

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("function/remove error %s", err.Error())
}

func forgetFunction(fn *FunctionDesc) {
	var err error

	if fn.Event.Source != "" {
		err = eventSetup(&conf, fn, false)
		if err != nil {
			log.Errorf("remove event %s error: %s", fn.Event, err.Error())
		}
	}

	err = mwareRemove(&conf, fn.SwoId, fn.Mware)
	if err != nil {
		log.Errorf("remove mware error: %s", err.Error())
	}

	statsStopCollect(&conf, fn)
	cleanRepo(fn)
	logRemove(fn)
	dbFuncRemove(fn)
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
		link := dbBalancerLinkFind(fn.Inst().DepName())
		if link != nil {
			url = link.VIP() + "/" + fn.Cookie
		}
	}

	stats = statsGet(&fn)

	err = swy.HTTPMarshalAndWrite(w,  swyapi.FunctionInfo{
			State:          fnStates[fn.State],
			Mware:          fn.Mware,
			Commit:         fn.Src.Commit,
			URL:		url,
			Script:		swyapi.FunctionScript{
				Lang:		fn.Script.Lang,
				Run:		fn.Script.Run,
				Env:		fn.Script.Env,
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

func handleFunctionRun(w http.ResponseWriter, r *http.Request) {
	var id *SwoId
	var params swyapi.FunctionRun
	var fn FunctionDesc
	var stdout, stderr string
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

	fn_code, stdout, stderr, err = doRun(fn.Inst(), "run", params.Args)
	if err != nil {
		goto out
	}

	err = swy.HTTPMarshalAndWrite(w, swyapi.FunctionRunResult{
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
	swy.DBFuncStateQue: "preparing",
	swy.DBFuncStateStl: "stalled",
	swy.DBFuncStateBld: "building",
	swy.DBFuncStateBlt: "built", // FIXME -- WTF?
	swy.DBFuncStatePrt: "partial",
	swy.DBFuncStateRdy: "ready",
	swy.DBFuncStateUpd: "updating",
	swy.DBFuncStateTrm: "terminating",
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

	err = mwareSetup(&conf, *id, params.Mware, nil)
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
				Counter: mware.Counter,
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
	var devel bool

	flag.StringVar(&config_path,
			"conf",
				"",
				"path to a config file")
	flag.BoolVar(&devel, "devel", false, "launch in development mode")
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

	r.HandleFunc("/v1/mware/add",			handleMwareAdd)
	r.HandleFunc("/v1/mware/list",			handleMwareList)
	r.HandleFunc("/v1/mware/remove",		handleMwareRemove)
	if devel {
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
