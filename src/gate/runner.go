package main

import (
	"net/http"
	"fmt"
	"context"

	"../apis/apps"
	"../common"
	"../common/http"
)

func doRun(ctx context.Context, fn *FunctionDesc, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	conn, err := balancerGetConnAny(ctx, fn.Cookie, nil)
	if conn == "" {
		ctxlog(ctx).Errorf("Can't find %s cookie balancer: %s", fn.Cookie, err.Error())
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	return doRunConn(ctx, conn, nil, fn.Cookie, event, args)
}

func talkHTTP(conn, cookie string, args map[string]string, res *swyapi.SwdFunctionRunResult) error {
	var resp *http.Response
	var err error

	resp, err = swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + conn + "/v1/run/" + cookie,
				Timeout: uint(conf.Runtime.Timeout.Max),
			}, args)
	if err != nil {
		return err
	}

	err = swyhttp.ReadAndUnmarshalResp(resp, res)
	if err != nil {
		return err
	}

	return nil
}

func doRunConn(ctx context.Context, conn string, fmd *FnMemData, cookie, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	if event != "call" {
		ctxlog(ctx).Debugf("RUN %s %s (%v)", cookie, event, args)
	}

	var wd_result swyapi.SwdFunctionRunResult
	var err error

	sopq := statsStart()

	err = talkHTTP(conn, cookie, args, &wd_result)
	if err != nil {
		goto out
	}

	if fmd == nil {
		fmd = memdGet(cookie)
	}

	statsUpdate(fmd, sopq, &wd_result)

	if wd_result.Stdout != "" || wd_result.Stderr != "" {
		logSaveResult(cookie, event, wd_result.Stdout, wd_result.Stderr)
	}

	if event != "call" {
		ctxlog(ctx).Debugf("RETurn %s: %d out[%s] err[%s]", cookie,
			wd_result.Code, wd_result.Stdout, wd_result.Stderr)
	}

	return &wd_result, nil

out:
	return nil, fmt.Errorf("RUN error %s", err.Error())
}

func runFunctionOnce(ctx context.Context, fn *FunctionDesc) {
	ctxlog(ctx).Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(ctx, fn, "oneshot", map[string]string{})
	ctxlog(ctx).Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(ctx, &conf, fn)
	dbFuncSetState(ctx, fn, swy.DBFuncStateStl);
}
