package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"strings"
	"net/http"
	"flag"
	"time"
	"os"
	"fmt"
	"errors"

	"../apis/apps"
	"../common"
	"../common/http"
	"../common/keystone"
	"../common/secrets"
)

var admdSecrets map[string]string

type YAMLConfDaemon struct {
	Address		string			`yaml:"address"`
	HTTPS		*swyhttp.YAMLConfHTTPS	`yaml:"https,omitempty"`
}

type YAMLConf struct {
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
	Gate		string			`yaml:"gate"`
	Keystone	string			`yaml:"keystone"`
	DB		string			`yaml:"db"`
	kc		*swy.XCreds
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

	log.Debugf("Try to login user %s", params.UserName)

	token, td.Expires, err = swyks.KeystoneAuthWithPass(conf.kc.Addr(), conf.kc.Domn, &params)
	if err != nil {
		resp = http.StatusUnauthorized
		goto out
	}

	td.Endpoint = conf.Gate
	log.Debugf("Login passed, token %s (exp %s)", token[:16], td.Expires)

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

func handleAdminReq(r *http.Request, params interface{}) (*swyks.KeystoneTokenData, int, error) {
	err := swyhttp.ReadAndUnmarshalReq(r, params)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return nil, http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	td, code := swyks.KeystoneGetTokenData(conf.kc.Addr(), token)
	if code != 0 {
		return nil, code, fmt.Errorf("Keystone auth error")
	}

	return td, 0, nil
}

func checkAdminOrOwner(user, target string, td *swyks.KeystoneTokenData) (string, error) {
	if target == "" || target == user {
		if !swyks.KeystoneRoleHas(td, swyks.SwyUserRole) && !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
			return "", errors.New("Not logged in")
		}

		return user, nil
	} else {
		if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
			return "", errors.New("Not an admin")
		}

		return target, nil
	}
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	var ui swyapi.UserInfo
	var rui *swyapi.UserInfo

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &ui)
	if err != nil {
		goto out
	}

	code = http.StatusForbidden
	ui.UId, err = checkAdminOrOwner(td.Project.Name, ui.UId, td)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	rui, err = getUserInfo(conf.kc, ui.UId)
	if err != nil {
		log.Errorf("GetUserDesc: %s", err.Error())
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, rui)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	var params swyapi.ListUsers
	var result []*swyapi.UserInfo
	var code = http.StatusBadRequest

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	if swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
		result, err = listUsers(conf.kc)
		if err != nil {
			goto out
		}
	} else if swyks.KeystoneRoleHas(td, swyks.SwyUserRole) {
		var ui *swyapi.UserInfo
		ui, err = getUserInfo(conf.kc, td.Project.Name)
		if err != nil {
			goto out
		}
		result = append(result, ui)
	} else {
		code = http.StatusForbidden
		err = errors.New("Not swifty role")
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func makeGateReq(gate, tennant, addr string, in interface{}, out interface{}, authToken string) error {
	resp, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
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
		err = swyhttp.ReadAndUnmarshalResp(resp, out)
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

func handleDelUser(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserInfo
	var code = http.StatusBadRequest

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* User can be deleted by admin or self only. Admin
	 * cannot delete self */
	code = http.StatusForbidden
	if params.UId == "" || params.UId == td.Project.Name {
		if !swyks.KeystoneRoleHas(td, swyks.SwyUserRole) {
			err = errors.New("Not authorized")
			goto out
		}

		params.UId = td.Project.Name
	} else {
		if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
			err = errors.New("Not an admin")
			goto out
		}
	}

	code = http.StatusServiceUnavailable
	err = tryRemoveAllProjects(params.UId, r.Header.Get("X-Auth-Token"))
	if err != nil {
		goto out
	}

	log.Debugf("Del user %v", params)
	code = http.StatusBadRequest
	err = ksDelUserAndProject(conf.kc, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusNoContent)

	return

out:
	http.Error(w, err.Error(), code)
}

func handleAddUser(w http.ResponseWriter, r *http.Request) {
	var params swyapi.AddUser
	var code = http.StatusBadRequest

	ses := session.Copy()
	defer ses.Close()

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* User can be added by admin or UI */
	code = http.StatusForbidden
	if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) && !swyks.KeystoneRoleHas(td, swyks.SwyUIRole) {
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
		var plim *swyapi.UserLimits

		plim, err = dbGetPlanLimits(ses, &conf, params.PlanId)
		if err != nil {
			goto out
		}

		plim.Id = params.UId
		err = dbSetUserLimits(ses, &conf, plim)
		if err != nil {
			goto out
		}
	}

	err = ksAddUserAndProject(conf.kc, &params)
	if err != nil {
		dbDelUserLimits(ses, &conf, params.UId)
		goto out
	}

	w.WriteHeader(http.StatusCreated)

	return

out:
	http.Error(w, err.Error(), code)
}

func handleSetLimits(w http.ResponseWriter, r *http.Request) {
	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ses := session.Copy()
	defer ses.Close()

	var params swyapi.UserLimits

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusForbidden
	if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
		err = errors.New("Only admin may change user limits")
		goto out
	}

	if params.PlanId != "" {
		var plim *swyapi.UserLimits

		plim, err = dbGetPlanLimits(ses, &conf, params.PlanId)
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

	err = dbSetUserLimits(ses, &conf, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), code)
}

func handleGetLimits(w http.ResponseWriter, r *http.Request) {
	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	ses := session.Copy()
	defer ses.Close()

	var params swyapi.UserInfo
	var ulim *swyapi.UserLimits

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusForbidden
	params.UId, err = checkAdminOrOwner(td.Project.Name, params.UId, td)
	if err != nil {
		goto out
	}

	ulim, err = dbGetUserLimits(ses, &conf, params.UId)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, ulim)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserLogin
	var code = http.StatusBadRequest

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	if params.Password == "" {
		err = fmt.Errorf("Empty password")
		goto out
	}

	code = http.StatusForbidden
	params.UserName, err = checkAdminOrOwner(td.Project.Name, params.UserName, td)
	if err != nil {
		goto out
	}

	log.Debugf("Change pass to %s", params.UserName)
	err = ksChangeUserPass(conf.kc, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusCreated)

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
		swy.ReadYamlConfig(config_path, &conf)
		setupLogger(&conf)
	} else {
		setupLogger(nil)
		log.Errorf("Provide config path")
		return
	}

	admdSecrets, err = swysec.ReadSecrets("admd")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	log.Debugf("config: %v", &conf)

	conf.kc = swy.ParseXCreds(conf.Keystone)

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
	r.HandleFunc("/v1/users", handleListUsers).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/userinfo", handleUserInfo).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/adduser", handleAddUser).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/deluser", handleDelUser).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/setpass", handleSetPassword).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/limits/set", handleSetLimits).Methods("POST", "OPTIONS")
	r.HandleFunc("/v1/limits/get", handleGetLimits).Methods("POST", "OPTIONS")

	err = swyhttp.ListenAndServe(
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
