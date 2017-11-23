package main

import (
	"go.uber.org/zap"

	"encoding/json"
	"net/http"
	"os/exec"
	"errors"
	"bytes"
	"syscall"
	"flag"
	"fmt"
	"time"
	"os"
	"io/ioutil"
	"sync/atomic"

	"../common"
	"../apis/apps"
)

type YAMLConfDaemon struct {
	Addr		string			`yaml:"addr"`
}

type YAMLConf struct {
	Daemon		YAMLConfDaemon		`yaml:"daemon"`
}

var function *swyapi.SwdFunctionDesc
var wdogStatsSyncPeriod = 5 * time.Second /* FIXME: should be configured */

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

func get_exit_code(err error) int {
	if exitError, ok := err.(*exec.ExitError); ok {
		ws := exitError.Sys().(syscall.WaitStatus)
		return ws.ExitStatus()
	} else {
		return -1 // XXX -- what else?
	}
}

type runReq struct {
	Params *swyapi.SwdFunctionRun
	Result chan *swyapi.SwdFunctionRunResult
}

var runQueue chan *runReq

func doRun() {
	for {
		req := <-runQueue

		stdout := new(bytes.Buffer)
		stderr := new(bytes.Buffer)

		run := req.Params.Args[0]
		args := req.Params.Args[1:]

		log.Debugf("Exec %s args %v", run, args)

		cmd := exec.Command(run, args...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err := cmd.Run()

		result := &swyapi.SwdFunctionRunResult{}
		if err != nil {
			result.Code = get_exit_code(err)
			log.Errorf("Run exited with %d (%s)", result.Code, err.Error())
		} else {
			result.Code = 0
			log.Errorf("OK");
		}

		result.Stdout = stdout.String()
		result.Stderr = stderr.String()

		req.Result <- result
	}
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req runReq
	var params swyapi.SwdFunctionRun
	var result *swyapi.SwdFunctionRunResult

	err := swy.HTTPReadAndUnmarshal(r, &params)
	if err != nil {
		goto out
	}

	if params.PodToken != function.PodToken {
		err = errors.New("Pod Token mismatch")
		goto out
	}

	req = runReq{Params: &params, Result: make(chan *swyapi.SwdFunctionRunResult)}
	runQueue <- &req
	result = <-req.Result

	err = swy.HTTPMarshalAndWrite(w, result)
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
	path := os.Getenv("PATH")

	err = os.Chdir(params.Dir)
	if err != nil {
		err = fmt.Errorf("Can't change dir to %s: %s",
				params.Dir, err.Error())
		goto out
	} else {
		log.Debugf("setupFunction: Chdir to %s", params.Dir)
	}

	err = os.Setenv("PATH", path + ":" + params.Dir)
	if err != nil {
		err = fmt.Errorf("can't set PATH: %s", err.Error())
		goto out
	}

	log.Debugf("PATH=%s", os.Getenv("PATH"))

	function = params
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

type wdogStatsOpaque struct {
}

var stats = swyapi.SwdStats{}

func wdogStatsStart() *wdogStatsOpaque {
	atomic.AddUint64(&stats.Called, 1)
	return &wdogStatsOpaque{}
}

func wdogStatsStop(st *wdogStatsOpaque) {
}

func wdogStatsRead() *swyapi.SwdStats {
	ret := &swyapi.SwdStats{}
	ret.Called = atomic.LoadUint64(&stats.Called)
	return ret
}

func wdogStatsMw(fn http.HandlerFunc) http.Handler {
	next := http.HandlerFunc(fn)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opaque := wdogStatsStart()
		next.ServeHTTP(w, r)
		wdogStatsStop(opaque)
	})
}

func wdogStatsSync(params *swyapi.SwdFunctionDesc) {
	statsFname := os.Getenv("SWD_POD_NAME")
	statsFile := params.Stats + "/" + statsFname
	statsTFile := params.Stats + "/." + statsFname + ".upd"
	var last uint64

	for {
		time.Sleep(wdogStatsSyncPeriod)
		st := wdogStatsRead()
		if st.Called == last {
			/* No need to update */
			continue
		}

		last = st.Called
		data, _ := json.Marshal(st)
		ioutil.WriteFile(statsTFile, data, 0644)
		os.Rename(statsTFile, statsFile)
	}
}

func main() {
	var params swyapi.SwdFunctionDesc
	var conf_path string
	var desc_raw string
	var err error
	var conf YAMLConf

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

	http.Handle("/v1/run", wdogStatsMw(handleRun))
	go wdogStatsSync(&params)
	runQueue = make(chan *runReq)
	go doRun()

	log.Fatal(http.ListenAndServe(conf.Daemon.Addr, nil))
}
