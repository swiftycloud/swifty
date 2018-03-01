package main

import (
	"go.uber.org/zap"

	"net/http"
	"os/exec"
	"strconv"
	"errors"
	"bytes"
	"time"
	"sync"
	"syscall"
	"fmt"
	"io"
	"os"

	"../common"
	"../common/http"
	"../common/xqueue"
	"../apis/apps"
)

var podToken string
var build bool
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

type runnerRes struct {
	Code	int		`json:"code"`
	Return	string		`json:"return"`
}

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
		case <-time.After(time.Duration(fnTmo) * time.Millisecond):
			break
		}

		log.Debugf("Timeout!")

		timeout = true
		xerr := runner.cmd.Process.Kill()
		if xerr != nil {
			log.Errorf("Can't kill runner: %s", xerr.Error())
		}
		<-done
	}()

	var rr runnerRes
	err = runner.q.Recv(&rr)
	done <-true

	rout := readLines(runner.fin)
	rerr := readLines(runner.fine)

	if err != nil {
		if timeout {
			restartRunner()
			return &swyapi.SwdFunctionRunResult{
				Return: "timeout",
				Code: 524, /* A Timeout Occurred */
			}, 0, nil
		}

		if err == io.EOF {
			restartRunner()
			return &swyapi.SwdFunctionRunResult{
				Return: "exited",
				Code: 500,
				Stdout: rout,
				Stderr: rerr,
			}, 0, nil
		}

		err = fmt.Errorf("Can't get back the result: %s", err.Error())
		return nil, code, err
	}

	return &swyapi.SwdFunctionRunResult{
		Code: rr.Code,
		Return: rr.Return,
		Stdout: rout,
		Stderr: rerr,
		/* FIXME -- calc Time and CTime */
	}, 0, nil
}

var builders = map[string]func(*swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	"golang": doBuildGo,
	"swift": doBuildSwift,
}

func doBuild(params *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
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
func doBuildGo(params *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove("/go/src/swyfunc")
	srcdir := params.Args["sources"]
	err := os.Symlink("/go/src/swycode/" + srcdir, "/go/src/swyfunc")
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
	os.Remove("/go/src/swyfunc") /* Just an attempt */

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
func doBuildSwift(params *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove("/swift/runner/Sources/script.swift")
	srcdir := params.Args["sources"]
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
	var params swyapi.SwdFunctionRun
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest

	err := swyhttp.ReadAndUnmarshalReq(r, &params)
	if err != nil {
		goto out
	}

	if params.PodToken != podToken {
		err = errors.New("Pod Token mismatch")
		goto out
	}

	if !build {
		result, code, err = doRun(&params)
	} else {
		result, err = doBuild(&params)
		if err != nil {
			log.Errorf("Error building FN: %s", err.Error())
		}
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
	var err error

	swy.InitLogger(log)

	addr := getSwdAddr()
	if addr == "" {
		log.Fatal("No address specified")
	}

	podToken = swy.SafeEnv("SWD_POD_TOKEN", "")
	if podToken == "" {
		log.Fatal("SWD_POD_TOKEN not set")
	}

	tmos := swy.SafeEnv("SWD_FN_TMO", "")
	if tmos == "" {
		log.Fatal("SWD_FN_TMO not set")
	}

	fnTmo, err = strconv.Atoi(tmos)
	if err != nil {
		log.Fatal("Bad timeout value")
	}

	lang = swy.SafeEnv("SWD_LANG", "")
	if lang == "" {
		log.Fatal("SWD_LANG not set")
	}

	inst := swy.SafeEnv("SWD_INSTANCE", "")
	if inst == "" {
		err = startRunner()
		if err != nil {
			log.Fatal("Can't start runner")
		}
	} else {
		build = true
	}

	http.HandleFunc("/v1/run", handleRun)
	log.Fatal(http.ListenAndServe(addr, nil))
}
