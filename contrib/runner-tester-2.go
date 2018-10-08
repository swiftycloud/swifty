package main

import (
	"errors"
	"encoding/json"
	"net/http"
	"time"
	"sync"
	"syscall"
	"fmt"
	"io"
	"net"
	"os"

	"../src/common/http"
	"../src/common/xqueue"
	"../src/apis"
)

type Runner struct {
	lock	sync.Mutex
	q	*xqueue.Queue
	fin	*os.File
	fine	*os.File

	ready	bool
	restart	func(*Runner)
	p	*proxyRunner
}

type proxyRunner struct {
	wc	*net.UnixConn
	rkey	string
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
	x1 := time.Since(start)

	var out RunnerRes
	err = runner.q.Recv(&out)

	x2 := time.Since(start)

	ret := &swyapi.SwdFunctionRunResult{
		Stdout: readLines(runner.fin),
		Stderr: readLines(runner.fine),
		Time: uint(time.Since(start) / time.Microsecond),
	}

	x3 := time.Since(start)

	fmt.Printf(">   %10d %10d %10d\n", x1, x2, x3)

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
			fmt.Printf("Can't read data back: %s", err.Error())
			ret.Code = -http.StatusInternalServerError
			ret.Return = "unknown"
		}
	}

	return ret, nil
}

var glock sync.Mutex

func handleRun(runner *Runner, body string) {
	var result *swyapi.SwdFunctionRunResult
	var err error

	runner.lock.Lock()
	if runner.ready {
		result, err = doRun(runner, []byte(body))
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

	return

out:
	fmt.Printf("Error running: %s", err.Error())
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
		fmt.Printf("Can't resolve wdogconn addr: %s", err.Error())
		goto er
	}

	c, err = net.DialUnix("unixpacket", nil, wadd)
	if err != nil {
		fmt.Printf("Can't connect wdogconn: %s", err.Error())
		goto er
	}

	mn, cn, _, _, err = c.ReadMsgUnix(msg, cmsg)
	if err != nil {
		fmt.Printf("Can't get runner creds: %s", err.Error())
		goto erc
	}

	scms, err = syscall.ParseSocketControlMessage(cmsg[:cn])
	if err != nil {
		fmt.Printf("Can't parse sk cmsg: %s", err.Error())
		goto erc
	}

	if len(scms) != 1 {
		fmt.Printf("Need one scm, got %d", len(scms))
		goto erc
	}

	rfds, err = syscall.ParseUnixRights(&scms[0])
	if err != nil {
		fmt.Printf("Can't parse scm rights: %s", err.Error())
		goto erc
	}

	err = json.Unmarshal(msg[:mn], &rinf)
	if err != nil {
		fmt.Printf("Can't unmarshal runner info: %s", err.Error())
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
	fmt.Printf("Stopping %s", runner.p.rkey)
	runner.q.Close()
	runner.fin.Close()
	runner.fine.Close()
	runner.p.wc.Close()
	prox_runners.Delete(runner.p.rkey)

	runner.p.wc = nil
	runner.ready = false
}

func main() {
	var runner *Runner

	rkey := os.Args[1]
	dir := os.Args[2]
	body := os.Args[3]

	for i := 0; i < 128; i++ {
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

				fmt.Printf("Proxifying %s\n", rkey)
				runner, err = makeProxyRunner(dir, rkey)
				if err != nil {
					prox_lock.Unlock()
					fmt.Printf("Cannot make proxy runner: %s\n", err.Error())
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

		handleRun(runner, body)
	}
}
