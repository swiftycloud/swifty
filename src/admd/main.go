package main

import (
	"go.uber.org/zap"

	"gopkg.in/mgo.v2/bson"
	"github.com/gorilla/mux"

	"strings"
	"net/http"
	"flag"
	"time"
	"os"
	"fmt"
	"errors"

	"../apis"
	"../common"
	"../common/http"
	"../common/keystone"
	"../common/secrets"
)

var admdSecrets map[string]string

type YAMLConfDaemon struct {
	Address		string			`yaml:"address"`
	HTTPS		*xhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConf struct {
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Gate		string			`yaml:"gate"`
	Keystone	string			`yaml:"keystone"`
	DB		string			`yaml:"db"`
	kc		*xh.XCreds
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

func handleUserLogin(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var token string
	var resp = http.StatusBadRequest
	var td swyapi.UserToken

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	log.Debugf("Try to login user %s", params.UserName)

	token, td.Expires, err = xkst.KeystoneAuthWithPass(conf.kc.Addr(), conf.kc.Domn, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = conf.Gate
	log.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

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

func handleUser(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdmdReq(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	uid := mux.Vars(r)["uid"]
	if uid == "me" {
		uid = td.User.Id
	}

	switch r.Method {
	case "GET":
		handleUserInfo(w, r, uid, td)
	case "PUT":
		handleUserUpdate(w, r, uid, td)
	case "DELETE":
		handleDelUser(w, r, uid, td)
	}
}

func handlePlan(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdmdReq(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	pid := mux.Vars(r)["pid"]
	if !bson.IsObjectIdHex(pid) {
		http.Error(w, "Bad plan ID value", http.StatusBadRequest)
		return
	}

	p_id := bson.ObjectIdHex(pid)
	switch r.Method {
	case "GET":
		handlePlanInfo(w, r, p_id, td)
	case "DELETE":
		handleDelPlan(w, r, p_id, td)
	}
}

func handleUserUpdate(w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) {
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
	err = xhttp.ReadAndUnmarshalReq(r, &params)
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handlePlanInfo(w http.ResponseWriter, r *http.Request, pid bson.ObjectId, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdmdReq(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	switch r.Method {
	case "GET":
		handleListUsers(w, r, td)
	case "POST":
		handleAddUser(w, r, td)
	}
}

func handlePlans(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdmdReq(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	switch r.Method {
	case "GET":
		handleListPlans(w, r, td)
	case "POST":
		handleAddPlan(w, r, td)
	}
}

func handleListUsers(w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func (pl *PlanLimits)toInfo() *swyapi.PlanLimits {
	return&swyapi.PlanLimits{
		Id:	pl.ObjID.Hex(),
		Name:	pl.Name,
		Fn:	pl.Fn,
	}
}

func handleListPlans(w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
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
		return fmt.Errorf("Bad responce from server: %s", string(resp.Status))
	}

	if out != nil {
		err = xhttp.ReadAndUnmarshalResp(resp, out)
		if err != nil {
			return fmt.Errorf("Bad responce body: %s", err.Error())
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

func handleDelUser(w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleDelPlan(w http.ResponseWriter, r *http.Request, pid bson.ObjectId, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleAddUser(w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) {
	var params swyapi.AddUser
	var kid string
	var err error

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusBadRequest
	err = xhttp.ReadAndUnmarshalReq(r, &params)
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

	if params.PlanId != "" {
		if !bson.IsObjectIdHex(params.PlanId) {
			err = errors.New("Bad plan ID value")
			goto out
		}

		var plim *PlanLimits
		plim, err = dbGetPlanLimits(ses, bson.ObjectIdHex(params.PlanId))
		if err != nil {
			goto out
		}

		err = dbSetUserLimits(ses, &conf, &swyapi.UserLimits {
			UId:	params.UId,
			PlanId:	params.PlanId,
			Fn:	plim.Fn,
		})
		if err != nil {
			goto out
		}
	}

	kid, err = ksAddUserAndProject(conf.kc, &params)
	if err != nil {
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

	return

out:
	http.Error(w, err.Error(), code)
}

type PlanLimits struct {
	ObjID	bson.ObjectId		`bson:"_id,omitempty"`
	Name	string			`bson:"name"`
	Fn	*swyapi.FunctionLimits	`bson:"function,omitempty"`
}

func handleAddPlan(w http.ResponseWriter, r *http.Request, td *xkst.KeystoneTokenData) {
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
	err = xhttp.ReadAndUnmarshalReq(r, &params)
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
		Fn:	params.Fn,
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleSetLimits(w http.ResponseWriter, r *http.Request, uid string, td *xkst.KeystoneTokenData) {
	var params swyapi.UserLimits
	var rui *swyapi.UserInfo
	var err error

	ses := session.Copy()
	defer ses.Close()

	code := http.StatusForbidden
	if !xkst.HasRole(td, swyapi.AdminRole) {
		err = errors.New("Only admin may change user limits")
		goto out
	}

	code = http.StatusBadRequest
	err = xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if params.PlanId != "" {
		if !bson.IsObjectIdHex(params.PlanId) {
			err = errors.New("Bad plan ID value")
			goto out
		}

		var plim *PlanLimits
		plim, err = dbGetPlanLimits(ses, bson.ObjectIdHex(params.PlanId))
		if err != nil {
			goto out
		}

		/* Set nil params' limits to plans' ones */
		if plim.Fn != nil {
			if params.Fn == nil {
				params.Fn = plim.Fn
			} else {
				if params.Fn.Rate == 0 {
					params.Fn.Rate = plim.Fn.Rate
					params.Fn.Burst = plim.Fn.Burst
				}

				if params.Fn.MaxInProj == 0 {
					params.Fn.MaxInProj = plim.Fn.MaxInProj
				}

				if params.Fn.GBS == 0 {
					params.Fn.GBS = plim.Fn.GBS
				}

				if params.Fn.BytesOut == 0 {
					params.Fn.BytesOut = plim.Fn.BytesOut
				}
			}
		}
	}

	code = http.StatusInternalServerError
	rui, err = getUserInfo(conf.kc, uid, false)
	if err != nil {
		goto out
	}

	params.UId = rui.UId
	err = dbSetUserLimits(ses, &conf, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
}

func handleGetLimits(w http.ResponseWriter, uid string, td *xkst.KeystoneTokenData) {
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var params swyapi.ChangePass
	var code = http.StatusBadRequest

	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	uid := mux.Vars(r)["uid"]

	td, code, err := handleAdmdReq(r)
	if err != nil {
		goto out
	}

	if uid == "me" {
		uid = td.User.Id
	}

	code = http.StatusBadRequest
	err = xhttp.ReadAndUnmarshalReq(r, &params)
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

	return

out:
	http.Error(w, err.Error(), code)
}

func handleUserLimits(w http.ResponseWriter, r *http.Request) {
	if xhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	uid := mux.Vars(r)["uid"]

	td, code, err := handleAdmdReq(r)
	if err != nil {
		goto out
	}

	if uid == "me" {
		uid = td.User.Id
	}

	code = http.StatusForbidden
	if r.Method == "PUT" {
		handleSetLimits(w, r, uid, td)
	} else {
		handleGetLimits(w, uid, td)
	}

	return

out:
	http.Error(w, err.Error(), code)
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

	if _, err := os.Stat(config_path); err == nil {
		xh.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	admdSecrets, err = xsecret.ReadSecrets("admd")
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
	r.HandleFunc("/v1/users", handleUsers).Methods("POST", "GET", "OPTIONS")
	r.HandleFunc("/v1/users/{uid}", handleUser).Methods("GET", "PUT", "DELETE", "OPTIONS")
	r.HandleFunc("/v1/users/{uid}/pass", handleSetPassword).Methods("PUT", "OPTIONS")
	r.HandleFunc("/v1/users/{uid}/limits", handleUserLimits).Methods("PUT", "GET", "OPTIONS")
	r.HandleFunc("/v1/plans", handlePlans).Methods("POST", "GET", "OPTIONS")
	r.HandleFunc("/v1/plans/{pid}", handlePlan).Methods("GET", "DELETE", "OPTIONS")

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
