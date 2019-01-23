/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"github.com/gorilla/mux"
	"encoding/json"
	"strings"
	"net/http"
	"sync"
	"syscall"
	"net"
	"os"

	"swifty/common/xqueue"
)

type proxyRunner struct {
	wc	*net.UnixConn
	rkey	string
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
	podtok := v["podtok"]
	podip := v["podip"]
	rkey := podtok + "/" + podip

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

