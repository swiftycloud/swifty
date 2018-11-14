/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"sync"
	"swifty/common/xqueue"
	"os"
	"os/exec"
	"net"
	"fmt"
	"syscall"
	"strconv"
)

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

type proxyRunner struct {
	wc	*net.UnixConn
	rkey	string
}

