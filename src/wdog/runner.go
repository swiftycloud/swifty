/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"sync"
	"os"
	"os/exec"
	"strings"
	"time"
	"fmt"
	"encoding/json"
	"syscall"
	"strconv"
	"net/http"
	"io"

	"swifty/apis"
	"swifty/common/xqueue"
	"swifty/common/http"
)

type RunnerRes struct {
	/* Runner return code. 0 for OK, non zero for code-run error (e.g. exception) */
	Res	int
	/* Status the code wants to propagate back to caller */
	Status	int
	/* JSON-encoded return value of a function */
	Ret	string
	/* List of actions to be taken after the funciton is called */
	Then	json.RawMessage
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
		Then: out.Then,
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

const lRunner = "/usr/bin/start_runner.sh"

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

	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "SWD_") {
			env = append(env, e)
		}
	}

	runner.l.cmd = exec.Command("/usr/bin/swy-runner",
					runner.l.fout, runner.l.ferr,
					runner.q.GetId(), lRunner, runner.l.suff)
	runner.l.cmd.Env = env
	err = runner.l.cmd.Start()
	if err != nil {
		return fmt.Errorf("Can't start runner: %s", err.Error())
	}

	log.Debugf("Started runner (queue %s)", runner.q.FDS())
	runner.q.Started()
	return nil
}

func makeLocalRunner(lang string, tmous int64, suff string) (*Runner, error) {
	var err error
	p := make([]int, 2)

	ld := ldescs[lang]
	lr := &localRunner{lang: ld, tmous: tmous, suff: suff}
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

	ld.prep(ld, suff)

	err = startQnR(runner)
	if err != nil {
		return nil, err
	}

	return runner, nil
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
