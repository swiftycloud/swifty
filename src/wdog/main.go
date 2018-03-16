package main

import (
	"go.uber.org/zap"
	"github.com/gorilla/mux"

	"strings"
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

type Runner struct {
	lock	sync.Mutex
	cmd	*exec.Cmd
	q	*xqueue.Queue
	tmous	int64
	lang	string
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

func restartRunner(runner *Runner) {
	if runner.cmd.Process.Kill() != nil {
		/* Nothing else, but kill outselves, the pod will exit
		 * and k8s will restart us
		 */
		os.Exit(1)
	}

	runner.cmd.Wait()
	runner.q.Close()
	startQnR(runner)
}

func startRunner(lang string, tmous int64) (*Runner, error) {
	var err error
	p := make([]int, 2)

	runner := &Runner {lang: lang, tmous: tmous}

	err = syscall.Pipe(p)
	if err != nil {
		return nil, fmt.Errorf("Can't make out pipe: %s", err.Error())
	}

	runner.fout = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	syscall.CloseOnExec(p[0])
	runner.fin = os.NewFile(uintptr(p[0]), "runner.stdout")

	err = syscall.Pipe(p)
	if err != nil {
		return nil, fmt.Errorf("Can't make err pipe: %s", err.Error())
	}

	runner.ferr = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	syscall.CloseOnExec(p[0])
	runner.fine = os.NewFile(uintptr(p[0]), "runner.stderr")

	err = startQnR(runner)
	if err != nil {
		return nil, err
	}

	return runner, nil
}

var runners = map[string]string {
	"golang": "/go/src/swycode/function",
	"python": "/usr/bin/swy-runner.py",
	"swift": "/swift/swycode/debug/function",
}

func startQnR(runner *Runner) error {
	var err error

	runner.q, err = xqueue.MakeQueue()
	if err != nil {
		return fmt.Errorf("Can't make queue: %s", err.Error())
	}

	err = runner.q.RcvTimeout(runner.tmous)
	if err != nil {
		return fmt.Errorf("Can't set receive timeout: %s", err.Error())
	}

	runner.cmd = exec.Command(runners[runner.lang], runner.q.GetId(), runner.fout, runner.ferr)
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

func doRun(runner *Runner, body []byte) (*swyapi.SwdFunctionRunResult, error) {
	var err error

	runner.lock.Lock()
	defer runner.lock.Unlock()

	start := time.Now()
	err = runner.q.SendBytes(body)
	if err != nil {
		return nil, fmt.Errorf("Can't send args: %s", err.Error())
	}

	var res string
	res, err = runner.q.RecvStr()

	ret := &swyapi.SwdFunctionRunResult{
		Stdout: readLines(runner.fin),
		Stderr: readLines(runner.fine),
		Time: uint(time.Since(start) / time.Microsecond),
	}

	if err == nil {
		if res[0] == '0' {
			ret.Code = 0
		} else {
			ret.Code = http.StatusInternalServerError
		}
		ret.Return = res[2:]
	} else {
		restartRunner(runner)

		switch {
		case err == io.EOF:
			ret.Code = http.StatusInternalServerError
			ret.Return = "exited"
		case err == xqueue.TIMEOUT:
			ret.Code = swyhttp.StatusTimeoutOccurred
			ret.Return = "timeout"
		default:
			log.Errorf("Can't read data back: %s", err.Error())
			ret.Code = http.StatusInternalServerError
			ret.Return = "unknown"
		}
	}

	return ret, nil
}

var builders = map[string]func(*swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	"golang": doBuildGo,
	"swift": doBuildSwift,
}

var buildlock sync.Mutex
var buildlang string

func doBuild(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	buildlock.Lock()
	defer buildlock.Unlock()

	fn, ok := builders[buildlang]
	if !ok {
		return nil, fmt.Errorf("No builder for %s", buildlang)
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

func handleRun(runner *Runner, w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	result, err = doRun(runner, body)
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
	spath := "/var/run/swifty/" + strings.Replace(podip, ".", "_", -1)
	os.Remove(spath)
	addr, err := net.ResolveUnixAddr("unixpacket", spath)
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
	swy.InitLogger(log)

	podIP := swy.SafeEnv("SWD_POD_IP", "")
	if podIP == "" {
		log.Fatal("NO POD_IP")
	}

	podPort := swy.SafeEnv("SWD_PORT", "")
	if podPort == "" {
		log.Fatal("NO PORT")
	}

	lang := swy.SafeEnv("SWD_LANG", "")
	if lang == "" {
		log.Fatal("SWD_LANG not set")
	}

	r := mux.NewRouter()

	inst := swy.SafeEnv("SWD_INSTANCE", "")
	if inst == "build" {
		buildlang = lang
		r.HandleFunc("/v1/run", handleBuild)
	} else {
		tmos := swy.SafeEnv("SWD_FN_TMO", "")
		if tmos == "" {
			log.Fatal("SWD_FN_TMO not set")
		}

		tmo, err := strconv.Atoi(tmos)
		if err != nil {
			log.Fatal("Bad timeout value")
		}

		podToken := swy.SafeEnv("SWD_POD_TOKEN", "")
		if podToken == "" {
			log.Fatal("SWD_POD_TOKEN not set")
		}

		tmous := int64((time.Duration(tmo) * time.Millisecond) / time.Microsecond)
		runner, err := startRunner(lang, tmous)
		if err != nil {
			log.Fatal("Can't start runner")
		}

		err = startCResponder(podIP)
		if err != nil {
			log.Fatal("Can't start cresponder: %s", err.Error())
		}


		r.HandleFunc("/v1/run/" + podToken,
				func(w http.ResponseWriter, r *http.Request) {
					handleRun(runner, w, r)
				})
	}

	srv := &http.Server{
		Handler:	r,
		Addr:		podIP + ":" + podPort,
		WriteTimeout:	60 * time.Second,
		ReadTimeout:	60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
