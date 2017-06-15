package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
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

type FnEventDesc struct {
	Source		string		`bson:"source"`
	CronTab		string		`bson:"crontab"`
	MwareId		string		`bson:"mwid"`
	MQueue		string		`bson:"mqueue"`
}

type FunctionDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	Index		string		`bson:"index"`		// Project + FuncName

	Project		string		`bson:"project"`	// Project name
	FuncName	string		`bson:"name"`		// Function name
	Cookie		string		`bson:"cookie"`		// Some "unique" identifier
	State		int		`bson:"state"`		// Function state
	CronID		int		`bson:"cronid"`		// ID of cron trigger (if present)
	URLCall		bool		`bson:"urlcall"`	// Funciton is callable via direct URL
	Commit		string		`bson:"commit"`		// Top commit in the repo
	OldCommit	string		`bson:"oldcommit"`
	Event		FnEventDesc	`bson:"event"`
	Repo		string		`bson:"repo"`
	Mware		[]string	`bson:"mware"`
	Script		FnScriptDesc	`bson:"script"`
	Replicas	int		`bson:"replicas"`
	OneShot		bool		`bson:"oneshot"`
}

func (fi *FnInst)DepName() string {
	dn := "swd-" + fi.fn.Cookie[:40] + "-" + fi.Commit[:8]
	if fi.Build {
		dn += "-bld"
	}
	return dn
}

func (fi *FnInst)Replicas() int32 {
	if fi.Build {
		return 1
	} else {
		return int32(fi.fn.Replicas)
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
	Commit		string
	Build		bool

	fn		*FunctionDesc
}

func (fn *FunctionDesc) Inst() *FnInst {
	return &FnInst { Commit: fn.Commit, Build: false, fn: fn }
}

func (fn *FunctionDesc) InstOld() *FnInst {
	return &FnInst { Commit: fn.OldCommit, Build: false, fn: fn }
}

func (fn *FunctionDesc) InstBuild() *FnInst {
	return &FnInst { Commit: fn.Commit, Build: true, fn: fn }
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

type YAMLConfMWCreds struct {
	Addr		string			`yaml:"address"`
	User		string			`yaml:"user"`
	Pass		string			`yaml:"password"`
}

type YAMLConfMQ struct {
	YAMLConfMWCreds				`yaml:",inline"`
	AdminPort	string			`yaml:"admport"`
}

type YAMLConfSQL struct {
	YAMLConfMWCreds				`yaml:",inline"`
}

type YAMLConfMw struct {
	MQ		YAMLConfMQ		`yaml:"mq"`
	SQL		YAMLConfSQL		`yaml:"sql"`
}

type YAMLConfRt struct {
	Image		string			`yaml:"image"`
}

type YAMLConfKuber struct {
	ConfigPath	string			`yaml:"config-path"`
}

type YAMLConf struct {
	DB		swy.YAMLConfDB		`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
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
		log.Debugf("Building desc")
		run = strings.Split(RtBuildCmd(fn.Script.Lang), " ")[:1]
	} else {
		// Classical run or after-the-build run.
		log.Debugf("Running desc")
		run = RtRunCmd(fn)
	}

	jdata, err = json.Marshal(&swyapi.SwdFunctionDesc{
				Run:		run,
				Dir:		RtGetWdogPath(fn),
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
	log.Debugf("oneshot RUN for %s", fn.FuncName)
	doRun(fn, "oneshot", fn.Inst().DepName(), []string{})
	log.Debugf("oneshor %s finished", fn.FuncName)

	swk8sRemove(&conf, fn, fn.Inst())
	dbFuncSetState(fn, swy.DBFuncStateStl);
}

func buildFunction(fn *FunctionDesc) error {
	var err error
	var orig_state int

	build_cmd := strings.Split(RtBuildCmd(fn.Script.Lang), " ")
	log.Debugf("build RUN %s args %v", fn.FuncName, build_cmd[1:])
	code, _, stderr, err := doRun(fn, "build", fn.InstBuild().DepName(), build_cmd[1:])
	log.Debugf("build %s finished", fn.FuncName)
	logSaveEvent(fn, "built", "")
	if err != nil {
		goto out
	}

	if code != 0 || stderr != "" {
		err = fmt.Errorf("stderr: %s", stderr)
		goto out
	}

	err = swk8sRemove(&conf, fn, fn.InstBuild())
	if err != nil {
		log.Errorf("remove deploy error: %s", err.Error())
		goto out
	}

	orig_state = fn.State
	if orig_state == swy.DBFuncStateBld {
		err = dbFuncSetState(fn, swy.DBFuncStateBlt)
		if err == nil {
			err = swk8sRun(&conf, fn, fn.Inst())
		}
	} else {
		err = dbFuncSetState(fn, swy.DBFuncStateRdy)
		if err == nil {
			err = swk8sUpdate(&conf, fn)
		}
	}
	if err != nil {
		goto out_nok8s
	}

	return nil

out:
	swk8sRemove(&conf, fn, fn.InstBuild())
out_nok8s:
	if orig_state == swy.DBFuncStateBld {
		dbFuncSetState(fn, swy.DBFuncStateStl);
	} else {
		// Keep fn ready with the original commit of
		// the repo checked out
		dbFuncSetState(fn, swy.DBFuncStateRdy)
	}
	return fmt.Errorf("buildFunction: %s", err.Error())
}

func notifyPodUpdate(pod *BalancerPod) {
	var err error = nil

	if pod.State == swy.DBPodStateRdy {
		fn, err2 := dbFuncFind(pod.Project, pod.FuncName)
		if err2 != nil {
			err = err2
			goto out
		}

		logSaveEvent(&fn, "POD", fmt.Sprintf("state: %s", fnStates[fn.State]))
		log.Debugf("POD %s stared", pod.UID)
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
	} else {
		log.Debugf("POD %s stopped", pod.UID)
	}
	return

out:
	log.Errorf("POD update notify: %s", err.Error())
}

func handleProjectList(w http.ResponseWriter, r *http.Request) {
	var result []swyapi.ProjectItem
	var params swyapi.ProjectList
	var fns, mws []string
	projects := make(map[string]struct{})

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	fns, mws, err = dbProjectListAll()
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
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func genCookie(fn *FunctionDesc) string {
	h := sha256.New()
	h.Write([]byte(fn.Project + "/" + fn.FuncName))

	return hex.EncodeToString(h.Sum(nil))
}

func getFunctionDesc(p_add *swyapi.FunctionAdd) *FunctionDesc {
	fn := &FunctionDesc {
		Project:	p_add.Project,
		FuncName:	p_add.FuncName,
		Event:		FnEventDesc {
			Source:		p_add.Event.Source,
			CronTab:	p_add.Event.CronTab,
			MwareId:	p_add.Event.MwareId,
			MQueue:		p_add.Event.MQueue,
		},
		Repo:		p_add.Repo,
		Replicas:	p_add.Replicas,
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

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("function/add params %v", params)

	if params.Replicas < 1 {
		params.Replicas = 1
	} else if params.Replicas > 32 {
		params.Replicas = 32
	}

	if params.Project == "" || params.FuncName == "" ||
			params.Repo == "" || params.Script.Lang == "" {
		err = errors.New("Parameters are missed")
		goto out
	}

	err = swy.ValidateProjectAndFuncName(params.Project, params.FuncName)
	if err != nil {
		goto out
	}

	fn = getFunctionDesc(&params)
	if RtBuilding(fn.Script.Lang) {
		fn.State = swy.DBFuncStateBld
	} else {
		fn.State = swy.DBFuncStateQue
	}

	err = dbFuncAdd(fn)
	if err != nil {
		goto out
	}

	// FIXME -- move to /built handler
	err = mwareSetup(&conf, fn.Project, params.Mware, fn)
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

	err = cloneRepo(fn)
	if err != nil {
		goto out_clean_mware
	}

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
	mwareRemove(&conf, fn.Project, fn.Mware)
out_clean_func:
	dbFuncRemove(fn)
out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("function/add error %s", err.Error())
}

func handleFunctionUpdate(w http.ResponseWriter, r *http.Request) {
	var fn FunctionDesc
	var params swyapi.FunctionUpdate

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("function/update params %v", params)

	fn, err = dbFuncFind(params.Project, params.FuncName)
	if err != nil {
		goto out
	}

	// FIXME -- lock other requests :\
	if fn.State != swy.DBFuncStateRdy && fn.State != swy.DBFuncStateStl {
		err = fmt.Errorf("function %s is not running", fn.FuncName)
		goto out
	}

	err = updateRepo(&fn)
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
		log.Debugf("Updating dep")
		err = swk8sUpdate(&conf, &fn)
	}

	if err != nil {
		goto out
	}

	logSaveEvent(&fn, "updated", fmt.Sprintf("to: %s", fn.Commit))
	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("function/update error %s", err.Error())
}

func handleFunctionRemove(w http.ResponseWriter, r *http.Request) {
	var fn FunctionDesc
	var params swyapi.FunctionRemove

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("function/remove params %v", params)

	// Allow to remove function if only we're in known state,
	// otherwise wait for function building to complete
	err = dbFuncSetStateCond(params.Project, params.FuncName,
					swy.DBFuncStateTrm,
					[]int{swy.DBFuncStateRdy, swy.DBFuncStateStl})
	if err != nil {
		goto out
	}

	fn, err = dbFuncFind(params.Project, params.FuncName)
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
	http.Error(w, err.Error(), http.StatusBadRequest)
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

	err = mwareRemove(&conf, fn.Project, fn.Mware)
	if err != nil {
		log.Errorf("remove mware error: %s", err.Error())
	}

	cleanRepo(fn)
	logRemove(fn)
	dbFuncRemove(fn)
}

func doRun(fn *FunctionDesc, event, depname string, args []string) (int, string, string, error) {
	log.Debugf("RUN %s", fn.FuncName)

	var wd_result swyapi.SwdFunctionRunResult
	var resp *http.Response
	var link *BalancerLink
	var resp_body []byte
	var err error

	link = dbBalancerLinkFind(depname)
	if link == nil {
		err = fmt.Errorf("Can't find balancer link %s", depname)
		goto out
	}

	log.Debugf("`- RUN nr replicas %d available %d", link.NumRS, link.CntRS)
	if link.NumRS == 0 {
		err = fmt.Errorf("No available pods found")
		goto out
	}

	resp, err = swy.HTTPMarshalAndPostTimeout("http://" + link.VIP() + "/v1/function/run",
				120,
				&swyapi.SwdFunctionRun{
					PodToken:	fn.Cookie,
					Args:		args,
				})
	if err != nil {
		goto out
	}

	resp_body, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		err = errors.New("Can't read reply")
		goto out
	}

	err = json.Unmarshal(resp_body, &wd_result)
	if err != nil {
		err = fmt.Errorf("Unmarshal error %s", err.Error())
		goto out
	}

	logSaveResult(fn, event, wd_result.Stdout, wd_result.Stderr)
	log.Debugf("`- RUN %s OK: out[%s] err[%s]", fn.FuncName, wd_result.Stdout, wd_result.Stderr)
	return wd_result.Code, wd_result.Stdout, wd_result.Stderr, nil

out:
	return 0, "", "", fmt.Errorf("RUN error %s", err.Error())
}

func handleFunctionInfo(w http.ResponseWriter, r *http.Request) {
	var params swyapi.FunctionID
	var fn FunctionDesc
	var url = ""

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	fn, err = dbFuncFind(params.Project, params.FuncName)
	if err != nil {
		goto out
	}

	if (fn.URLCall) {
		link := dbBalancerLinkFind(fn.Inst().DepName())
		if link != nil {
			url = link.VIP() + "/" + fn.Cookie
		}
	}

	err = swy.HTTPMarshalAndWrite(w,  swyapi.FunctionInfo{
			State:          fnStates[fn.State],
			Mware:          fn.Mware,
			Commit:         fn.Commit,
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
		})
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("logs error %s", err.Error())
}
func handleFunctionLogs(w http.ResponseWriter, r *http.Request) {
	var params swyapi.FunctionID
	var resp []swyapi.FunctionLogEntry
	var logs []DBLogRec

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	logs, err = logGetFor(params.Project, params.FuncName)
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
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("logs error %s", err.Error())
}

func handleFunctionRun(w http.ResponseWriter, r *http.Request) {
	var params swyapi.FunctionRun
	var fn FunctionDesc
	var err error
	var stdout, stderr string
	var code int

	err = swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("handleFunctionRun: params %v", params)

	fn, err = dbFuncFindStates(params.Project, params.FuncName,
			[]int{swy.DBFuncStateRdy, swy.DBFuncStateUpd})
	if err != nil {
		err = errors.New("No such function")
		goto out
	}

	code, stdout, stderr, err = doRun(&fn, "run", fn.Inst().DepName(), params.Args)
	if err != nil {
		goto out
	}

	log.Debugf("handleFunctionRun: OK")
	err = swy.HTTPMarshalAndWrite(w, swyapi.FunctionRunResult{
		Code:		code,
		Stdout:		stdout,
		Stderr:		stderr,
	})
	if err == nil {
		return
	}

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
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
	var recs []FunctionDesc
	var result []swyapi.FunctionItem
	var params swyapi.FunctionList

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	if params.Project == "" {
		err = errors.New("Parameters are missed")
		goto out
	}

	// List all but terminating
	recs, err = dbFuncListAll(params.Project, []int{
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
				FuncName:	v.FuncName,
				State:		fnStates[v.State],
		})
	}

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func handleMwareAdd(w http.ResponseWriter, r *http.Request) {
	var params swyapi.MwareAdd

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("mware/add: params %v", params)

	err = mwareSetup(&conf, params.Project, params.Mware, nil)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("mware/add error: %s", err.Error())
}

func handleMwareList(w http.ResponseWriter, r *http.Request) {
	var result []swyapi.MwareGetItem
	var params swyapi.MwareList
	var mwares []MwareDesc
	var err error

	err = swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	mwares, err = dbMwareGetAll(params.Project)
	if err != nil {
		goto out
	}

	for _, mware := range mwares {
		result = append(result,
			swyapi.MwareGetItem{
				MwareItem: swyapi.MwareItem {
					ID:	mware.MwareID,
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
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("mware/get error: %s", err.Error())
}

func handleMwareRemove(w http.ResponseWriter, r *http.Request) {
	var params swyapi.MwareRemove
	var err error

	err = swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("mware/remove: params %v", params)
	err = mwareRemove(&conf, params.Project, params.MwareIDs)
	if err != nil {
		err = fmt.Errorf("Unable to setup middleware: %s", err.Error())
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("mware/remove error: %s", err.Error())
}

func handleMwareCinfo(w http.ResponseWriter, r *http.Request) {
	var params swyapi.MwareCinfo
	var envs []string

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	envs, err = mwareGetEnv(&conf, params.Project, params.MwId)
	if err != nil {
		goto out
	}

	err = swy.HTTPMarshalAndWrite(w, &swyapi.MwareCinfoResp{ Envs: envs })
	if err != nil {
		goto out
	}
	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
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
