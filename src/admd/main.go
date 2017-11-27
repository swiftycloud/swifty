package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"net/http"
	"flag"
	"time"
	"fmt"
	"errors"

	"../apis/apps"
	"../common"
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

func handleAdminReq(r *http.Request, params interface{}) (*swy.KeystoneTokenData, int, error) {
	err := swy.HTTPReadAndUnmarshal(r, params)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return nil, http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	td, code := swy.KeystoneGetTokenData(conf.Keystone.Addr, token)
	if code != 0 {
		return nil, code, fmt.Errorf("Keystone auth error")
	}

	return td, 0, nil
}

func checkAdminOrOwner(user, target string, td *swy.KeystoneTokenData) (string, error) {
	if target == "" || target == user {
		if !swy.KeystoneRoleHas(td, swy.SwyUserRole) && !swy.KeystoneRoleHas(td, swy.SwyAdminRole) {
			return "", errors.New("Not logged in")
		}

		return user, nil
	} else {
		if !swy.KeystoneRoleHas(td, swy.SwyAdminRole) {
			return "", errors.New("Not an admin")
		}

		return target, nil
	}
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	var ui swyapi.UserInfo
	var kui *swy.KeystoneUser

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
	kui, err = ksGetUserInfo(&conf.Keystone, ui.Id)
	if err != nil {
		goto out
	}

	log.Debugf("USER: %s/%s/%s", kui.Id, kui.Name, kui.Description)
	err = swy.HTTPMarshalAndWrite(w, swyapi.UserInfo{
				Id: ui.Id,
				Name: kui.Description,
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

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* Listing users is only possible for admin */
	code = http.StatusForbidden
	if !swy.KeystoneRoleHas(td, swy.SwyAdminRole) {
		goto out
	}

	code = http.StatusBadRequest
	result, err = ksListUsers(&conf.Keystone)
	if err != nil {
		goto out
	}

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleDelUser(w http.ResponseWriter, r *http.Request) {
	var params swyapi.UserInfo
	var code = http.StatusBadRequest

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* User can be deleted by admin or self only. Admin
	 * cannot delete self */
	code = http.StatusForbidden
	if params.Id == "" || params.Id == td.Project.Name {
		if !swy.KeystoneRoleHas(td, swy.SwyUserRole) {
			err = errors.New("Not authorized")
			goto out
		}

		params.Id = td.Project.Name
	} else {
		if !swy.KeystoneRoleHas(td, swy.SwyAdminRole) {
			err = errors.New("Not an admin")
			goto out
		}
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

	td, code, err := handleAdminReq(r, &params)
	if err != nil {
		goto out
	}

	/* User can be added by admin or UI */
	code = http.StatusForbidden
	if !swy.KeystoneRoleHas(td, swy.SwyAdminRole) && !swy.KeystoneRoleHas(td, swy.SwyUserRole) {
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

	admdSecrets, err = swy.ReadSecrets("admd")
	if err != nil {
		log.Errorf("Can't read gate secrets")
		return
	}

	log.Debugf("config: %v", &conf)

	err = ksInit(&conf.Keystone)
	if err != nil {
		log.Errorf("Can't init ks: %s", err.Error())
		return
	}

	r := mux.NewRouter()
	r.HandleFunc("/v1/login", handleUserLogin)
	r.HandleFunc("/v1/users", handleListUsers)
	r.HandleFunc("/v1/userinfo", handleUserInfo)
	r.HandleFunc("/v1/adduser", handleAddUser)
	r.HandleFunc("/v1/deluser", handleDelUser)
	r.HandleFunc("/v1/setpass", handleSetPassword)

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
