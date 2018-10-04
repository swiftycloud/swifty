package main

import (
	"net/http"
	"fmt"
	"time"
	"context"
	"strconv"
	"strings"
	"io/ioutil"

	"../apis"
	"../common/http"
	"../common/xrest"
	"../common/xratelimit"
)

func makeArgs(sopq *statsOpaque, r *http.Request, path, key string) *swyapi.SwdFunctionRun {
	defer r.Body.Close()

	args := &swyapi.SwdFunctionRun{}
	args.Args = make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args.Args[k] = v[0]
		sopq.argsSz += len(k) + len(v[0])
	}

	body, err := ioutil.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		ct := r.Header.Get("Content-Type")
		ctp := strings.SplitN(ct, ";", 2)
		if len(ctp) > 0 {
			/*
			 * Some comments on the content/type
			 * THe text/plain type is simple
			 * The app/json type means, there's an object
			 * inside and we can decode it rigt in the
			 * runner. On the other hand, decoding the
			 * json into a struct, rather into a generic
			 * map is better for compile-able languages.
			 * Any binary type is better to be handled
			 * with asyncs, as binary data can be big and
			 * tranferring is back and firth is not good.
			 */
			switch ctp[0] {
			case "application/json", "text/plain":
				args.ContentType = ctp[0]
				args.Body = string(body)
				sopq.bodySz = len(body)
			}
		}
	}

	if path == "" {
		path = reqPath(r)
	}
	args.Path = &path
	args.Method = r.Method
	args.Key = key

	return args
}

type podConn struct {
	Addr	string
	Host	string
	Port	string
	Cookie	string
}

func talkHTTP(addr, port, url string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	var resp *http.Response
	var res swyapi.SwdFunctionRunResult
	var err error

	resp, err = xhttp.Req(
			&xhttp.RestReq{
				Address: "http://" + addr + ":" + port + "/v1/run/" + url,
				Timeout: uint(conf.Runtime.Timeout.Max),
			}, args)
	if err != nil {
		if resp == nil {
			wdogErrors.WithLabelValues("NOCODE").Inc()
		} else {
			wdogErrors.WithLabelValues(resp.Status).Inc()
		}
		return nil, err
	}

	err = xhttp.RResp(resp, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (conn *podConn)Run(ctx context.Context, sopq *statsOpaque, suff, event string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	var res *swyapi.SwdFunctionRunResult
	var err error

	args.Event = event
	proxy := WdogProxyOK && suff == ""

	if proxy {
		res, err = talkHTTP(conn.Host, strconv.Itoa(conf.Wdog.Port),
				conn.Cookie + "/" + strings.Replace(conn.Addr, ".", "_", -1), args)
	}

	if !proxy || err != nil {
		url := conn.Cookie
		if suff != "" {
			url += "/" + suff
		}
		if sopq != nil && sopq.trace != nil {
			sopq.trace["wdog.req"] = time.Since(sopq.ts)
		}
		res, err = talkHTTP(conn.Addr, conn.Port, url, args)
		if sopq != nil && sopq.trace != nil {
			sopq.trace["wdog.resp"] = time.Since(sopq.ts)
		}
		if err != nil {
			goto out
		}
	}

	if res.Stdout != "" || res.Stderr != "" {
		go logSaveResult(ctx, conn.Cookie, event, res.Stdout, res.Stderr)
	}

	return res, nil

out:
	return nil, fmt.Errorf("RUN error %s", err.Error())
}

func doRun(ctx context.Context, fn *FunctionDesc, event string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	fmd, err := memdGetFn(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't %s memdat: %s", fn.Cookie, err.Error())
		return nil, err
	}

	conn, err := balancerGetConnAny(ctx, fmd)
	if conn == nil {
		ctxlog(ctx).Errorf("Can't find %s cookie balancer: %s", fn.Cookie, err.Error())
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	traceFnEvent(ctx, "run (" + event + ")", fn)

	sopq := statsStart()
	res, err := conn.Run(ctx, sopq, "", event, args)
	if err == nil {
		statsUpdate(fmd, sopq, res)
	}

	return res, err
}

func doRunBg(ctx context.Context, fn *FunctionDesc, event string, args *swyapi.SwdFunctionRun) {
	_, err := doRun(ctx, fn, event, args)
	if err != nil {
		ctxlog(ctx).Errorf("bg.%s: error running fn: %s", event, err.Error())
	}
}

func prepareTempRun(ctx context.Context, fn *FunctionDesc, params *swyapi.FunctionSources, w http.ResponseWriter) (string, *xrest.ReqErr) {
	td, err := tendatGet(ctx, gctx(ctx).Tenant)
	if err != nil {
		return "", GateErrD(err)
	}

	td.runlock.Lock()
	defer td.runlock.Unlock()

	if td.runrate == nil {
		td.runrate = xratelimit.MakeRL(0, uint(conf.RunRate))
	}

	if !td.runrate.Get() {
		http.Error(w, "Try-run is once per second", http.StatusTooManyRequests)
		return "", nil
	}

	suff, err := putTempSources(ctx, fn, params)
	if err != nil {
		return "", GateErrE(swyapi.GateGenErr, err)
	}

	err = tryBuildFunction(ctx, fn, suff)
	if err != nil {
		return "", GateErrM(swyapi.GateGenErr, "Error building function")
	}

	return suff, nil
}
