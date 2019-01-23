/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"go.uber.org/zap"
	"github.com/gorilla/mux"
	"errors"
	"strings"
	"net/http"
	"os/exec"
	"strconv"
	"time"
	"sync"
	"syscall"
	"io/ioutil"
	"os"

	"swifty/common"
	"swifty/common/http"
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
	makeExecutablePath(ld._runner + suff)
}

type LangDesc struct {
	_runner		string
	build		bool
	prep		func(*LangDesc, string)
	info		func() (string, []string, error)
	packages	func(string) ([]string, error)
	install		func(string, string) error
	remove		func(string, string) error
}

var ldescs = map[string]*LangDesc {
	"golang": &LangDesc {
		build:	true,
		prep:	mkExecRunner,
		_runner:	"/go/src/swycode/runner",
		info:	goInfo,
		packages: goPackages,
		install:  goInstall,
		remove:   goRemove,
	},
	"python": &LangDesc {
		prep:	mkExecPath,
		info:	pyInfo,
		packages: xpipPackages,
		install:  pipInstall,
		remove:   xpipRemove,
	},
	"swift": &LangDesc {
		build:	true,
		prep:	mkExecRunner,
		_runner:	"/swift/swycode/runner",
	},
	"nodejs": &LangDesc {
		prep:	mkExecPath,
		info:	nodeInfo,
		packages: nodeModules,
		install:  npmInstall,
		remove:   nodeRemove,
	},
	"ruby": &LangDesc {
		prep:	mkExecPath,
		info:	rubyInfo,
	},
	"csharp": &LangDesc {
		build:	true,
		prep:	mkExecPath,
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
	var pks []string
	var res []*swyapi.Package

	err := errors.New("Not implemented")
	if ld.packages == nil {
		goto out
	}

	pks, err = ld.packages(tenant)
	if err != nil {
		goto out
	}

	for _, p := range pks {
		res = append(res, &swyapi.Package{ Name: p })
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

func installPackage(w http.ResponseWriter, r *http.Request, ld *LangDesc, tenant string) {
	var rq swyapi.Package

	err := errors.New("Not implemented")
	if ld.install == nil {
		goto out
	}

	err = xhttp.RReq(r, &rq)
	if err != nil {
		goto out
	}

	err = ld.install(tenant, rq.Name)
	if err != nil {
		goto out
	}

	w.WriteHeader(http.StatusOK)
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
	case "PUT":
		installPackage(w, r, ld, tenant)
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

func handleBuild(w http.ResponseWriter, r *http.Request) {
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
	result, err = doBuildCommon(&params)
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

		if ld.build {
			r.HandleFunc("/v1/build", handleBuild)
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

		r.HandleFunc("/v1/run/{podtok}/{podip}",
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
