package main

import (
	"go.uber.org/zap"

	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"errors"
	"bytes"
	"time"
	"sync"
	"syscall"
	"fmt"
	"os"

	"../common"
	"../common/http"
	"../common/xqueue"
	"../apis/apps"
)

var function *swyapi.SwdFunctionDesc

type Runner struct {
	cmd	*exec.Cmd
	q	*xqueue.Queue
	fout	string
	ferr	string
	fin	*os.File
	fine	*os.File
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

func get_exit_code(err error) (bool, int) {
	if exitError, ok := err.(*exec.ExitError); ok {
		ws := exitError.Sys().(syscall.WaitStatus)
		return true, ws.ExitStatus()
	} else {
		return false, -1 // XXX -- what else?
	}
}

var runner *Runner

func restartRunner() {
	runner.cmd.Wait()
	runner.q.Close()
	startQnR()
}

func startRunner() error {
	var err error
	p := make([]int, 2)

	runner = &Runner {}

	err = syscall.Pipe(p)
	if err != nil {
		return fmt.Errorf("Can't make out pipe: %s", err.Error())
	}

	runner.fout = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	runner.fin = os.NewFile(uintptr(p[0]), "runner.stdout")

	err = syscall.Pipe(p)
	if err != nil {
		return fmt.Errorf("Can't make err pipe: %s", err.Error())
	}

	runner.ferr = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	runner.fine = os.NewFile(uintptr(p[0]), "runner.stderr")

	return startQnR()
}

func startQnR() error {
	var err error

	runner.q, err = xqueue.MakeQueue()
	if err != nil {
		return fmt.Errorf("Can't make queue: %s", err.Error())
	}

	runner.cmd = exec.Command("/go/src/swycode/function", runner.q.GetId(), runner.fout, runner.ferr)
	err = runner.cmd.Start()
	if err != nil {
		return fmt.Errorf("Can't start runner: %s", err.Error())
	}

	log.Debugf("Started runner (queue %s)", runner.q.FDS())
	runner.q.Started()
	return nil
}

func readLines(f *os.File) string {
	var ret string

	buf := make([]byte, 512, 512)
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			return ret
		}
		ret += string(buf[:n])
	}
}

var runlock sync.Mutex

func doRun(params *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, int, error) {
	var err error

	timeout := false
	code := http.StatusInternalServerError

	runlock.Lock()
	defer runlock.Unlock()

	log.Debugf("Running FN (%v)", params.Args)
	err = runner.q.Send(params.Args)
	if err != nil {
		return nil, code, fmt.Errorf("Can't send args: %s", err.Error())
	}

	done := make(chan bool)
	go func() {
		select {
		case <-done:
			return
		case <-time.After(time.Duration(function.Timeout) * time.Millisecond):
			break
		}

		log.Debugf("Timeout!")

		timeout = true
		xerr := runner.cmd.Process.Kill()
		if xerr != nil {
			log.Errorf("Can't kill runner: %s", xerr.Error())
		}
	}()

	var res string
	res, err = runner.q.RecvStr()
	if err != nil {
		if !timeout {
			err = fmt.Errorf("Can't get back the result: %s", err.Error())
			return nil, code, err
		}

		restartRunner()
		return &swyapi.SwdFunctionRunResult{
			Return: "timeout",
			Code: 524, /* A Timeout Occurred */
		}, 0, nil
	}

	done <-true

	rout := readLines(runner.fin)
	rerr := readLines(runner.fine)

	return &swyapi.SwdFunctionRunResult{
		Return: res,
		Code: 0,
		Stdout: rout,
		Stderr: rerr,
		/* FIXME -- calc Time and CTime */
	}, 0, nil
}

func doBuild() (*swyapi.SwdFunctionRunResult, error) {
	err := os.Chdir("/go/src/swyrunner")
	if err != nil {
		return nil, fmt.Errorf("Can't chdir to swywdog: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run go build in /go/src/swyrunner")
	cmd := exec.Command("go", "build", "-o", "../swycode/function")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.SwdFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.SwdFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var params swyapi.SwdFunctionRun
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if params.PodToken != function.PodToken {
		err = errors.New("Pod Token mismatch")
		goto out
	}

	if !function.Build {
		result, code, err = doRun(&params)
	} else {
		result, err = doBuild()
	}
	if err != nil {
		goto out
	}

	err = swyhttp.MarshalAndWrite(w, result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("%s", err.Error())
}

func getSwdAddr() string {
	podIP := swy.SafeEnv("SWD_POD_IP", "")
	if podIP == "" {
		log.Debugf("NO POD_IP")
		return ""
	}

	podPort := swy.SafeEnv("SWD_PORT", "")
	if podPort == "" {
		log.Debugf("NO PORT")
		return ""
	}

	return podIP + ":" + podPort
}

func main() {
	var params swyapi.SwdFunctionDesc
	var desc_raw string
	var err error

	swy.InitLogger(log)

	addr := getSwdAddr()
	if addr == "" {
		log.Fatal("No address specified")
	}

	desc_raw = swy.SafeEnv("SWD_FUNCTION_DESC", "")
	if desc_raw == "" {
		log.Fatal("SWD_FUNCTION_DESC not set")
	}

	err = json.Unmarshal([]byte(desc_raw), &params)
	if err != nil {
		log.Fatal("SWD_FUNCTION_DESC unmarshal error: %s, abort", err.Error())
	}

	function = &params

	if !function.Build {
		err = startRunner()
		if err != nil {
			log.Fatal("Can't start runner")
		}
	}

	http.HandleFunc("/v1/run", handleRun)
	log.Fatal(http.ListenAndServe(addr, nil))
}
