package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

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

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
	Admin		string			`yaml:"admin"`
	Pass		string			`yaml:"pass"`
}

type YAMLConf struct {
	Listen		string			`yaml:"listen"`
	Gate		string			`yaml:"gate"`
	Keystone	YAMLConfKeystone	`yaml:"keystone"`
}

var conf YAMLConf
var gatesrv *http.Server
var log *zap.SugaredLogger

var CORS_Headers = []string {
	"Content-Type",
	"Content-Length",
	"X-Subject-Token",
	"X-Auth-Token",
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

	token, td.Expires, err = swyks.KeystoneAuthWithPass(conf.Keystone.Addr, conf.Keystone.Domain, &params)
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

	td, code := swyks.KeystoneGetTokenData(conf.Keystone.Addr, token)
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
	var kud *ksUserDesc

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &ui)
	if err != nil {
		goto out
	}

	code = http.StatusForbidden
	ui.Id, err = checkAdminOrOwner(td.Project.Name, ui.Id, td)
	if err != nil {
		goto out
	}

	code = http.StatusBadRequest
	kud, err = ksGetUserDesc(&conf.Keystone, ui.Id)
	if err != nil {
		log.Errorf("GetUserDesc: %s", err.Error())
		goto out
	}

	log.Debugf("USER: %s/%s/%s", ui.Id, kud.Name, kud.Email)
	err = swyhttp.MarshalAndWrite(w, swyapi.UserInfo{
				Id: ui.Id,
				Name: kud.Name,
			})
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	var params swyapi.ListUsers
	var result *[]swyapi.UserInfo
	var code = http.StatusBadRequest

	if swyhttp.HandleCORS(w, r, CORS_Methods, CORS_Headers) { return }

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* Listing users is only possible for admin */
	code = http.StatusForbidden
	if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
		err = errors.New("Not admin cannot list users")
		goto out
	}

	code = http.StatusBadRequest
	result, err = ksListUsers(&conf.Keystone)
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, &result)
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
	if params.Id == "" || params.Id == td.Project.Name {
		if !swyks.KeystoneRoleHas(td, swyks.SwyUserRole) {
			err = errors.New("Not authorized")
			goto out
		}

		params.Id = td.Project.Name
	} else {
		if !swyks.KeystoneRoleHas(td, swyks.SwyAdminRole) {
			err = errors.New("Not an admin")
			goto out
		}
	}

	code = http.StatusServiceUnavailable
	err = tryRemoveAllProjects(params.Id, r.Header.Get("X-Auth-Token"))
	if err != nil {
		goto out
	}

	log.Debugf("Del user %v", params)
	code = http.StatusBadRequest
	err = ksDelUserAndProject(&conf.Keystone, &params)
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

	log.Debugf("Add user %v", params)
	code = http.StatusBadRequest
	err = ksAddUserAndProject(&conf.Keystone, &params)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusCreated)

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
	err = ksChangeUserPass(&conf.Keystone, &params)
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

	swy.InitLogger(log)
}

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

	err = ksInit(&conf.Keystone)
	if err != nil {
		log.Errorf("Can't init ks: %s", err.Error())
		return
	}

	r := mux.NewRouter()
	r.HandleFunc("/v1/login", handleUserLogin).Methods("POST")
	r.HandleFunc("/v1/users", handleListUsers).Methods("POST")
	r.HandleFunc("/v1/userinfo", handleUserInfo).Methods("POST")
	r.HandleFunc("/v1/adduser", handleAddUser).Methods("POST")
	r.HandleFunc("/v1/deluser", handleDelUser).Methods("POST")
	r.HandleFunc("/v1/setpass", handleSetPassword).Methods("POST")

	gatesrv = &http.Server{
			Handler:      r,
			Addr:         conf.Listen,
			WriteTimeout: 60 * time.Second,
			ReadTimeout:  60 * time.Second,
	}

	err = gatesrv.ListenAndServe()
	if err != nil {
		log.Errorf("ListenAndServe: %s", err.Error())
	}
}
