package main

import (
	"go.uber.org/zap"

	"net/http"
	"os/exec"
	"strconv"
	"bytes"
	"time"
	"sync"
	"syscall"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"

	"../common"
	"../common/http"
	"../common/xqueue"
	"../apis/apps"
)

var fnTmo int
var lang string

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
	syscall.CloseOnExec(p[0])
	runner.fin = os.NewFile(uintptr(p[0]), "runner.stdout")

	err = syscall.Pipe(p)
	if err != nil {
		return fmt.Errorf("Can't make err pipe: %s", err.Error())
	}

	runner.ferr = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	syscall.CloseOnExec(p[0])
	runner.fine = os.NewFile(uintptr(p[0]), "runner.stderr")

	return startQnR()
}

var runners = map[string]string {
	"golang": "/go/src/swycode/function",
	"python": "/usr/bin/swy-runner.py",
	"swift": "/swift/swycode/debug/function",
}

func startQnR() error {
	var err error

	runner.q, err = xqueue.MakeQueue()
	if err != nil {
		return fmt.Errorf("Can't make queue: %s", err.Error())
	}

	runner.cmd = exec.Command(runners[lang], runner.q.GetId(), runner.fout, runner.ferr)
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

func doRun(body []byte) (*swyapi.SwdFunctionRunResult, error) {
	var err error
	timeout := false

	runlock.Lock()
	defer runlock.Unlock()

	start := time.Now()
	err = runner.q.SendBytes(body)
	if err != nil {
		return nil, fmt.Errorf("Can't send args: %s", err.Error())
	}

	done := make(chan bool)
	go func() {
		select {
		case <-done:
			return
		case <-time.After(time.Duration(fnTmo) * time.Millisecond):
			break
		}

		timeout = true
		xerr := runner.cmd.Process.Kill()
		if xerr != nil {
			log.Errorf("Can't kill runner: %s", xerr.Error())
		}
		<-done
	}()

	var res string
	res, err = runner.q.RecvStr()
	rt := time.Since(start)
	done <-true

	var code int
	if res[0] == '0' {
		code = 0
	} else {
		code = http.StatusInternalServerError
	}

	ret := &swyapi.SwdFunctionRunResult{
		Code: code,
		Return: res[2:],
		Stdout: readLines(runner.fin),
		Stderr: readLines(runner.fine),
		Time: uint(rt / time.Microsecond),
	}

	if err != nil {
		restartRunner()

		switch {
		case timeout:
			ret.Code = swyhttp.StatusTimeoutOccurred
			ret.Return = "timeout"

		case err == io.EOF:
			ret.Code = http.StatusInternalServerError
			ret.Return = "exited"

		default:
			return nil, fmt.Errorf("Can't get back the result: %s", err.Error())
		}
	}

	return ret, nil
}

var builders = map[string]func(*swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	"golang": doBuildGo,
	"swift": doBuildSwift,
}

func doBuild(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	runlock.Lock()
	defer runlock.Unlock()

	fn, ok := builders[lang]
	if !ok {
		return nil, fmt.Errorf("No builder for %s", lang)
	}

	return fn(params)
}

/*
 * All functions sit at /go/src/swycode/
 * Runner sits at /go/src/swyrunner/
 */
func doBuildGo(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove("/go/src/swyrunner/script.go")
	srcdir := params.Sources
	err := os.Symlink("/go/src/swycode/" + srcdir + "/script.go", "/go/src/swyrunner/script.go")
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	err = os.Chdir("/go/src/swyrunner")
	if err != nil {
		return nil, fmt.Errorf("Can't chdir to swywdog: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run go build on %s", srcdir)
	cmd := exec.Command("go", "build", "-o", "../swycode/" + srcdir + "/function")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	os.Remove("/go/src/swyrunner/script.go") /* Just an attempt */

	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.SwdFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.SwdFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

/*
 * All functions sit at /swift/swycode/
 * Runner sits at /swift/runner/
 */
func doBuildSwift(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove("/swift/runner/Sources/script.swift")
	srcdir := params.Sources
	err := os.Symlink("/swift/swycode/" + srcdir + "/script.swift", "/swift/runner/Sources/script.swift")
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	err = os.Chdir("/swift/runner")
	if err != nil {
		return nil, fmt.Errorf("Can't chdir to runner dir: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run swift build on %s", srcdir)
	cmd := exec.Command("swift", "build", "--build-path", "../swycode/" + srcdir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	os.Remove("/swift/runner/Sources/script.swift")

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
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	result, err = doRun(body)
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

func handleBuild(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var params swyapi.SwdFunctionBuild
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest
	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	result, err = doBuild(&params)
	if err != nil {
		log.Errorf("Error building FN: %s", err.Error())
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

func startCResponder(podip string) error {
	addr, err := net.ResolveUnixAddr("unixpacket", "/var/run/swifty/" + podip)
	if err != nil {
		return err
	}

	sk, err := net.ListenUnix("unixpacket", addr)
	if err != nil {
		return err
	}

	go func() {
		for {
			cln, err := sk.AcceptUnix()
			if err != nil {
				log.Errorf("Can't accept cresponder connection: %s", err.Error())
				break
			}

			log.Debugf("CResponder accepted conn")
			cln.Close()
		}
	}()

	return nil
}

func main() {
	var err error

	swy.InitLogger(log)

	podIP := swy.SafeEnv("SWD_POD_IP", "")
	if podIP == "" {
		log.Fatal("NO POD_IP")
	}

	podPort := swy.SafeEnv("SWD_PORT", "")
	if podPort == "" {
		log.Fatal("NO PORT")
	}

	lang = swy.SafeEnv("SWD_LANG", "")
	if lang == "" {
		log.Fatal("SWD_LANG not set")
	}

	inst := swy.SafeEnv("SWD_INSTANCE", "")
	if inst == "build" {
		http.HandleFunc("/v1/run", handleBuild)
	} else {
		err = startRunner()
		if err != nil {
			log.Fatal("Can't start runner")
		}

		err = startCResponder(podIP)
		if err != nil {
			log.Fatal("Can't start cresponder: %s", err.Error())
		}

		tmos := swy.SafeEnv("SWD_FN_TMO", "")
		if tmos == "" {
			log.Fatal("SWD_FN_TMO not set")
		}

		fnTmo, err = strconv.Atoi(tmos)
		if err != nil {
			log.Fatal("Bad timeout value")
		}

		podToken := swy.SafeEnv("SWD_POD_TOKEN", "")
		if podToken == "" {
			log.Fatal("SWD_POD_TOKEN not set")
		}

		http.HandleFunc("/v1/run/" + podToken, handleRun)
	}

	log.Fatal(http.ListenAndServe(podIP + ":" + podPort, nil))
}
