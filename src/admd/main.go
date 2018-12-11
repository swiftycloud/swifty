/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"go.uber.org/zap"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"github.com/gorilla/mux"

	"context"
	"strings"
	"net/http"
	"flag"
	"time"
	"os"
	"fmt"
	"errors"

	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/keystone"
	"swifty/common/secrets"
	"swifty/common/xrest"
	"swifty/common/xrest/sysctl"
)

var admdSecrets xsecret.Store

type YAMLConfDaemon struct {
	Address		string			`yaml:"address"`
	HTTPS		*xhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConf struct {
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Gate		string			`yaml:"gate"`
	Keystone	string			`yaml:"keystone"`
	DB		string			`yaml:"db"`
	DefaultPlan	string			`yaml:"default_plan"`
	kc		*xh.XCreds
}

func init() {
	sysctl.AddStringSysctl("default_plan_name", &conf.DefaultPlan)
	sysctl.AddRoSysctl("admd_version", func() string { return Version })
}

var conf YAMLConf
var gatesrv *http.Server
var log *zap.SugaredLogger

var CORS_Headers = []string {
	"Content-Type",
	"Content-Length",
	"X-Subject-Token",
	"X-Auth-Token",
	"X-Relay-Tennant",
}

var CORS_Methods = []string {
	http.MethodPost,
	http.MethodGet,
	http.MethodDelete,
	http.MethodPut,
}

func admdErrE(err error, code int) *xrest.ReqErr {
	return &xrest.ReqErr{
		Message: err.Error(),
		Code: uint(code),
	}
}

func admdErrM(err string, code int) *xrest.ReqErr {
	return &xrest.ReqErr{
		Message: err,
		Code: uint(code),
	}
}

func admdErr(code int) *xrest.ReqErr {
	return &xrest.ReqErr{
		Code: uint(code),
	}
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

	log.Debugf("Try to login user %s", params.UserName)

	token, td.Expires, err = xkst.KeystoneAuthWithPass(conf.kc.Addr(), conf.kc.Domn, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = xh.MakeEndpoint(conf.Gate)
	log.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

	w.Header().Set("X-Subject-Token", token)
	err = xhttp.Respond(w, &td)
	if err != nil {
		resp = http.StatusInternalServerError
		goto out
	}

	return

out:
	log.Warnf("Failed login attempt from %s", r.RemoteAddr)
	http.Error(w, err.Error(), resp)
}

func handleAdmdReq(r *http.Request) (*xkst.KeystoneTokenData, int, error) {
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return nil, http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	td, code := xkst.KeystoneGetTokenData(conf.kc.Addr(), token)
	if code != 0 {
		return nil, code, fmt.Errorf("Keystone auth error")
	}

	return td, 0, nil
}

func genReqHandler(cb func(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) {
			return
		}

		td, code, err := handleAdmdReq(r)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}

		ctx := context.Background()

		log.Debugf("REQ %s %s.%s", td.Project.Name, r.Method, r.URL.Path)

		cerr := cb(ctx, w, r, td)
		if cerr != nil {
			log.Errorf("Error: %s", cerr.Message)
			http.Error(w, cerr.Message, int(cerr.Code))
		}
	})
}

func handleUser(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	switch r.Method {
	case "GET":
		return handleUserInfo(ctx, w, r, uid, td)
	case "PUT":
		return handleUserUpdate(ctx, w, r, uid, td)
	case "DELETE":
		return handleDelUser(ctx, w, r, uid, td)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handlePlan(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	pid := mux.Vars(r)["pid"]
	if !bson.IsObjectIdHex(pid) {
		return admdErrM("Bad plan ID value", http.StatusBadRequest)
	}

	p_id := bson.ObjectIdHex(pid)
	switch r.Method {
	case "GET":
		return handlePlanInfo(ctx, w, r, p_id, td)
	case "PUT":
		return handlePlanUpdate(ctx, w, r, p_id, td)
	case "DELETE":
		return handleDelPlan(ctx, w, r, p_id, td)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handleUserUpdate(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.ModUser
	var rui *swyapi.UserInfo
	var err error

	code := http.StatusForbidden

	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UserRole) {
			goto out
		}
	} else {
		if !xkst.HasRole(td, swyapi.AdminRole) {
			goto out
		}
	}

	code = http.StatusBadRequest
	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	rui, err = getUserInfo(conf.kc, uid, false)
	if err != nil {
		log.Errorf("GetUserDesc: %s", err.Error())
		goto out
	}

	if params.Enabled != nil {
		err = ksSetUserEnabled(conf.kc, uid, *params.Enabled)
		if err != nil {
			goto out
		}

		rui.Enabled = *params.Enabled
	}

	err = xhttp.Respond(w, rui)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func handleUserInfo(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var rui *swyapi.UserInfo
	var err error

	code := http.StatusForbidden

	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UserRole) {
			goto out
		}
	} else {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.MonitorRole) {
			goto out
		}
	}

	code = http.StatusBadRequest
	rui, err = getUserInfo(conf.kc, uid, true)
	if err != nil {
		log.Errorf("GetUserDesc: %s", err.Error())
		goto out
	}

	err = xhttp.Respond(w, rui)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func handlePlanInfo(ctx context.Context, w http.ResponseWriter, r *http.Request, pid bson.ObjectId, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var pl *PlanLimits
	var err error

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusInternalServerError
	pl, err = dbGetPlanLimits(ses, pid)
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, pl.toInfo())
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func handlePlanUpdate(ctx context.Context, w http.ResponseWriter, r *http.Request, pid bson.ObjectId, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.PlanLimits
	var pl *PlanLimits
	var err error

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusBadRequest
	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	pl, err = dbGetPlanLimits(ses, pid)
	if err != nil {
		goto out
	}

	if params.Descr != "" {
		pl.Descr = params.Descr
	}

	patchFnLimits(&pl.Fn, params.Fn)
	patchPkgLimits(&pl.Pkg, params.Pkg)
	patchRepoLimits(&pl.Repo, params.Repo)
	patchMwareLimits(pl.Mware, params.Mware)
	patchS3Limits(&pl.S3, params.S3)

	err = dbSetPlanLimits(ses, pl)
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, pl.toInfo())
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func handleSysctls(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	if !xkst.HasRole(td, swyapi.AdminRole) {
		return admdErr(http.StatusForbidden)
	}

	return xrest.HandleMany(ctx, w, r, sysctl.Sysctls{}, nil)
}

func handleSysctl(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	if !xkst.HasRole(td, swyapi.AdminRole) {
		return admdErr(http.StatusForbidden)
	}

	var upd string
	return xrest.HandleOne(ctx, w, r, sysctl.Sysctls{}, &upd)
}

func handleUsers(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	switch r.Method {
	case "GET":
		return handleListUsers(ctx, w, r, td)
	case "POST":
		return handleAddUser(ctx, w, r, td)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handlePlans(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	switch r.Method {
	case "GET":
		return handleListPlans(ctx, w, r, td)
	case "POST":
		return handleAddPlan(ctx, w, r, td)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handleListUsers(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var result []*swyapi.UserInfo
	var err error

	code := http.StatusInternalServerError
	if xkst.HasRole(td, swyapi.AdminRole, swyapi.MonitorRole) {
		result, err = listUsers(conf.kc)
		if err != nil {
			goto out
		}
	} else if xkst.HasRole(td, swyapi.UserRole) {
		var ui *swyapi.UserInfo
		ui, err = getUserInfo(conf.kc, td.User.Id, false)
		if err != nil {
			goto out
		}
		result = append(result, ui)
	} else {
		code = http.StatusForbidden
		err = errors.New("Not swifty role")
		goto out
	}

	err = xhttp.Respond(w, result)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func (pl *PlanLimits)toInfo() *swyapi.PlanLimits {
	return &swyapi.PlanLimits{
		Id:	pl.ObjID.Hex(),
		Name:	pl.Name,
		Descr:	pl.Descr,
		Fn:	pl.Fn,
		Pkg:	pl.Pkg,
		Repo:	pl.Repo,
		Mware:	pl.Mware,
		S3:	pl.S3,
	}
}

func (pl *PlanLimits)toUserLimits(uid string) *swyapi.UserLimits {
	return &swyapi.UserLimits{
		UId:    uid,
		PlanId: pl.ObjID.Hex(),
		PlanNm: pl.Name,
		Fn:	pl.Fn,
		Pkg:	pl.Pkg,
		Repo:	pl.Repo,
		Mware:	pl.Mware,
		S3:	pl.S3,
	}
}

func handleListPlans(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var result []*swyapi.PlanLimits

	code := http.StatusInternalServerError
	ses := session.Copy()
	defer ses.Close()

	pls, err := dbListPlanLimits(ses)
	if err != nil {
		goto out
	}

	for _, pl := range(pls) {
		result = append(result, pl.toInfo())
	}

	err = xhttp.Respond(w, result)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func makeGateReq(gate, tennant, addr string, in interface{}, out interface{}, authToken string) error {
	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Address: "http://" + gate + "/v1/" + addr,
				Headers: map[string]string {
					"X-Auth-Token": authToken,
					"X-Relay-Tennant": tennant,
				},
			}, in)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bad response from server: %s", string(resp.Status))
	}

	if out != nil {
		err = xhttp.RResp(resp, out)
		if err != nil {
			return fmt.Errorf("Bad response body: %s", err.Error())
		}
	}

	return nil
}

func tryRemoveAllProjects(uid string, authToken string) error {
	var projects []swyapi.ProjectItem
	err := makeGateReq(conf.Gate, uid, "project/list", &swyapi.ProjectList{}, &projects, authToken)
	if err != nil {
		return fmt.Errorf("Can't list projects: %s", err.Error())
	}

	for _, prj := range projects {
		derr := makeGateReq(conf.Gate, uid, "project/del", &swyapi.ProjectDel{Project: prj.Project}, nil, authToken)
		if derr != nil {
			err = fmt.Errorf("Can't delete project %s: %s", prj.Project, derr.Error())
		}
	}

	return err
}

func handleDelUser(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var rui *swyapi.UserInfo
	var err error

	ses := session.Copy()
	defer ses.Close()

	/* User can be deleted by admin or self only. Admin
	 * cannot delete self */
	code := http.StatusForbidden
	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.UserRole) ||
				xkst.HasRole(td, swyapi.AdminRole) {
			err = errors.New("Not authorized")
			goto out
		}
	} else {
		if !xkst.HasRole(td, swyapi.AdminRole) {
			err = errors.New("Not an admin")
			goto out
		}
	}

	code = http.StatusInternalServerError
	rui, err = getUserInfo(conf.kc, uid, false)
	if err != nil {
		goto out
	}

	code = http.StatusServiceUnavailable
	err = tryRemoveAllProjects(rui.UId, r.Header.Get("X-Auth-Token"))
	if err != nil {
		goto out
	}

	err = dbDelUserLimits(ses, &conf, rui.UId)
	if err != nil {
		goto out
	}

	log.Debugf("Del user %s", rui.UId)
	code = http.StatusBadRequest
	err = ksDelUserAndProject(conf.kc, uid, rui.UId)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusNoContent)

	return nil

out:
	return admdErrE(err, code)
}

func handleDelPlan(ctx context.Context, w http.ResponseWriter, r *http.Request, pid bson.ObjectId, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var err error

	ses := session.Copy()
	defer ses.Close()

	/* User can be deleted by admin or self only. Admin
	 * cannot delete self */
	code := http.StatusForbidden
	if !xkst.HasRole(td, swyapi.AdminRole) {
		err = errors.New("Not an admin")
		goto out
	}

	err = dbDelPlanLimits(ses, pid)
	code = http.StatusInternalServerError
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusNoContent)

	return nil

out:
	return admdErrE(err, code)
}

func selectPlan(ses *mgo.Session, params *swyapi.AddUser) (*PlanLimits, error) {

	if params.PlanId == "" {
		pn := params.PlanNm
		if pn == "" {
			pn = conf.DefaultPlan
			if pn == "" {
				return nil, nil
			}
		}

		return dbGetPlanLimitsByName(ses, pn)
	}

	if !bson.IsObjectIdHex(params.PlanId) {
		return nil, errors.New("Bad plan ID value")
	}

	plim, err := dbGetPlanLimits(ses, bson.ObjectIdHex(params.PlanId))
	if err != nil {
		return nil, err
	}

	if params.PlanNm != "" && plim.Name != params.PlanNm {
		return  nil, errors.New("Plang name mismatch")
	}

	return plim, nil
}

func handleAddUser(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.AddUser
	var kid string
	var err error
	var plim *PlanLimits

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusBadRequest
	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	/* User can be added by admin or UI */
	code = http.StatusForbidden
	if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UIRole) {
		err = errors.New("Only admin or UI may add users")
		goto out
	}

	if strings.HasPrefix(params.UId, ".") {
		err = errors.New("Bad ID for a user")
		goto out
	}

	log.Debugf("Add user %v", params)
	code = http.StatusBadRequest

	plim, err = selectPlan(ses, &params)
	if err != nil {
		goto out
	}

	if plim != nil {
		err = dbSetUserLimits(ses, &conf, plim.toUserLimits(params.UId))
		if err != nil {
			goto out
		}
	}

	kid, err, code = ksAddUserAndProject(conf.kc, &params)
	if err != nil {
		if code == http.StatusConflict {
			err = errors.New("User already exists")
		} else {
			log.Errorf("Can't add user: %s/%d", err.Error(), code)
			if code == -1 {
				code = http.StatusInternalServerError
			}
			err = errors.New("Error registering user")
		}
		dbDelUserLimits(ses, &conf, params.UId)
		goto out
	}

	err = xhttp.Respond2(w, &swyapi.UserInfo{
			ID:		kid,
			UId:		params.UId,
			Name:		params.Name,
			Roles:		[]string{swyapi.UserRole},
		}, http.StatusCreated)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

type PlanLimits struct {
	ObjID	bson.ObjectId		`bson:"_id,omitempty"`
	Name	string			`bson:"name"`
	Descr	string			`bson:"descr"`
	Fn	*swyapi.FunctionLimits	`bson:"function,omitempty"`
	Pkg	*swyapi.PackagesLimits	`bson:"packages,omitempty"`
	Repo	*swyapi.ReposLimits	`bson:"repos,omitempty"`
	Mware	map[string]*swyapi.MwareLimits	`bson:"mware"`
	S3	*swyapi.S3Limits	`bson:"s3,omitempty"`
}

func handleAddPlan(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.PlanLimits
	var pl *PlanLimits
	var err error

	ses := session.Copy()
	defer ses.Close()

	/* User can be added by admin or UI */
	code := http.StatusForbidden
	if !xkst.HasRole(td, swyapi.AdminRole) {
		err = errors.New("Only admin may add plans")
		goto out
	}

	code = http.StatusBadRequest
	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	if params.Name == "" {
		err = errors.New("Bad name for a plan")
		goto out
	}

	pl = &PlanLimits {
		ObjID:	bson.NewObjectId(),
		Name:	params.Name,
		Descr:	params.Descr,
		Fn:	params.Fn,
		Pkg:	params.Pkg,
		Repo:	params.Repo,
		Mware:	params.Mware,
		S3:	params.S3,
	}
	code = http.StatusInternalServerError
	err = dbAddPlanLimits(ses, pl)
	if err != nil {
		goto out
	}

	params.Id = pl.ObjID.Hex()
	err = xhttp.Respond2(w, &params, http.StatusCreated)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func patchFnLimits(tgt **swyapi.FunctionLimits, from *swyapi.FunctionLimits) {
	if from == nil {
		return
	}

	into := *tgt
	if into == nil {
		*tgt = from
		return
	}

	if from.Rate != 0 {
		into.Rate = from.Rate
		into.Burst = from.Burst
	}

	if from.Max != 0 {
		into.Max = from.Max
	}

	if from.GBS != 0 {
		into.GBS = from.GBS
	}

	if from.BytesOut != 0 {
		into.BytesOut = from.BytesOut
	}
}

func patchPkgLimits(tgt **swyapi.PackagesLimits, from *swyapi.PackagesLimits) {
	if from == nil {
		return
	}

	into := *tgt
	if into == nil {
		*tgt = from
		return
	}

	if from.DiskSizeK != 0 {
		into.DiskSizeK = from.DiskSizeK
	}
}

func patchRepoLimits(tgt **swyapi.ReposLimits, from *swyapi.ReposLimits) {
	if from == nil {
		return
	}

	into := *tgt
	if into == nil {
		*tgt = from
		return
	}

	if from.Number != 0 {
		into.Number = from.Number
	}
}

func patchMwareLimits(into map[string]*swyapi.MwareLimits, from map[string]*swyapi.MwareLimits) {
	for m, lf := range from {
		lt, ok := into[m]
		if !ok {
			into[m] = lf
		} else {
			if lf.Number != 0 {
				lt.Number = lf.Number
			}
		}
	}
}

func patchS3Limits(tgt **swyapi.S3Limits, from *swyapi.S3Limits) {
	if from == nil {
		return
	}

	into := *tgt
	if into == nil {
		*tgt = from
		return
	}

	if from.SpaceMB != 0 {
		into.SpaceMB = from.SpaceMB
	}

	if from.DownloadMB != 0 {
		into.DownloadMB = from.DownloadMB
	}
}

func handleSetLimits(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.UserLimits
	var rui *swyapi.UserInfo
	var err error
	var plim *PlanLimits

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusForbidden
	if !xkst.HasRole(td, swyapi.AdminRole) {
		err = errors.New("Only admin may change user limits")
		goto out
	}

	code = http.StatusBadRequest
	err = xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	if params.PlanId != "" {
		if !bson.IsObjectIdHex(params.PlanId) {
			err = errors.New("Bad plan ID value")
			goto out
		}

		plim, err = dbGetPlanLimits(ses, bson.ObjectIdHex(params.PlanId))
		if err != nil {
			goto out
		}
	} else {
		plim = &PlanLimits{}
	}

	patchFnLimits(&plim.Fn, params.Fn)
	patchPkgLimits(&plim.Pkg, params.Pkg)
	patchRepoLimits(&plim.Repo, params.Repo)
	patchMwareLimits(plim.Mware, params.Mware)
	patchS3Limits(&plim.S3, params.S3)

	code = http.StatusInternalServerError
	rui, err = getUserInfo(conf.kc, uid, false)
	if err != nil {
		goto out
	}

	err = dbSetUserLimits(ses, &conf, plim.toUserLimits(rui.UId))
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return nil

out:
	return admdErrE(err, code)
}

func handleGetLimits(ctx context.Context, w http.ResponseWriter, uid string, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	ses := session.Copy()
	defer ses.Close()

	var ulim *swyapi.UserLimits
	var rui *swyapi.UserInfo
	var err error

	code := http.StatusForbidden
	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UserRole) {
			goto out
		}
	} else {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.MonitorRole) {
			goto out
		}
	}

	code = http.StatusInternalServerError
	rui, err = getUserInfo(conf.kc, uid, false)
	if err != nil {
		goto out
	}

	ulim, err = dbGetUserLimits(ses, &conf, rui.UId)
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, ulim)
	if err != nil {
		goto out
	}

	return nil

out:
	return admdErrE(err, code)
}

func handleSetPassword(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	var params swyapi.ChangePass

	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	code := http.StatusBadRequest
	err := xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	if params.Password == "" {
		err = fmt.Errorf("Empty password")
		goto out
	}

	code = http.StatusForbidden
	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole) {
			if !xkst.HasRole(td, swyapi.UserRole) {
				err = errors.New("Not a swifty user")
				goto out
			}

			if params.CPassword == "" {
				err = errors.New("Old password required")
				goto out
			}
		}
	} else {
		if !xkst.HasRole(td, swyapi.AdminRole) {
			err = errors.New("Not an admin")
			goto out
		}
	}

	log.Debugf("Change pass to %s", uid)
	err = ksChangeUserPass(conf.kc, uid, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusCreated)

	return nil

out:
	return admdErrE(err, code)
}

func handleUserLimits(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	if r.Method == "PUT" {
		return handleSetLimits(ctx, w, r, uid, td)
	} else {
		return handleGetLimits(ctx, w, uid, td)
	}
}

func handleUserCreds(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UserRole) {
			return admdErr(http.StatusForbidden)
		}
	} else {
		if r.Method != "GET" {
			/* Keystone doesn't allow such anyway */
			return admdErr(http.StatusForbidden)
		}

		if !xkst.HasRole(td, swyapi.AdminRole) {
			return admdErr(http.StatusForbidden)
		}
	}

	switch r.Method {
	case "GET":
		return handleListCreds(ctx, w, r, uid)
	case "POST":
		return handleCreateCreds(ctx, w, r, uid)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handleUserCred(ctx context.Context, w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) *xrest.ReqErr {
	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	if uid == td.User.Id {
		if !xkst.HasRole(td, swyapi.AdminRole, swyapi.UserRole) {
			return admdErr(http.StatusForbidden)
		}
	} else {
		if r.Method != "GET" {
			/* Keystone doesn't allow such */
			return admdErr(http.StatusForbidden)
		}

		if !xkst.HasRole(td, swyapi.AdminRole) {
			return admdErr(http.StatusForbidden)
		}
	}

	key := mux.Vars(r)["key"]

	switch r.Method {
	case "GET":
		return handleShowCred(ctx, w, r, uid, key)
	case "DELETE":
		return handleDeleteCred(ctx, w, r, uid, key)
	}

	return admdErr(http.StatusMethodNotAllowed)
}

func handleCreateCreds(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string) *xrest.ReqErr {
	var params swyapi.Creds

	err := xhttp.RReq(r, &params)
	if err != nil {
		return admdErrE(err, http.StatusBadRequest)
	}

	err = ksCreateCreds(conf.kc, uid, &params, r)
	if err != nil {
		return admdErrM("Cannot create creds", http.StatusInternalServerError)
	}

	err = xhttp.Respond(w, &params)
	if err != nil {
		return admdErrE(err, http.StatusInternalServerError)
	}

	return nil
}

func handleListCreds(ctx context.Context, w http.ResponseWriter, r *http.Request, uid string) *xrest.ReqErr {
	creds, err := ksListCreds(conf.kc, uid)
	if err != nil {
		return admdErrM("Cannot list creds", http.StatusInternalServerError)
	}

	err = xhttp.Respond(w, creds)
	if err != nil {
		return admdErrE(err, http.StatusInternalServerError)
	}

	return nil
}

func handleShowCred(ctx context.Context, w http.ResponseWriter, r *http.Request, uid,key string) *xrest.ReqErr {
	cred, err := ksGetCred(conf.kc, uid, key)
	if err != nil {
		return admdErrM("Cannot get cred", http.StatusInternalServerError)
	}

	err = xhttp.Respond(w, cred)
	if err != nil {
		return admdErrE(err, http.StatusInternalServerError)
	}

	return nil
}

func handleDeleteCred(ctx context.Context, w http.ResponseWriter, r *http.Request, uid,key string) *xrest.ReqErr {
	err, code := ksRemoveCred(conf.kc, uid, key)
	if err != nil {
		return admdErrM("Cannot remove cred", code)
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func setupLogger(conf *YAMLConf) {
	lvl := zap.DebugLevel

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
}

func isLite() bool { return Flavor == "lite" }

func main() {
	var config_path string
	var devel bool
	var err error

	flag.StringVar(&config_path,
			"conf",
				"/etc/swifty/conf/admd.yaml",
				"path to a config file")
	flag.BoolVar(&devel, "devel", false, "launch in development mode")
	flag.Parse()

	sysctl.AddRoSysctl("admd_mode", func() string {
		ret := "mode:"
		if devel {
			ret += "devel"
		} else {
			ret += "prod"
		}

		ret += ", flavor:" + Flavor

		return ret
	})

	if _, err := os.Stat(config_path); err == nil {
		xh.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	if conf.DefaultPlan == "" {
		log.Warnf("No default plan limit set!")
	}

	admdSecrets, err = xsecret.Init("admd")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	log.Debugf("config: %v", &conf)

	conf.kc = xh.ParseXCreds(conf.Keystone)

	err = ksInit(conf.kc)
	if err != nil {
		log.Errorf("Can't init ks: %s", err.Error())
		return
	}

	err = dbConnect(&conf)
	if err != nil {
		log.Fatalf("Can't setup mongo: %s", err.Error())
	}

	r := mux.NewRouter()
	r.HandleFunc("/v1/login", handleUserLogin).Methods("POST", "OPTIONS")
	r.Handle("/v1/users", genReqHandler(handleUsers)).Methods("POST", "GET", "OPTIONS")
	r.Handle("/v1/users/{uid}", genReqHandler(handleUser)).Methods("GET", "PUT", "DELETE", "OPTIONS")
	r.Handle("/v1/users/{uid}/pass", genReqHandler(handleSetPassword)).Methods("PUT", "OPTIONS")
	r.Handle("/v1/users/{uid}/limits", genReqHandler(handleUserLimits)).Methods("PUT", "GET", "OPTIONS")
	r.Handle("/v1/users/{uid}/creds", genReqHandler(handleUserCreds)).Methods("GET", "POST", "OPTIONS")
	r.Handle("/v1/users/{uid}/creds/{key}", genReqHandler(handleUserCred)).Methods("GET", "DELETE", "OPTIONS")
	r.Handle("/v1/plans", genReqHandler(handlePlans)).Methods("POST", "GET", "OPTIONS")
	r.Handle("/v1/plans/{pid}", genReqHandler(handlePlan)).Methods("GET", "DELETE", "PUT", "OPTIONS")
	r.Handle("/v1/sysctl", genReqHandler(handleSysctls)).Methods("GET", "OPTIONS")
	r.Handle("/v1/sysctl/{name}", genReqHandler(handleSysctl)).Methods("GET", "PUT", "OPTIONS")

	err = xhttp.ListenAndServe(
		&http.Server{
			Handler:      r,
			Addr:         conf.Daemon.Address,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
		}, conf.Daemon.HTTPS, devel || isLite(), func(s string) { log.Debugf(s) })
	if err != nil {
		log.Errorf("ListenAndServe: %s", err.Error())
	}
}
