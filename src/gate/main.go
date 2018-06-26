package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"gopkg.in/mgo.v2"
	"encoding/json"
	"encoding/hex"
	"net/http"
	"net/url"
	"errors"
	"os/exec"
	"flag"
	"strings"
	"context"
	"strconv"
	"sync/atomic"
	"time"
	"fmt"
	"os"
	"io/ioutil"
	"gopkg.in/mgo.v2/bson"

	"../apis/apps"
	"../common"
	"../common/http"
	"../common/keystone"
	"../common/secrets"
	"../common/xratelimit"
)

var SwyModeDevel bool
var SwdProxyOK bool
var gateSecrets map[string]string
var gateSecPas []byte

const (
	SwyDefaultProject string		= "default"
	SwyPodStartTmo time.Duration		= 120 * time.Second
	SwyBodyArg string			= "_SWY_BODY_"
	SwyJWTClaimsArg string			= "_SWY_JWT_CLAIMS_"
	SwyDepScaleupRelax time.Duration	= 16 * time.Second
	SwyDepScaledownStep time.Duration	= 8 * time.Second
	SwyTenantLimitsUpdPeriod time.Duration	= 120 * time.Second
	SwyURLEventID				= "000URL"
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
	HTTPS		*swyhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
}

type YAMLConfRabbit struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*swy.XCreds
}

type YAMLConfMaria struct {
	Creds		string			`yaml:"creds"`
	QDB		string			`yaml:"quotdb"`
	c		*swy.XCreds
}

type YAMLConfMongo struct {
	Creds		string			`yaml:"creds"`
	c		*swy.XCreds
}

type YAMLConfPostgres struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	c		*swy.XCreds
}

type YAMLConfS3 struct {
	Creds		string			`yaml:"creds"`
	AdminPort	string			`yaml:"admport"`
	Notify		string			`yaml:"notify"`
	HiddenKeyTmo	uint32			`yaml:"hidden-key-timeout"`
	c		*swy.XCreds
	cn		*swy.XCreds
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
	MaxReplicas	uint32			`yaml:"max-replicas"`
}

type YAMLConf struct {
	DB		string			`yaml:"db"`
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
	Mware		YAMLConfMw		`yaml:"middleware"`
	Runtime		YAMLConfRt		`yaml:"runtime"`
	Wdog		YAMLConfSwd		`yaml:"wdog"`
	Swage		string			`yaml:"swage"`
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
	http.MethodPut,
	http.MethodDelete,
}

type gateContext struct {
	context.Context
	Tenant	string
	ReqId	uint64
	S	*mgo.Session
}

var reqIds uint64

func mkContext(tenant string) (context.Context, func(context.Context)) {
	gatectx := &gateContext{context.Background(), tenant, atomic.AddUint64(&reqIds, 1), session.Copy()}
	return gatectx, func(ctx context.Context) {
				gctx(ctx).S.Close()
			}
}

func gctx(ctx context.Context) *gateContext {
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
	var fns []*FunctionDesc
	var mws []*MwareDesc
	var id *SwoId
	var ferr *swyapi.GateErr

	err := swyhttp.ReadAndUnmarshalReq(r, &par)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	id = ctxSwoId(ctx, par.Project, "")

	fns, err = dbFuncListProj(ctx, id, []string{})
	if err != nil {
		return GateErrD(err)
	}
	for _, fn := range fns {
		id.Name = fn.SwoId.Name
		xerr := removeFunctionId(ctx, &conf, id)
		if xerr != nil {
			ctxlog(ctx).Error("Funciton removal failed: %s", xerr.Message)
			ferr = GateErrM(xerr.Code, "Cannot remove " + id.Name + " function: " + xerr.Message)
		}
	}

	mws, err = dbMwareListProj(ctx, id, "", []string{})
	if err != nil {
		return GateErrD(err)
	}

	for _, mw := range mws {
		id.Name = mw.SwoId.Name
		xerr := mwareRemoveId(ctx, &conf.Mware, id)
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

	err = swyhttp.MarshalAndWrite(w, &result)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func fnFindForReq(ctx context.Context, r *http.Request) (*FunctionDesc, *swyapi.GateErr) {
	fnid := mux.Vars(r)["fid"]
	if !bson.IsObjectIdHex(fnid) {
		return nil, GateErrM(swy.GateBadRequest, "Bad FN ID")
	}

	fn, err := dbFuncFindOne(ctx, ctxObjId(ctx, fnid))
	if err != nil {
		return nil, GateErrD(err)
	}

	return fn, nil
}

func handleFunctionTriggers(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	q := r.URL.Query()

	switch r.Method {
	case "GET":
		ename := q.Get("name")

		var evd []*FnEventDesc
		var err error
		var hasUrl = false

		if ename == "" {
			evd, err = dbListFnEvents(ctx, fn.Cookie)
			if err != nil {
				return GateErrD(err)
			}
		} else {
			var ev *FnEventDesc

			ev, err = dbFuncEventByName(ctx, fn, ename)
			if err != nil {
				return GateErrD(err)
			}

			evd = append(evd, ev)
		}

		var evs []*swyapi.FunctionEvent
		for _, e := range evd {
			if e.Source == "url" {
				hasUrl = true
			}

			evs = append(evs, e.toAPI(fn))
		}

		if fn.URL && !hasUrl {
			evs = append(evs, fn.getURLEvt())
		}

		err = swyhttp.MarshalAndWrite(w, evs)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var evt swyapi.FunctionEvent

		err := swyhttp.ReadAndUnmarshalReq(r, &evt)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		eid, erc := eventsAdd(ctx, fn, &evt)
		if erc != nil {
			return erc
		}

		err = swyhttp.MarshalAndWrite(w, eid)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func handleFunctionAuthCtx(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		err := swyhttp.MarshalAndWrite(w, fn.AuthCtx)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "PUT":
		var ac string

		err := swyhttp.ReadAndUnmarshalReq(r, &ac)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		err = fn.setAuthCtx(ctx, ac)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionSize(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		err := swyhttp.MarshalAndWrite(w, &swyapi.FunctionSize{
			Memory:		fn.Size.Mem,
			Timeout:	fn.Size.Tmo,
			Rate:		fn.Size.Rate,
			Burst:		fn.Size.Burst,
		})
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "PUT":
		var sz swyapi.FunctionSize

		err := swyhttp.ReadAndUnmarshalReq(r, &sz)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		err = swyFixSize(&sz, &conf)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		err = fn.setSize(ctx, &sz)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		var minf []*swyapi.MwareInfo
		for _, mwn := range fn.Mware {
			id := fn.SwoId
			id.Name = mwn
			mw, err := dbMwareGetItem(ctx, &id)

			var mi *swyapi.MwareInfo
			if err == nil {
				mi = mw.toFnInfo(ctx)
			} else {
				mi = &swyapi.MwareInfo{Name: mwn}
			}
			minf = append(minf, mi)
		}

		err := swyhttp.MarshalAndWrite(w, minf)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var mid string

		err := swyhttp.ReadAndUnmarshalReq(r, &mid)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		if !bson.IsObjectIdHex(mid) {
			return GateErrM(swy.GateBadRequest, "Bad MW ID value")
		}

		oid := ctxObjId(ctx, mid)
		oid["project"] = fn.SwoId.Project
		mw, err := dbMwareGetOne(ctx, oid)
		if err != nil {
			return GateErrD(err)
		}

		err = fn.addMware(ctx, mw)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	mid := mux.Vars(r)["mid"]
	if !bson.IsObjectIdHex(mid) {
		return GateErrM(swy.GateBadRequest, "Bad MW ID value")
	}

	oid := ctxObjId(ctx, mid)
	oid["project"] = fn.SwoId.Project
	mw, err := dbMwareGetOne(ctx, oid)
	if err != nil {
		return GateErrD(err)
	}

	switch r.Method {
	case "DELETE":
		err = fn.delMware(ctx, mw)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}


func handleFunctionS3Bs(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		err := swyhttp.MarshalAndWrite(w, fn.S3Buckets)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var bname string
		err := swyhttp.ReadAndUnmarshalReq(r, &bname)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}
		err = fn.addS3Bucket(ctx, bname)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionS3B(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	bname := mux.Vars(r)["bname"]

	switch r.Method {
	case "DELETE":
		err := fn.delS3Bucket(ctx, bname)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionTrigger(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	eid := mux.Vars(r)["eid"]
	if eid == SwyURLEventID {
		if r.Method == "DELETE" {
			return GateErrM(swy.GateBadRequest, "Cannot remove URL from this FN")
		}

		err := swyhttp.MarshalAndWrite(w, fn.getURLEvt())
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

		return nil
	}

	if !bson.IsObjectIdHex(eid) {
		return GateErrM(swy.GateBadRequest, "Bad event ID")
	}

	ed, err := dbFindEvent(ctx, eid)
	if err != nil {
		return GateErrD(err)
	}
	if ed.FnId != fn.Cookie {
		return GateErrC(swy.GateNotFound)
	}

	switch r.Method {
	case "GET":
		err := swyhttp.MarshalAndWrite(w, ed.toAPI(fn))
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "DELETE":
		erc := eventsDelete(ctx, fn, ed)
		if erc != nil {
			return erc
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionWait(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	var wo swyapi.FunctionWait
	err := swyhttp.ReadAndUnmarshalReq(r, &wo)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	timeout := time.Duration(wo.Timeout) * time.Millisecond
	var tmo bool

	if wo.Version != "" {
		ctxlog(ctx).Debugf("function/wait %s -> version >= %s, tmo %d", fn.SwoId.Str(), wo.Version, int(timeout))
		err, tmo = waitFunctionVersion(ctx, fn, wo.Version, timeout)
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

func handleFunctionSources(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		src, cerr := fn.getSources()
		if cerr != nil {
			return cerr
		}

		err := swyhttp.MarshalAndWrite(w, src)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	case "PUT":
		var src swyapi.FunctionSources

		err := swyhttp.ReadAndUnmarshalReq(r, &src)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		cerr := fn.updateSources(ctx, &src)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleTenantStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.TenantStatsReq

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	ten := gctx(ctx).Tenant
	ctxlog(ctx).Debugf("Get FN stats %s", ten)

	td, err := tendatGet(ctx, ten)
	if err != nil {
		return GateErrD(err)
	}

	var resp swyapi.TenantStatsResp
	prev := &td.stats

	if params.Periods > 0 {
		var atst []TenStats

		atst, err = dbTenStatsGetArch(ctx, ten, params.Periods)
		if err != nil {
			return GateErrD(err)
		}

		for i := 0; i < params.Periods && i < len(atst); i++ {
			cur := &atst[i]
			resp.Stats = append(resp.Stats, swyapi.TenantStats{
				Called:		prev.Called - cur.Called,
				GBS:		prev.GBS() - cur.GBS(),
				BytesOut:	prev.BytesOut - cur.BytesOut,
				Till:		prev.TillS(),
				From:		cur.TillS(),
			})
			prev = cur
		}
	}

	resp.Stats = append(resp.Stats, swyapi.TenantStats{
		Called:		prev.Called,
		GBS:		prev.GBS(),
		BytesOut:	prev.BytesOut,
		Till:		prev.TillS(),
	})

	err = swyhttp.MarshalAndWrite(w, resp)
	if err != nil {
		return GateErrE(swy.GateBadResp, err)
	}

	return nil
}

func handleFunctionStats(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		periods := reqPeriods(r.URL.Query())
		if periods < 0 {
			return GateErrC(swy.GateBadRequest)
		}

		stats, cerr := getFunctionStats(ctx, fn, periods)
		if cerr != nil {
			return cerr
		}

		err := swyhttp.MarshalAndWrite(w, &swyapi.FunctionStatsResp{ Stats: stats })
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func handleFunctionLogs(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		logs, err := logGetFor(ctx, &fn.SwoId)
		if err != nil {
			return GateErrD(err)
		}

		var resp []*swyapi.FunctionLogEntry
		for _, loge := range logs {
			resp = append(resp, &swyapi.FunctionLogEntry{
				Event:	loge.Event,
				Ts:	loge.Time.Format(time.UnixDate),
				Text:	loge.Text,
			})
		}

		err = swyhttp.MarshalAndWrite(w, resp)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func fnCallable(fn *FunctionDesc) bool {
	return fn.isURL() && (fn.State == swy.DBFuncStateRdy)
}

func makeArgMap(sopq *statsOpaque, r *http.Request) map[string]string {
	defer r.Body.Close()

	args := make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args[k] = v[0]
		sopq.argsSz += len(k) + len(v[0])
	}

	body, err := ioutil.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		args[SwyBodyArg] = string(body)
		sopq.bodySz = len(body)
	}

	return args
}

var grl *xratelimit.RL

func ratelimited(fmd *FnMemData) bool {
	var frl, trl *xratelimit.RL

	/* Per-function RL first, as it's ... more likely to fail */
	frl = fmd.crl
	if frl != nil && !frl.Get() {
		goto f
	}

	trl = fmd.td.crl
	if trl != nil && !trl.Get() {
		goto t
	}

	if grl != nil && !grl.Get() {
		goto g
	}

	return false

g:
	if trl != nil {
		trl.Put()
	}
t:
	if frl != nil {
		frl.Put()
	}
f:
	return true
}

func rslimited(fmd *FnMemData) bool {
	tmd := fmd.td

	if tmd.GBS_l != 0 {
		if tmd.stats.GBS() - tmd.GBS_o > tmd.GBS_l {
			return true
		}
	}

	if tmd.BOut_l != 0 {
		if tmd.stats.BytesOut - tmd.BOut_o > tmd.BOut_l {
			return true
		}
	}

	return false
}

func handleFunctionCall(w http.ResponseWriter, r *http.Request) {
	var arg_map map[string]string
	var res *swyapi.SwdFunctionRunResult
	var err error
	var code int
	var fmd *FnMemData
	var conn *podConn

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	sopq := statsStart()

	ctx, done := mkContext("::call")
	defer done(ctx)

	fnId := mux.Vars(r)["fnid"]

	fmd, err = memdGet(ctx, fnId)
	if err != nil {
		code = http.StatusInternalServerError
		err = errors.New("Error getting function")
		goto out
	}

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

	if rslimited(fmd) {
		code = http.StatusLocked
		err = errors.New("Resources exhausted")
		goto out
	}

	conn, err = balancerGetConnAny(ctx, fnId, fmd)
	if err != nil {
		code = http.StatusInternalServerError
		err = errors.New("DB error")
		goto out
	}


	arg_map = makeArgMap(sopq, r)

	if fmd.ac != nil {
		var auth_args map[string]string

		auth_args, err = fmd.ac.Verify(r)
		if err != nil {
			code = http.StatusUnauthorized
			goto out
		}

		for k, v:= range(auth_args) {
			arg_map[k] = v
		}
	}

	res, err = doRunConn(ctx, conn, fnId, "call", arg_map)
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

	statsUpdate(fmd, sopq, res)

	return

out:
	http.Error(w, err.Error(), code)
}

func reqPeriods(q url.Values) int {
	aux := q.Get("periods")
	periods := 0
	if aux != "" {
		var err error
		periods, err = strconv.Atoi(aux)
		if err != nil {
			return -1
		}
	}

	return periods
}

func handleFunctions(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	q := r.URL.Query()
	switch r.Method {
	case "GET":
		project := q.Get("project")
		if project == "" {
			project = SwyDefaultProject
		}

		details := (q.Get("details") != "")
		periods := reqPeriods(q)
		if periods < 0 {
			return GateErrC(swy.GateBadRequest)
		}

		var fns []*FunctionDesc
		var err error

		fname := q.Get("name")
		if fname == "" {
			fns, err = dbFuncListProj(ctx, ctxSwoId(ctx, project, ""), q["label"])
			if err != nil {
				return GateErrD(err)
			}
			glog.Debugf("Found %d fns", len(fns))
		} else {
			var fn *FunctionDesc

			fn, err = dbFuncFind(ctx, ctxSwoId(ctx, project, fname))
			if err != nil {
				return GateErrD(err)
			}
			fns = append(fns, fn)
		}

		var ret []*swyapi.FunctionInfo
		for _, fn := range fns {
			fi, cerr := fn.toInfo(ctx, details, periods)
			if cerr != nil {
				return cerr
			}

			ret = append(ret, fi)
		}

		err = swyhttp.MarshalAndWrite(w, &ret)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var params swyapi.FunctionAdd

		err := swyhttp.ReadAndUnmarshalReq(r, &params)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		err = swyFixSize(&params.Size, &conf)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		if params.Name == "" {
			return GateErrM(swy.GateBadRequest, "No function name")
		}
		if params.Code.Lang == "" {
			return GateErrM(swy.GateBadRequest, "No language specified")
		}

		err = validateFuncName(&params)
		if err != nil {
			return GateErrM(swy.GateBadRequest, "Bad project/function name")
		}

		if !RtLangEnabled(params.Code.Lang) {
			return GateErrM(swy.GateBadRequest, "Unsupported language")
		}

		for _, env := range(params.Code.Env) {
			if strings.HasPrefix(env, "SWD_") {
				return GateErrM(swy.GateBadRequest, "Environment var cannot start with SWD_")
			}
		}

		id := ctxSwoId(ctx, params.Project, params.Name)
		fid, cerr := addFunction(ctx, &conf, getFunctionDesc(id, &params))
		if cerr != nil {
			return cerr

		}

		err = swyhttp.MarshalAndWrite(w, fid)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func handleFunction(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	switch r.Method {
	case "GET":
		periods := reqPeriods(r.URL.Query())
		if periods < 0 {
			return GateErrC(swy.GateBadRequest)
		}

		fi, cerr := fn.toInfo(ctx, true, periods)
		if cerr != nil {
			return cerr
		}

		err := swyhttp.MarshalAndWrite(w, fi)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "PUT":
		var fu swyapi.FunctionUpdate

		err := swyhttp.ReadAndUnmarshalReq(r, &fu)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		if fu.UserData != nil {
			err = fn.setUserData(ctx, *fu.UserData)
			if err != nil {
				return GateErrE(swy.GateGenErr, err)
			}
		}

		if fu.State != "" {
			cerr := setFunctionState(ctx, &conf, fn, fu.State)
			if cerr != nil {
				return cerr
			}
		}

	case "DELETE":
		cerr := removeFunction(ctx, &conf, fn)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleFunctionRun(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	fn, cerr := fnFindForReq(ctx, r)
	if cerr != nil {
		return cerr
	}

	var params swyapi.FunctionRun
	var res *swyapi.SwdFunctionRunResult

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		return GateErrE(swy.GateBadRequest, err)
	}

	if fn.State != swy.DBFuncStateRdy {
		return GateErrM(swy.GateNotAvail, "Function not ready (yet)")
	}

	conn, errc := balancerGetConnExact(ctx, fn.Cookie, fn.Src.Version)
	if errc != nil {
		return errc
	}

	res, err = doRunConn(ctx, conn, fn.Cookie, "run", params.Args)
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	if fn.SwoId.Project == "test" {
		var fort []byte
		fort, err = exec.Command("fortune", "fortunes").Output()
		if err == nil {
			res.Stdout = string(fort)
		}
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

func handleMwares(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	q := r.URL.Query()

	switch r.Method {
	case "GET":
		project := q.Get("project")
		if project == "" {
			project = SwyDefaultProject
		}

		details := (q.Get("details") != "")
		mwtype := q.Get("type")

		var mws []*MwareDesc
		var err error

		mname := q.Get("name")
		if mname == "" {
			mws, err = dbMwareListProj(ctx, ctxSwoId(ctx, project, ""), mwtype, q["label"])
			if err != nil {
				return GateErrD(err)
			}
		} else {
			var mw *MwareDesc

			mw, err = dbMwareGetItem(ctx, ctxSwoId(ctx, project, mname))
			if err != nil {
				return GateErrD(err)
			}
			mws = append(mws, mw)
		}

		var ret []*swyapi.MwareInfo
		for _, mw := range mws {
			mi, cerr := mw.toInfo(ctx, &conf.Mware, details)
			if cerr != nil {
				return cerr
			}

			ret = append(ret, mi)
		}

		err = swyhttp.MarshalAndWrite(w, &ret)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var params swyapi.MwareAdd

		err := swyhttp.ReadAndUnmarshalReq(r, &params)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		ctxlog(ctx).Debugf("mware/add: %s params %v", gctx(ctx).Tenant, params)

		id := ctxSwoId(ctx, params.Project, params.Name)
		mid, cerr := mwareSetup(ctx, &conf.Mware, getMwareDesc(id, &params))
		if cerr != nil {
			return cerr
		}

		err = swyhttp.MarshalAndWrite(w, mid)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

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

func handleS3Access(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	var params swyapi.S3Access

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

func handleDeployments(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	q := r.URL.Query()

	switch r.Method {
	case "GET":
		var deps []*DeployDesc
		var err error

		project := q.Get("project")
		if project == "" {
			project = SwyDefaultProject
		}

		dname := q.Get("name")
		if dname == "" {
			deps, err = dbDeployListProj(ctx, ctxSwoId(ctx, project, ""), q["label"])
			if err != nil {
				return GateErrD(err)
			}
		} else {
			var dep *DeployDesc

			dep, err = dbDeployGet(ctx, bson.M{"cookie": ctxSwoId(ctx, project, dname).Cookie()})
			if err != nil {
				return GateErrD(err)
			}
			deps = append(deps, dep)
		}

		var dis []*swyapi.DeployInfo
		for _, d := range deps {
			di, cerr := d.toInfo(ctx, false)
			if cerr != nil {
				return cerr
			}

			dis = append(dis, di)
		}

		err = swyhttp.MarshalAndWrite(w, dis)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var ds swyapi.DeployStart

		err := swyhttp.ReadAndUnmarshalReq(r, &ds)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		dd := getDeployDesc(ctxSwoId(ctx, ds.Project, ds.Name))
		cerr := dd.getItems(ds.Items)
		if cerr != nil {
			return cerr
		}

		did, cerr := deployStart(ctx, dd)
		if cerr != nil {
			return cerr
		}

		err = swyhttp.MarshalAndWrite(w, &did)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func handleDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	did := mux.Vars(r)["did"]
	if !bson.IsObjectIdHex(did) {
		return GateErrM(swy.GateBadRequest, "Bad deploy ID value")
	}

	dd, err := dbDeployGet(ctx, ctxObjId(ctx, did))
	if err != nil {
		return GateErrD(err)
	}

	return handleOneDeployment(ctx, w, r, dd)
}

func handleOneDeployment(ctx context.Context, w http.ResponseWriter, r *http.Request, dd *DeployDesc) *swyapi.GateErr {
	switch r.Method {
	case "GET":
		di, cerr := dd.toInfo(ctx, true)
		if cerr != nil {
			return cerr
		}

		err := swyhttp.MarshalAndWrite(w, di)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "DELETE":
		cerr := deployStop(ctx, dd)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
}

func handleAuths(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	q := r.URL.Query()
	project := q.Get("project")
	if project == "" {
		project = SwyDefaultProject
	}

	switch r.Method {
	case "GET":
		deps, err := dbDeployList(ctx, bson.M{
					"tennant":	gctx(ctx).Tenant,
					"project":	project,
					"labels":	"auth",
				})
		if err != nil {
			return GateErrD(err)
		}

		var auths []*swyapi.AuthInfo
		for _, d := range deps {
			auths = append(auths, &swyapi.AuthInfo{ Id: d.ObjID.Hex(), Name: d.SwoId.Name })
		}

		err = swyhttp.MarshalAndWrite(w, auths)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "POST":
		var aa swyapi.AuthAdd

		err := swyhttp.ReadAndUnmarshalReq(r, &aa)
		if err != nil {
			return GateErrE(swy.GateBadRequest, err)
		}

		if aa.Type != "" && aa.Type != "jwt" {
			return GateErrM(swy.GateBadRequest, "No such auth type")
		}

		dd := getDeployDesc(ctxSwoId(ctx, project, aa.Name))
		dd.Labels = []string{ "auth" }
		dd.getItems([]*swyapi.DeployItem {
			{ Mware: &swyapi.MwareAdd{ Name: aa.Name + "_jwt", Type: "authjwt" } },
			{ Mware: &swyapi.MwareAdd{ Name: aa.Name + "_mgo", Type: "mongo" } },
			{ Function: &swyapi.FunctionAdd {
				Name: aa.Name + "_um",
				Code: swyapi.FunctionCode {
					Lang: "golang",
					Env: []string{ "SWIFTY_AUTH_NAME=" + aa.Name },
				},
				Sources: swyapi.FunctionSources {
					Type: "swage",
					Swage: &swyapi.FunctionSwage { Template: "umjwt0", },
				},
				Mware: []string { aa.Name + "_jwt", aa.Name + "_mgo" },
				Url: true,
			}},
		})

		did, cerr := deployStart(ctx, dd)
		if cerr != nil {
			return cerr
		}

		err = swyhttp.MarshalAndWrite(w, &did)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}
	}

	return nil
}

func handleAuth(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	did := mux.Vars(r)["aid"]
	if !bson.IsObjectIdHex(did) {
		return GateErrM(swy.GateBadRequest, "Bad auth deploy ID value")
	}

	oid := ctxObjId(ctx, did)
	oid["labels"] = "auth"
	ad, err := dbDeployGet(ctx, oid)
	if err != nil {
		return GateErrD(err)
	}

	return handleOneDeployment(ctx, w, r, ad)
}

func handleMware(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr {
	mid := mux.Vars(r)["mid"]
	if !bson.IsObjectIdHex(mid) {
		return GateErrM(swy.GateBadRequest, "Bad mware ID value")
	}

	mw, err := dbMwareGetOne(ctx, ctxObjId(ctx, mid))
	if err != nil {
		return GateErrD(err)
	}

	switch r.Method {
	case "GET":
		mi, cerr := mw.toInfo(ctx, &conf.Mware, true)
		if cerr != nil {
			return cerr
		}

		err = swyhttp.MarshalAndWrite(w, mi)
		if err != nil {
			return GateErrE(swy.GateBadResp, err)
		}

	case "DELETE":
		cerr := mwareRemove(ctx, &conf.Mware, mw)
		if cerr != nil {
			return cerr
		}

		w.WriteHeader(http.StatusOK)
	}

	return nil
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

func genReqHandler(cb func(ctx context.Context, w http.ResponseWriter, r *http.Request) *swyapi.GateErr) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

		tennant, code, err := handleGenericReq(r)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}

		ctx, done := mkContext(tennant)
		defer done(ctx)

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
	conf.Mware.Maria.c = swy.ParseXCreds(conf.Mware.Maria.Creds)
	conf.Mware.Maria.c.Resolve()

	conf.Mware.Rabbit.c = swy.ParseXCreds(conf.Mware.Rabbit.Creds)
	conf.Mware.Rabbit.c.Resolve()

	conf.Mware.Mongo.c = swy.ParseXCreds(conf.Mware.Mongo.Creds)
	conf.Mware.Mongo.c.Resolve()

	conf.Mware.Postgres.c = swy.ParseXCreds(conf.Mware.Postgres.Creds)
	conf.Mware.Postgres.c.Resolve()

	conf.Mware.S3.c = swy.ParseXCreds(conf.Mware.S3.Creds)
	conf.Mware.S3.c.Resolve()

	conf.Mware.S3.cn = swy.ParseXCreds(conf.Mware.S3.Notify)
	conf.Mware.S3.cn.Resolve()
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

	if Flavor == "lite" {
		grl = xratelimit.MakeRL(0, 1000)
	}

	glog.Debugf("Flavor: %s", Flavor)
	glog.Debugf("PROXY: %v", SwdProxyOK)

	r := mux.NewRouter()
	r.HandleFunc("/v1/login",		handleUserLogin).Methods("POST", "OPTIONS")
	r.Handle("/v1/stats",			genReqHandler(handleTenantStats)).Methods("POST", "OPTIONS")
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
	r.Handle("/v1/functions/{fid}/middleware", genReqHandler(handleFunctionMwares)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/middleware/{mid}", genReqHandler(handleFunctionMware)).Methods("DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/s3buckets",  genReqHandler(handleFunctionS3Bs)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/functions/{fid}/s3buckets/{bname}",  genReqHandler(handleFunctionS3B)).Methods("DELETE", "OPTIONS")
	r.Handle("/v1/functions/{fid}/wait",	genReqHandler(handleFunctionWait)).Methods("POST", "OPTIONS")

	r.Handle("/v1/middleware",		genReqHandler(handleMwares)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/middleware/{mid}",	genReqHandler(handleMware)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/s3/access",		genReqHandler(handleS3Access)).Methods("POST", "OPTIONS")

	r.Handle("/v1/auths",			genReqHandler(handleAuths)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/auths/{aid}",		genReqHandler(handleAuth)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/deployments",		genReqHandler(handleDeployments)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/deployments/{did}",	genReqHandler(handleDeployment)).Methods("GET", "DELETE", "OPTIONS")

	r.Handle("/v1/info/langs",		genReqHandler(handleLanguages)).Methods("POST", "OPTIONS")
	r.Handle("/v1/info/mwares",		genReqHandler(handleMwareTypes)).Methods("POST", "OPTIONS")

	r.HandleFunc("/call/{fnid}",			handleFunctionCall).Methods("POST", "OPTIONS")

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

	err = BuilderInit(&conf)
	if err != nil {
		glog.Fatalf("Can't set up builder: %s", err.Error())
	}

	err = DeployInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't set up deploys: %s", err.Error())
	}

	err = PrometheusInit(ctx, &conf)
	if err != nil {
		glog.Fatalf("Can't set up prometheus: %s", err.Error())
	}

	done(ctx)

	err = swyhttp.ListenAndServe(
		&http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Addr,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, SwyModeDevel, func(s string) { glog.Debugf(s) })
	if err != nil {
		glog.Errorf("ListenAndServe: %s", err.Error())
	}

	dbDisconnect()
}
