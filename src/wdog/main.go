package main

import (
	"go.uber.org/zap"

	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"errors"
	"bytes"
	"syscall"
	"flag"
	"fmt"
	"os"
	"io/ioutil"

	"../common"
	"../apis/apps"
)

type YAMLConfDaemon struct {
	Addr		string			`yaml:"addr"`
}

type YAMLConf struct {
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
}

var conf YAMLConf
var function swyapi.SwdFunctionDesc

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

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
}

func checkPodToken(t string) error {
	if t == "" {
		return errors.New("Empty pod token detected")
	} else if strings.Compare(t, function.PodToken) != 0 {
		return errors.New("Pod token mismatch")

	}

	return nil
}

func get_exit_code(err error) int {
	if exitError, ok := err.(*exec.ExitError); ok {
		ws := exitError.Sys().(syscall.WaitStatus)
		return ws.ExitStatus()
	} else {
		return -1 // XXX -- what else?
	}
}

func uploaded() bool {
	return len(function.Run) > 0
}

func handleDirectCall(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var cmd *exec.Cmd
	var args []string
	var run string
	var err error
	var body []byte

	if !uploaded() {
		err = errors.New("Function not ready")
		goto out
	}

	body, err = ioutil.ReadAll(r.Body)
	if err != nil {
		goto out
	}

	run = function.Run[0]
	args = append(function.Run[1:], string(body))

	cmd = exec.Command(run, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		log.Errorf("Run failed with %s", err.Error())
		goto out
	}

	/* XXX -- responce type should be configurable */
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	/* XXX -- what to do with Stderr? */
	w.Write(stdout.Bytes())

	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("%s", err.Error())
}

func handleGateCall(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var params swyapi.SwdFunctionRun
	var result swyapi.SwdFunctionRunResult

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var cmd *exec.Cmd
	var args []string
	var run string
	var err error

	if !uploaded() {
		err = errors.New("Upload function first")
		goto out
	}

	_, err = swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	err = checkPodToken(params.PodToken)
	if err != nil {
		goto out
	}

	/* FIXME -- need to check that the caller is gate? */

	run = function.Run[0]
	args = function.Run[1:]
	for _, v := range params.Args {
		args = append(args, v)
	}

	log.Debugf("Exec %s args %v", run, args)

	cmd = exec.Command(run, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		result.Code = get_exit_code(err)
		log.Errorf("Run exited with %d (%s)", result.Code, err.Error())
	} else {
		result.Code = 0
		log.Errorf("OK");
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	err = swy.HTTPMarshalAndWrite(w, &result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), http.StatusBadRequest)
	log.Errorf("%s", err.Error())
}

func setupFunction(params *swyapi.SwdFunctionDesc) error {
	var err error

	log.Debugf("setupFunction: %v", params)

	if len(params.Run) < 1 {
		err = errors.New("Empty run passed")
		goto out
	}

	if params.Dir != "" {
		err = os.Chdir(params.Dir)
		if err != nil {
			err = fmt.Errorf("Can't change dir to %s: %s",
					params.Dir, err.Error())
			goto out
		} else {
			log.Debugf("setupFunction: Chdir to %s", params.Dir)
		}

		path := os.Getenv("PATH")
		err = os.Setenv("PATH", path + ":" + params.Dir)
		if err != nil {
			err = fmt.Errorf("can't set PATH: %s", err.Error())
			goto out
		}
	}

	log.Debugf("PATH=%s", os.Getenv("PATH"))

	function = *params
	log.Debugf("setupFunction: OK")
	return nil

out:
	return fmt.Errorf("setupFunction: %s", err.Error())
}

func getSwdAddr(env_name string, defaul_value string) string {
	envVal := swy.SafeEnv("SWD_ADDR", "")
	if envVal == "" {
		//
		// Kubernetes should provide us IP used
		// for the port, and we ship port as well.
		podIP := swy.SafeEnv("SWD_POD_IP", "")
		if podIP == "" {
			return defaul_value
		}
		return podIP + ":" + swy.SafeEnv("SWD_PORT", "8687")
	}
	return envVal
}

func main() {
	var params swyapi.SwdFunctionDesc
	var conf_path string
	var desc_raw string
	var err error

	swy.InitLogger(log)

	flag.StringVar(&conf_path,
			"conf",
				"",
				"path to the configuration file")
	flag.StringVar(&conf.Daemon.Addr,
			"watchdog-addr",
				getSwdAddr("SWD_ADDR", "0.0.0.0:8687"),
				"address:port to listen requests on")
	flag.Parse()

	if conf_path != "" {
		swy.ReadYamlConfig(conf_path, &conf)
	}

	log.Debugf("config: %v", &conf)

	desc_raw = swy.SafeEnv("SWD_FUNCTION_DESC", "")
	if desc_raw == "" {
		log.Fatal("SWD_FUNCTION_DESC not set")
	}

	err = json.Unmarshal([]byte(desc_raw), &params)
	if err != nil {
		log.Fatal("SWD_FUNCTION_DESC unmarshal error: %s, abort", err.Error())
	}

	err = setupFunction(&params)
	if err != nil {
		log.Fatalf("Can't setup function, abort: %s", err.Error())
	}

	http.HandleFunc("/",				handleRoot)
	if params.URLCall {
		http.HandleFunc("/" + params.PodToken,	handleDirectCall)
	}
	http.HandleFunc("/v1/function/run",		handleGateCall)
	log.Fatal(http.ListenAndServe(conf.Daemon.Addr, nil))
}
