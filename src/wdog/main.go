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
	"time"
	"sync"
	"path/filepath"
	"syscall"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"

	"swifty/common"
	"swifty/common/http"
	"swifty/common/xqueue"
	"swifty/apis"
)

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

type buildFn func(*swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error)

type LangDesc struct {
	runner		string
	build		buildFn
	prep		func(*LangDesc, string)
	info		func() (string, []string, error)
	packages	func(string) ([]string, error)
	remove		func(string, string) error
}

func goList(dir string) ([]string, error) {
	stuff := []string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, "/.git") {
			path, _ = filepath.Rel(dir, path) // Cut the packages folder
			path = filepath.Dir(path)       // Cut the .git one
			stuff = append(stuff, path)
			return filepath.SkipDir
		}

		return nil
	})

	if err != nil {
		return nil, errors.New("Error listing packages")
	}

	return stuff, nil
}

func goInfo() (string, []string, error) {
	v, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := goList("/go/src")
	if err != nil {
		return "", nil, err
	}

	return string(v), ps, nil
}

func goPackages(tenant string) ([]string, error) {
	return goList("/go-pkg/" + tenant + "/golang/src")
}

const (
        goOsArch string = "linux_amd64" /* FIXME -- run go env and parse */
)

func goRemove(tenant, name string) error {
	if strings.Contains(name, "..") {
		return errors.New("Bad package name")
	}

	d := "/go-pkg/" + tenant + "/golang"
	st, err := os.Stat(d + "/src/" + name + "/.git")
	if err != nil || !st.IsDir() {
		return errors.New("Package not installed")
	}

	err = os.Remove(d + "/pkg/" + goOsArch + "/" + name + ".a")
	if err != nil {
		log.Errorf("Can't remove %s' package %s: %s", tenant, name, err.Error())
		return errors.New("Error removing pkg")
	}

	x, err := xh.DropDir(d, "/src/" + name)
	if err != nil {
		log.Errorf("Can't remove %s' sources %s (%s): %s", tenant, name, x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func pyInfo() (string, []string, error) {
	v, err := exec.Command("python3", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := exec.Command("pip3", "list", "--format", "freeze").Output()
	if err != nil {
		return "", nil, err
	}

	return string(v), xh.GetLines(ps), nil
}

func xpipPackages(tenant string) ([]string, error) {
	ps, err := exec.Command("python3", "/usr/bin/xpip.py", tenant, "list").Output()
	if err != nil {
		return nil, err
	}

	return xh.GetLines(ps), nil
}

func xpipRemove(tenant, name string) error {
	return exec.Command("python3", "/usr/bin/xpip.py", tenant, "remove", name).Run()
}

func nodeInfo() (string, []string, error) {
	v, err := exec.Command("node", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	out, err := exec.Command("npm", "list").Output()
	if err != nil {
		return "", nil, err
	}

	o := xh.GetLines(out)
	ret := []string{}
	if len(o) > 0 {
		for _, p := range(o[1:]) {
			ps := strings.Fields(p)
			ret = append(ret, ps[len(ps)-1])
		}
	}

	return string(v), ret, nil
}

func nodeModules(tenant string) ([]string, error) {
	stuff := []string{}

	d := "/packages/" + tenant + "/nodejs/node_modules"
	dir, err := os.Open(d)
	if err != nil {
		return nil, errors.New("Error accessing node_modules")
	}

	ents, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return nil, errors.New("Error reading node_modules")
	}

	for _, sd := range ents {
		_, err := os.Stat(d + "/" + sd + "/package.json")
		if err == nil {
			stuff = append(stuff, sd)
		}
	}

	return stuff, nil
}

func nodeRemove(tenant, name string) error {
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		return errors.New("Bad package name")
	}

	d := "/packages/" + tenant + "/nodejs/node_modules"
	_, err := os.Stat(d + "/" + name + "/package.json")
	if err != nil {
		return errors.New("Package not installed")
	}

	x, err := xh.DropDir(d, name)
	if err != nil {
		log.Errorf("Can't remove %s' sources %s (%s): %s", tenant, name, x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func rubyInfo() (string, []string, error) {
	v, err := exec.Command("ruby", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := exec.Command("gem", "list").Output()
	if err != nil {
		return "", nil, err
	}

	return string(v), xh.GetLines(ps), nil
}

var ldescs = map[string]*LangDesc {
	"golang": &LangDesc {
		runner:	"/go/src/swycode/runner",
		build:	doBuildGo,
		prep:	mkExecRunner,
		info:	goInfo,
		packages: goPackages,
		remove:   goRemove,
	},
	"python": &LangDesc {
		runner:	"/usr/bin/swy-runner.py",
		prep:	mkExecPath,
		info:	pyInfo,
		packages: xpipPackages,
		remove:   xpipRemove,
	},
	"swift": &LangDesc {
		runner:	"/swift/swycode/debug/runner",
		build:	doBuildSwift,
		prep:	mkExecRunner,
	},
	"nodejs": &LangDesc {
		runner:	"/home/swifty/runner-js.sh",
		prep:	mkExecPath,
		info:	nodeInfo,
		packages: nodeModules,
		remove:   nodeRemove,
	},
	"ruby": &LangDesc {
		runner:	"/home/swifty/runner.rb",
		prep:	mkExecPath,
		info:	rubyInfo,
	},
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

func doRun(runner *Runner, body []byte) (*swyapi.WdogFunctionRunResult, error) {
	var err error

	start := time.Now()
	err = runner.q.SendBytes(body)
	if err != nil {
		log.Debugf("%s", readLines(runner.fin))
		log.Debugf("%s", readLines(runner.fine))
		return nil, fmt.Errorf("Can't send args: %s", err.Error())
	}

	var out RunnerRes
	err = runner.q.Recv(&out)

	ret := &swyapi.WdogFunctionRunResult{
		Stdout: readLines(runner.fin),
		Stderr: readLines(runner.fine),
		Time: uint(time.Since(start) / time.Microsecond),
	}

	if err == nil {
		if out.Res == 0 {
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

var glock sync.Mutex

func handleTry(lang string, tmous int64, w http.ResponseWriter, r *http.Request) {
	suff := mux.Vars(r)["suff"]

	glock.Lock()
	runner, err := makeLocalRunner(lang, tmous, suff)
	if err == nil {
		handleRun(runner, w, r)
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Errorf("%s", err.Error())
	}
	stopLocal(runner)
	glock.Unlock()
}

func handleRun(runner *Runner, w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var result *swyapi.WdogFunctionRunResult

	code := http.StatusBadRequest
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	runner.lock.Lock()
	if runner.ready {
		result, err = doRun(runner, body)
		if err != nil || result.Code < 0 {
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

func listPackages(w http.ResponseWriter, ld *LangDesc, tenant string) {
	var res []string

	err := errors.New("Not implemented")
	if ld.packages == nil {
		goto out
	}

	res, err = ld.packages(tenant)
	if err != nil {
		goto out
	}

	err = xhttp.Respond(w, &res)
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), http.StatusInternalServerError)
	log.Errorf("%s", err.Error())
}

func deletePackage(w http.ResponseWriter, r *http.Request, ld *LangDesc, tenant string) {
	var rq swyapi.Package

	err := errors.New("Not implemented")
	if ld.remove == nil {
		goto out
	}

	err = xhttp.RReq(r, &rq)
	if err != nil {
		goto out
	}

	err = ld.remove(tenant, rq.Name)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
	return

out:
	http.Error(w, err.Error(), http.StatusInternalServerError)
	log.Errorf("%s", err.Error())
}

func handlePackages(w http.ResponseWriter, r *http.Request, ld *LangDesc, tenant string) {
	switch r.Method {
	case "GET":
		listPackages(w, ld, tenant)
	case "DELETE":
		deletePackage(w, r, ld, tenant)
	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}

func handleInfo(w http.ResponseWriter, r *http.Request, ld *LangDesc) {
	if ld.info == nil {
		w.WriteHeader(http.StatusNotImplemented)
		return
	}

	v, pkgs, err := ld.info()
	err = xhttp.Respond(w, &swyapi.LangInfo{ Version: v, Packages: pkgs })
	if err != nil {
		goto out
	}

	return

out:
	http.Error(w, err.Error(), http.StatusInternalServerError)
	log.Errorf("%s", err.Error())
}

func handleBuild(w http.ResponseWriter, r *http.Request, fn buildFn) {
	defer r.Body.Close()
	var params swyapi.WdogFunctionBuild
	var result *swyapi.WdogFunctionRunResult

	code := http.StatusBadRequest
	err := xhttp.RReq(r, &params)
	if err != nil {
		goto out
	}

	code = http.StatusInternalServerError
	glock.Lock()
	result, err = fn(&params)
	glock.Unlock()
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

func makeProxyRunner(dir, rkey string) (*Runner, error) {
	var c *net.UnixConn
	var rfds []int
	var rinf runnerInfo
	var mn, cn int
	var scms []syscall.SocketControlMessage
	var pr *proxyRunner
	var runner *Runner

	msg := make([]byte, 1024)
	cmsg := make([]byte, 1024)

	wadd, err := net.ResolveUnixAddr("unixpacket", dir + "/" + rkey)
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
	log.Debugf("Stopping %s", runner.p.rkey)
	runner.q.Close()
	runner.fin.Close()
	runner.fine.Close()
	runner.p.wc.Close()
	prox_runners.Delete(runner.p.rkey)

	runner.p.wc = nil
	runner.ready = false
}

func handleProxy(dir string, w http.ResponseWriter, req *http.Request) {
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

			log.Debugf("Proxifying %s", rkey)
			runner, err = makeProxyRunner(dir, rkey)
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

func startCResponder(runner *Runner, dir, podip string) error {
	spath := dir + "/" + strings.Replace(podip, ".", "_", -1)
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
	if inst == "service" {
		lang := xh.SafeEnv("SWD_LANG", "")
		if lang == "" {
			log.Fatal("SWD_LANG not set")
		}

		ld, ok := ldescs[lang]
		if !ok {
			log.Fatal("No handler for lang")
		}

		if ld.build != nil {
			r.HandleFunc("/v1/build", func(w http.ResponseWriter, r *http.Request) {
				handleBuild(w, r, ld.build)
			})
		}

		r.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.HandleFunc("/v1/info", func(w http.ResponseWriter, r *http.Request) {
			handleInfo(w, r, ld)
		})
		r.HandleFunc("/v1/packages/{tenant}", func(w http.ResponseWriter, r *http.Request) {
			ten := mux.Vars(r)["tenant"]
			handlePackages(w, r, ld, ten)
		})
	} else if inst == "proxy" {
		crespDir := xh.SafeEnv("SWD_CRESPONDER", "")
		if crespDir == "" {
			log.Fatal("SWD_CRESPONDER not set")
		}

		r.HandleFunc("/v1/run/{fnid}/{podip}",
				func(w http.ResponseWriter, r *http.Request) {
					handleProxy(crespDir, w, r)
				})
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

		crespDir := xh.SafeEnv("SWD_CRESPONDER", "")

		tmous := int64((time.Duration(tmo) * time.Millisecond) / time.Microsecond)
		runner, err := makeLocalRunner(lang, tmous, "")
		if err != nil {
			log.Fatal("Can't start runner")
		}

		if crespDir != "" {
			log.Debugf("Starting proxy responder @%s", crespDir)
			err = startCResponder(runner, crespDir, podIP)
			if err != nil {
				log.Fatal("Can't start cresponder: %s", err.Error())
			}
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
