package main

import (
	"context"
	"time"
	"reflect"
	"os"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"container/list"
	"../apis"
	"../common/xrest"
)

const GateTracerPath = "/var/run/swifty/gate"

type Tracer struct {
	ten	string
	evs	chan *swyapi.TracerEvent
	l	*list.Element
}

var tLock sync.RWMutex
var tracers *list.List

func traceRequest(ctx context.Context, r *http.Request) {
	if tracers.Len() == 0 {
		return
	}

	traceEventSlow(ctx, "req", map[string]interface{} {
		"method": r.Method,
		"path": r.URL.Path,
	})
}

func traceError(ctx context.Context, ce *xrest.ReqErr) {
	if tracers.Len() == 0 {
		return
	}

	traceEventSlow(ctx, "error", map[string]interface{} {
		"code": ce.Code,
		"message": ce.Message,
	})
}

func traceResponce(ctx context.Context, r interface{}) {
	if tracers.Len() == 0 {
		return
	}

	traceEventSlow(ctx, "resp", map[string]interface{} {
		"values": reflect.TypeOf(r).String(),
	})
}

func traceEventSlow(ctx context.Context, typ string, values map[string]interface{}) {
	gct := gctx(ctx)

	evt := &swyapi.TracerEvent {
		Ts: time.Now(),
		Type: typ,
		RqID: gct.ReqId,
		Data: values,
	}

	tLock.RLock()
	for e := tracers.Front(); e != nil; e = e.Next() {
		t := e.Value.(*Tracer)
		if t.ten == gct.Tenant {
			t.evs <-evt
		}
	}
	tLock.RUnlock()
}

func addTracer(tenant string) *Tracer {
	glog.Debugf("Setup tracer for %s client (%d already)", tenant, tracers.Len())

	t := Tracer{
		ten: tenant,
		evs: make(chan *swyapi.TracerEvent),
	}

	tLock.Lock()
	t.l = tracers.PushBack(&t)
	tLock.Unlock()

	return &t
}

func delTracer(t *Tracer) {
	/*
	 * There can be a guy sitting in traceEventSlow and waiting for ...
	 * us? to release the evs chan space for the next message
	 */
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-t.evs:
				;
			case <-done:
				return
			}
		}
	}()

	tLock.Lock()
	tracers.Remove(t.l)
	tLock.Unlock()
	done <-true

	glog.Debugf("Terminating tracer for %s (%d left)", t.ten, tracers.Len())
}

func tracerRun(cln *net.UnixConn) {
	defer cln.Close()

	msg := make([]byte, 256)
	l, err := cln.Read(msg)
	if err != nil {
		glog.Debugf("Error getting tracer hello: %s", err.Error())
		return
	}

	var hm swyapi.TracerHello
	err = json.Unmarshal(msg[:l], &hm)
	if err != nil {
		glog.Debugf("Error parsing tracer hello: %s", err.Error())
		return
	}

	t := addTracer(hm.Tenant)
	defer delTracer(t)

	stop := make(chan bool)

	go func() {
		x := make([]byte, 1)
		_, err = cln.Read(x)
		stop <-true
	}()

	for {
		select {
		case ev := <-t.evs:
			dat, _ := json.Marshal(ev)
			l, err = cln.Write(dat)
			if err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}

func tracerListen(sk *net.UnixListener) {
	for {
		cln, err := sk.AcceptUnix()
		if err != nil {
			glog.Errorf("Can't accept tracer connection: %s", err.Error())
			break
		}

		glog.Debugf("Attached tracer %s", cln.RemoteAddr().String())
		go tracerRun(cln)
	}
}

func tracerInit() error {
	tracers = list.New()
	xrest.TraceFn = traceResponce

	os.Remove(GateTracerPath)
	addr, err := net.ResolveUnixAddr("unixpacket", GateTracerPath)
	if err != nil {
		return err
	}

	sk, err := net.ListenUnix("unixpacket", addr)
	if err != nil {
		glog.Errorf("Cannot bind unix socket to " + GateTracerPath)
		return err
	}

	go tracerListen(sk)

	return nil
}
