package main

import (
	"net/http"
	"fmt"
	"context"
	"strconv"
	"strings"

	"../apis"
	"../common"
	"../common/http"
)

type podConn struct {
	Addr	string
	Host	string
	Port	string
}

func doRun(ctx context.Context, fn *FunctionDesc, event string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	fmd, err := memdGetFn(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't %s memdat: %s", fn.Cookie, err.Error())
		return nil, err
	}

	conn, err := balancerGetConnAny(ctx, fn.Cookie, fmd)
	if conn == nil {
		ctxlog(ctx).Errorf("Can't find %s cookie balancer: %s", fn.Cookie, err.Error())
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	sopq := statsStart()
	res, err := doRunConn(ctx, conn, fn.Cookie, event, args)
	if err == nil {
		statsUpdate(fmd, sopq, res)
	}

	return res, err
}

func talkHTTP(addr, port, url string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	var resp *http.Response
	var res swyapi.SwdFunctionRunResult
	var err error

	resp, err = swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
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

	err = swyhttp.ReadAndUnmarshalResp(resp, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func doRunConn(ctx context.Context, conn *podConn, cookie, event string, args *swyapi.SwdFunctionRun) (*swyapi.SwdFunctionRunResult, error) {
	var res *swyapi.SwdFunctionRunResult
	var err error

	args.Event = event

	if SwdProxyOK {
		res, err = talkHTTP(conn.Host, strconv.Itoa(conf.Wdog.Port),
				cookie + "/" + strings.Replace(conn.Addr, ".", "_", -1), args)
	}

	if !SwdProxyOK || err != nil {
		res, err = talkHTTP(conn.Addr, conn.Port, cookie, args)
		if err != nil {
			goto out
		}
	}

	if res.Stdout != "" || res.Stderr != "" {
		logSaveResult(ctx, cookie, event, res.Stdout, res.Stderr)
	}

	return res, nil

out:
	return nil, fmt.Errorf("RUN error %s", err.Error())
}

func runFunctionOnce(ctx context.Context, fn *FunctionDesc) {
	ctxlog(ctx).Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(ctx, fn, "oneshot", &swyapi.SwdFunctionRun{})
	ctxlog(ctx).Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(ctx, &conf, fn)
	fn.ToState(ctx, swy.DBFuncStateStl, -1)
}
