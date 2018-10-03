package main

import (
	"go.uber.org/zap"
	"github.com/gorilla/mux"
	"errors"
	"encoding/json"
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
	"../apis"
)

type Runner struct {
	lock	sync.Mutex
	q	*xqueue.Queue
	fin	*os.File
	fine	*os.File

	ready	bool
	restart	func(*Runner)
	l	*localRunner
	p	*proxyRunner
}

type localRunner struct {
	cmd	*exec.Cmd
	lang	*LangDesc
	suff	string
	tmous	int64
	fout	string
	ferr	string
}

type proxyRunner struct {
	wc	*net.UnixConn
	rkey	string
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

func stopLocal(runner *Runner) {
	if runner.l.cmd.Process.Kill() != nil {
		/* Nothing else, but kill outselves, the pod will exit
		* and k8s will restart us
		*/
		os.Exit(1)
	}

	runner.l.cmd.Wait()
	runner.q.Close()
}

func restartLocal(runner *Runner) {
	stopLocal(runner)
	startQnR(runner)
}

func makeExecutablePath(path string) {
	s := strings.Split(path, "/")
	sp := ""
	for _, p := range s[1:] {
		sp += "/" + p

		st, _ := os.Stat(sp)
		if st != nil {
			os.Chmod(sp, st.Mode() | 0005)
		}
	}
}


/*
 * Kuber mounts all volumes with root-only perms. This hass been
 * dicussed in the github PR-s, but so far no good solutions. Thus
 * explicitly grant r and x bits for everything that needs it.
 */

func mkExecPath(ld *LangDesc, suff string) {
	exec.Command("chmod", "-R", "o+rX", "/function").Run()
}

func mkExecRunner(ld *LangDesc, suff string) {
	makeExecutablePath(ld.runner + suff)
}

func makeLocalRunner(lang string, tmous int64, suff string) (*Runner, error) {
	var err error
	p := make([]int, 2)

	ld := ldescs[lang]
	lr := &localRunner{lang: &ld, tmous: tmous, suff: suff}
	runner := &Runner {l: lr, restart: restartLocal, ready: true}

	err = syscall.Pipe(p)
	if err != nil {
		return nil, fmt.Errorf("Can't make out pipe: %s", err.Error())
	}

	lr.fout = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	syscall.CloseOnExec(p[0])
	runner.fin = os.NewFile(uintptr(p[0]), "runner.stdout")

	err = syscall.Pipe(p)
	if err != nil {
		return nil, fmt.Errorf("Can't make err pipe: %s", err.Error())
	}

	lr.ferr = strconv.Itoa(p[1])
	syscall.SetNonblock(p[0], true)
	syscall.CloseOnExec(p[0])
	runner.fine = os.NewFile(uintptr(p[0]), "runner.stderr")

	ld.prep(&ld, suff)

	err = startQnR(runner)
	if err != nil {
		return nil, err
	}

	return runner, nil
}

type buildFn func(*swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error)

type LangDesc struct {
	runner		string
	build		buildFn
	prep		func(*LangDesc, string)
}

var ldescs = map[string]LangDesc {
	"golang": LangDesc {
		runner:	"/go/src/swycode/runner",
		build:	doBuildGo,
		prep:	mkExecRunner,
	},
	"python": LangDesc {
		runner:	"/usr/bin/swy-runner.py",
		prep:	mkExecPath,
	},
	"swift": LangDesc {
		runner:	"/swift/swycode/debug/runner",
		build:	doBuildSwift,
		prep:	mkExecRunner,
	},
	"nodejs": LangDesc {
		runner:	"/home/swifty/runner-js.sh",
		prep:	mkExecPath,
	},
	"ruby": LangDesc {
		runner:	"/home/swifty/runner.rb",
		prep:	mkExecPath,
	},
}

func startQnR(runner *Runner) error {
	var err error

	runner.q, err = xqueue.MakeQueue()
	if err != nil {
		return fmt.Errorf("Can't make queue: %s", err.Error())
	}

	err = runner.q.RcvTimeout(runner.l.tmous)
	if err != nil {
		return fmt.Errorf("Can't set receive timeout: %s", err.Error())
	}

	var bin, scr string

	if runner.l.lang.build == nil {
		/* /bin/interpreter script${suff}.ext */
		bin = runner.l.lang.runner
		scr = "script" + runner.l.suff
	} else {
		/* /function${suff} - */
		bin = runner.l.lang.runner + runner.l.suff
		scr = "-"
	}

	runner.l.cmd = exec.Command("/usr/bin/swy-runner",
					runner.l.fout, runner.l.ferr,
					runner.q.GetId(), bin, scr)
	err = runner.l.cmd.Start()
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

type RunnerRes struct {
	Res	int
	Status	int
	Ret	string
}

func doRun(runner *Runner, body []byte) (*swyapi.SwdFunctionRunResult, error) {
	var err error

	start := time.Now()
	err = runner.q.SendBytes(body)
	if err != nil {
		return nil, fmt.Errorf("Can't send args: %s", err.Error())
	}

	var out RunnerRes
	err = runner.q.Recv(&out)

	ret := &swyapi.SwdFunctionRunResult{
		Stdout: readLines(runner.fin),
		Stderr: readLines(runner.fine),
		Time: uint(time.Since(start) / time.Microsecond),
	}

	if err == nil {
		if out.Res == 0 {
			fmt.Printf("Will report %d\n", out.Status)
			ret.Code = out.Status
		} else {
			ret.Code = -http.StatusInternalServerError
		}
		ret.Return = out.Ret
	} else {
		switch {
		case err == io.EOF:
			ret.Code = -http.StatusInternalServerError
			ret.Return = "exited"
		case err == xqueue.TIMEOUT:
			ret.Code = -xhttp.StatusTimeoutOccurred
			ret.Return = "timeout"
		default:
			log.Errorf("Can't read data back: %s", err.Error())
			ret.Code = -http.StatusInternalServerError
			ret.Return = "unknown"
		}
	}

	return ret, nil
}

var buildlock sync.Mutex

/*
 * All functions sit at /go/src/swycode/
 * Runner sits at /go/src/swyrunner/
 */
const goScript = "/go/src/swyrunner/script.go"

func doBuildGo(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove(goScript)
	srcdir := params.Sources
	err := os.Symlink("/go/src/swycode/" + srcdir + "/script" + params.Suff + ".go", goScript)
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run go build on %s", srcdir)
	cmd := exec.Command("go", "build", "-o", "../swycode/" + srcdir + "/runner" + params.Suff)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = "/go/src/swyrunner"
	err = cmd.Run()
	os.Remove(goScript)

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
const swiftScript = "/swift/runner/Sources/script.swift"

func doBuildSwift(params *swyapi.SwdFunctionBuild) (*swyapi.SwdFunctionRunResult, error) {
	os.Remove(swiftScript)
	srcdir := params.Sources
	err := os.Symlink("/swift/swycode/" + srcdir + "/script" + params.Suff + ".swift", swiftScript)
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run swift build on %s", srcdir)
	cmd := exec.Command("swift", "build", "--build-path", "../swycode/" + srcdir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = "/swift/runner"
	err = cmd.Run()
	os.Remove(swiftScript)
	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.SwdFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	err = os.Rename("/swift/swycode/debug/function", "/swift/swycode/debug/runner" + params.Suff)
	if err != nil {
		return nil, fmt.Errorf("Can't rename binary: %s", err.Error())
	}

	return &swyapi.SwdFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func handleTry(lang string, tmous int64, w http.ResponseWriter, r *http.Request) {
	suff := mux.Vars(r)["suff"]

	runner, err := makeLocalRunner(lang, tmous, suff)
	if err == nil {
		handleRun(runner, w, r)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Errorf("%s", err.Error())
	}
	stopLocal(runner)
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
	runner.lock.Lock()
	if runner.ready {
		result, err = doRun(runner, body)
		if err != nil || result.Code != 0 {
			runner.restart(runner)
		}
	} else {
		err = errors.New("Runner not ready")
	}
	runner.lock.Unlock()
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("%s", err.Error())
}

func handleBuild(w http.ResponseWriter, r *http.Request, fn buildFn) {
	defer r.Body.Close()
	var params swyapi.SwdFunctionBuild
	var result *swyapi.SwdFunctionRunResult

	code := http.StatusBadRequest
	err := xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	buildlock.Lock()
	result, err = fn(&params)
	buildlock.Unlock()
	if err != nil {
		log.Errorf("Error building FN: %s", err.Error())
		goto out
	}

	err = xhttp.Respond(w, result)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), code)
	log.Errorf("%s", err.Error())
}

var prox_runners sync.Map
var prox_lock sync.Mutex

type runnerInfo struct {
}

func makeProxyRunner(rkey string) (*Runner, error) {
	var c *net.UnixConn
	var rfds []int
	var rinf runnerInfo
	var mn, cn int
	var scms []syscall.SocketControlMessage
	var pr *proxyRunner
	var runner *Runner

	msg := make([]byte, 1024)
	cmsg := make([]byte, 1024)

	wadd, err := net.ResolveUnixAddr("unixpacket", "/var/run/swifty/wdogconn/" + rkey)
	if err != nil {
		log.Errorf("Can't resolve wdogconn addr: %s", err.Error())
		goto er
	}

	c, err = net.DialUnix("unixpacket", nil, wadd)
	if err != nil {
		log.Errorf("Can't connect wdogconn: %s", err.Error())
		goto er
	}

	mn, cn, _, _, err = c.ReadMsgUnix(msg, cmsg)
	if err != nil {
		log.Errorf("Can't get runner creds: %s", err.Error())
		goto erc
	}

	scms, err = syscall.ParseSocketControlMessage(cmsg[:cn])
	if err != nil {
		log.Errorf("Can't parse sk cmsg: %s", err.Error())
		goto erc
	}

	if len(scms) != 1 {
		log.Errorf("Need one scm, got %d", len(scms))
		goto erc
	}

	rfds, err = syscall.ParseUnixRights(&scms[0])
	if err != nil {
		log.Errorf("Can't parse scm rights: %s", err.Error())
		goto erc
	}

	err = json.Unmarshal(msg[:mn], &rinf)
	if err != nil {
		log.Errorf("Can't unmarshal runner info: %s", err.Error())
		goto ercc
	}

	/* FIXME -- up above we might have leaked the received FDs... */

	pr = &proxyRunner{rkey: rkey, wc: c}
	runner = &Runner{p: pr, restart: restartProxy, ready: true}
	runner.fin = os.NewFile(uintptr(rfds[0]), "runner.stdout")
	runner.fine = os.NewFile(uintptr(rfds[1]), "runner.stderr")
	runner.q = xqueue.OpenQueueFd(rfds[2])

	return runner, nil

ercc:
	for _, fd := range(rfds) {
		syscall.Close(fd)
	}
erc:
	c.Close()
er:
	return nil, err
}

func restartProxy(runner *Runner) {
	runner.q.Close()
	runner.fin.Close()
	runner.fine.Close()
	runner.p.wc.Close()
	prox_runners.Delete(runner.p.rkey)

	runner.p.wc = nil
	runner.ready = false
}

func handleProxy(w http.ResponseWriter, req *http.Request) {
	var runner *Runner

	v := mux.Vars(req)
	fnid := v["fnid"]
	podip := v["podip"]
	rkey := fnid + "/" + podip

	r, ok := prox_runners.Load(rkey)
	if ok {
		runner = r.(*Runner)
	} else {
		prox_lock.Lock()
		r, ok := prox_runners.Load(rkey)
		if ok {
			runner = r.(*Runner)
		} else {
			var err error

			runner, err = makeProxyRunner(rkey)
			if err != nil {
				prox_lock.Unlock()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			prox_runners.Store(rkey, runner)

			/* Watchdog for wdog disappearing */
			go func() {
				b := make([]byte, 1)
				runner.p.wc.Read(b)
				runner.lock.Lock()
				if runner.p.wc != nil {
					restartProxy(runner)
				}
				runner.lock.Unlock()
			}()
		}
		prox_lock.Unlock()
	}

	handleRun(runner, w, req)
}

func startCResponder(runner *Runner, podip string) error {
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
		var msg, cmsg []byte
		b := make([]byte, 1)
		for {
			cln, err := sk.AcceptUnix()
			if err != nil {
				log.Errorf("Can't accept cresponder connection: %s", err.Error())
				break
			}

			log.Debugf("CResponder accepted conn")
			runner.lock.Lock()
			msg, err = json.Marshal(&runnerInfo{})
			if err != nil {
				goto skip
			}

			cmsg = syscall.UnixRights(int(runner.fin.Fd()), int(runner.fine.Fd()), runner.q.Fd())
			_, _, err = cln.WriteMsgUnix(msg, cmsg, nil)
			if err != nil {
				goto skip
			}

			runner.ready = false
			runner.lock.Unlock()

			cln.Read(b)
			log.Debugf("Proxy disconnected, restarting runner")

			runner.lock.Lock()
			runner.ready = true
			restartLocal(runner)

		skip:
			runner.lock.Unlock()
			cln.Close()
		}
	}()

	return nil
}

func main() {
	podIP := xh.SafeEnv("SWD_POD_IP", "")
	if podIP == "" {
		log.Fatal("NO POD_IP")
	}

	podPort := xh.SafeEnv("SWD_PORT", "")
	if podPort == "" {
		log.Fatal("NO PORT")
	}

	r := mux.NewRouter()

	inst := xh.SafeEnv("SWD_INSTANCE", "")
	if inst == "build" {
		lang := xh.SafeEnv("SWD_LANG", "")
		if lang == "" {
			log.Fatal("SWD_LANG not set")
		}

		ld, ok := ldescs[lang]
		if !ok || ld.build == nil {
			log.Fatal("No build handler for lang")
		}

		r.HandleFunc("/v1/run", func(w http.ResponseWriter, r *http.Request) {
			handleBuild(w, r, ld.build)
		})
	} else if inst == "proxy" {
		r.HandleFunc("/v1/run/{fnid}/{podip}", handleProxy)
	} else {
		lang := xh.SafeEnv("SWD_LANG", "")
		if lang == "" {
			log.Fatal("SWD_LANG not set")
		}

		tmos := xh.SafeEnv("SWD_FN_TMO", "")
		if tmos == "" {
			log.Fatal("SWD_FN_TMO not set")
		}

		tmo, err := strconv.Atoi(tmos)
		if err != nil {
			log.Fatal("Bad timeout value")
		}

		podToken := xh.SafeEnv("SWD_POD_TOKEN", "")
		if podToken == "" {
			log.Fatal("SWD_POD_TOKEN not set")
		}

		tmous := int64((time.Duration(tmo) * time.Millisecond) / time.Microsecond)
		runner, err := makeLocalRunner(lang, tmous, "")
		if err != nil {
			log.Fatal("Can't start runner")
		}

		err = startCResponder(runner, podIP)
		if err != nil {
			log.Fatal("Can't start cresponder: %s", err.Error())
		}

		r.HandleFunc("/v1/run/" + podToken,
				func(w http.ResponseWriter, r *http.Request) {
					handleRun(runner, w, r)
				})
		r.HandleFunc("/v1/run/" + podToken + "/{suff}",
				func(w http.ResponseWriter, r *http.Request) {
					handleTry(lang, tmous, w, r)
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
