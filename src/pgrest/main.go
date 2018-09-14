package main

import (
	"go.uber.org/zap"
	"net/http"
	"errors"
	"strings"
	"bytes"
	"os/exec"
	"flag"
	"syscall"
	"os"
	"../apis"
	"../common"
	"../common/http"
	"../common/secrets"
)

var pgrTokens []string

type YAMLConf struct {
	Addr	string		`yaml:"address"`
	Token	string		`yaml:"token"`
	Uid	uint32		`yaml:"user"`
	Gid	uint32		`yaml:"group"`
}

var zcfg zap.Config = zap.Config {
	Level:            zap.NewAtomicLevelAt(zap.DebugLevel),
	Development:      true,
	DisableStacktrace:true,
	Encoding:         "console",
	EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
	OutputPaths:      []string{"stderr"},
	ErrorOutputPaths: []string{"stderr"},
}
var logger, _ = zcfg.Build()
var log = logger.Sugar()

func pgCheckString(str string) bool {
	return xh.NameSymsAllowed(str)
}

func pgRun(cmd *exec.Cmd) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var err error

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: conf.Uid, Gid: conf.Gid}
	err = cmd.Run()
	if err != nil {
		log.Errorf("Error running cmd: %s/%s", stdout.String(), stderr.String())
	}
	return err
}

func pgCreate(inf *swyapi.PgRequest) error {
	var err error

	if !pgCheckString(inf.User) ||
			! pgCheckString(inf.DbName) ||
			! pgCheckString(inf.Pass) {
		return errors.New("Bad string value")
	}

	log.Debugf("Add u: %s, db: %s", inf.User, inf.DbName)

	err = pgRun(exec.Command("psql", "-c", "CREATE USER " + inf.User + " WITH PASSWORD '" + inf.Pass + "';"))
	if err != nil {
		goto out
	}

	err = pgRun(exec.Command("psql", "-c", "CREATE DATABASE " + inf.DbName + ";"))
	if err != nil {
		goto out
	}

	err = pgRun(exec.Command("psql", "-c", "GRANT ALL PRIVILEGES ON DATABASE \"" + inf.DbName + "\" to " + inf.User + ";"))
	if err != nil {
		goto out
	}

	log.Debugf("`- added OK")
	return nil

out:
	pgDrop(inf)
	return err
}

func pgDrop(inf *swyapi.PgRequest) error {
	var err error

	if !pgCheckString(inf.User) ||
			! pgCheckString(inf.DbName) {
		return errors.New("Bad string value")
	}

	if inf.User == "postgres" || inf.DbName == "postgres" {
		return errors.New("System drop impossible")
	}

	log.Debugf("Drop u: %s, db: %s", inf.User, inf.DbName)

	err = pgRun(exec.Command("psql", "-c", "DROP DATABASE " + inf.DbName + ";"))
	if err != nil {
		log.Errorf("Cannot drop database %s: %s", inf.DbName, err.Error())
	}

	erru := pgRun(exec.Command("psql", "-c", "DROP USER " + inf.User + ";"))
	if erru != nil {
		log.Errorf("Cannot drop user %s: %s", inf.User, erru.Error())
		if err == nil {
			err = erru
		}
	}

	if err == nil {
		log.Debugf("`- dropped OK")
	}
	return err
}

func checkToken(token string) bool {
	for _, vt := range pgrTokens {
		if token == vt {
			return true
		}
	}
	return false
}

func handleRequest(w http.ResponseWriter, r *http.Request,
		handler func (*swyapi.PgRequest) error) {
	defer r.Body.Close()

	var code int
	var params swyapi.PgRequest

	code = http.StatusBadRequest
	err := xhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusUnauthorized
	if !checkToken(params.Token) {
		err = errors.New("Not authorized")
		goto out
	}

	code = http.StatusInternalServerError
	err = handler(&params)
	if err != nil {
		goto out
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	return

out:
	http.Error(w, err.Error(), code)
}

func handleCreate(w http.ResponseWriter, r *http.Request) { handleRequest(w, r, pgCreate) }
func handleDrop(w http.ResponseWriter, r *http.Request) { handleRequest(w, r, pgDrop) }

var conf YAMLConf

func main() {
	var conf_path string
	var err error
	var pgrSecrets map[string]string

	flag.StringVar(&conf_path,
			"conf",
				"/etc/swifty/conf/pgrest.yaml",
				"path to the configuration file")
	flag.Parse()
	if _, err := os.Stat(conf_path); err == nil {
		xh.ReadYamlConfig(conf_path, &conf)
	}

	pgrSecrets, err = xsecret.ReadSecrets("pgrest")
	if err != nil {
		log.Errorf("Can't read gate secrets: %s", err.Error())
		return
	}

	pgrTokens = strings.Split(pgrSecrets[conf.Token], ":")

	http.HandleFunc("/create", handleCreate)
	http.HandleFunc("/drop", handleDrop)
	log.Fatal(http.ListenAndServe(conf.Addr, nil))
}

