package main

import (
	"go.uber.org/zap"

	"github.com/gorilla/mux"

	"net/http"
	"flag"
	"time"
	"fmt"

	"../apis/apps"
	"../common"
)

type YAMLConfDB struct {
	StateDB		string		`yaml:"state"`
	Addr		string		`yaml:"address"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"password"`
}

type YAMLConfKeystone struct {
	Addr		string			`yaml:"address"`
	Domain		string			`yaml:"domain"`
	Admin		string			`yaml:"admin"`
	Pass		string			`yaml:"pass"`
}

type YAMLConf struct {
	Listen		string			`yaml:"listen"`
	DB		YAMLConfDB		`yaml:"db"`
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

func handleAdminReq(r *http.Request, params interface{}, roles []string) (int, error) {
	err := swy.HTTPReadAndUnmarshal(r, params)
	if err != nil {
		return http.StatusBadRequest, err
	}
	token := r.Header.Get("X-Auth-Token")
	if token == "" {
		return http.StatusUnauthorized, fmt.Errorf("Auth token not provided")
	}

	prj, code := swy.KeystoneVerify(conf.Keystone.Addr, token, roles)
	if prj == "" {
		return code, fmt.Errorf("Keystone authentication error")
	}

	return http.StatusOK, nil
}

func handleListUsers(w http.ResponseWriter, r *http.Request) {
	var params swyapi.ListUsers
	var result []swyapi.UserInfo
	var code = http.StatusBadRequest
	var projects []string

	code, err := handleAdminReq(r, &params, []string{swy.SwyAdminRole})
	if code != http.StatusOK {
		goto out
	}

	code = http.StatusBadRequest
	projects, err = ksListProjects(&conf.Keystone)
	if err != nil {
		goto out
	}

	for _, prj := range projects {
		result = append(result, swyapi.UserInfo{Id: prj})
	}

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
}

func handleAddUser(w http.ResponseWriter, r *http.Request) {
	var params swyapi.AddUser
	var code = http.StatusBadRequest

	code, err := handleAdminReq(r, &params, []string{swy.SwyAdminRole, swy.SwyUIRole})
	if code != http.StatusOK {
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

	err := ksInit(&conf.Keystone)
	if err != nil {
		log.Errorf("Can't init ks: %s", err.Error())
		return
	}

	r := mux.NewRouter()
	r.HandleFunc("/v1/login", handleUserLogin)
	r.HandleFunc("/v1/users", handleListUsers)
	r.HandleFunc("/v1/adduser", handleAddUser)

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
