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
	conn, err := dbBalancerGetConnByCookie(fn.Cookie)
	if conn == nil {
		ctxlog(ctx).Errorf("Can't find %s cookie balancer: %s", fn.Cookie, err.Error())
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	return doRunConn(ctx, conn, nil, fn.Cookie, event, args)
}

func doRunConn(ctx context.Context, conn *BalancerConn, fmd *FnMemData,
		cookie, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	return doRunIp(ctx, conn.VIP(), fmd, cookie, event, args)
}

func doRunIp(ctx context.Context, VIP string, fmd *FnMemData, cookie, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	ctxlog(ctx).Debugf("RUN %s %s (%v)", cookie, event, args)

	var wd_result swyapi.SwdFunctionRunResult
	var resp *http.Response
	var err error
	var sopq *statsOpaque

	sopq = statsStart()

	resp, err = swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + VIP + "/v1/run",
				Timeout: uint(conf.Runtime.Timeout.Max),
			},
			&swyapi.SwdFunctionRun{
				PodToken:	cookie,
				Args:		args,
			})
	if err != nil {
		goto out
	}

	err = swyhttp.ReadAndUnmarshalResp(resp, &wd_result)
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
	ctxlog(ctx).Debugf("RETurn %s: %d out[%s] err[%s]", cookie,
			wd_result.Code, wd_result.Stdout, wd_result.Stderr)

	return &wd_result, nil

out:
	return nil, fmt.Errorf("RUN error %s", err.Error())
}

func buildFunction(ctx context.Context, fn *FunctionDesc) error {
	var err, er2 error
	var orig_state int
	var res *swyapi.SwdFunctionRunResult

	orig_state = fn.State
	ctxlog(ctx).Debugf("build RUN %s", fn.SwoId.Str())
	conn, err := dbBalancerGetConnByDep(fn.InstBuild().DepName())
	if err != nil {
		ctxlog(ctx).Errorf("Can't find build balancer: %s", err.Error())
		err = fmt.Errorf("Can't find build balancer for %s", fn.SwoId.Str())
		goto out
	}

	res, err = doRunConn(ctx, conn, nil, fn.Cookie, "build", map[string]string{})
	ctxlog(ctx).Debugf("build %s finished", fn.SwoId.Str())
	logSaveEvent(fn, "built", "")
	if err != nil {
		goto out
	}

	if res.Code != 0 {
		err = fmt.Errorf("Build finished with %d", res.Code)
		goto out
	}

	err = swk8sRemove(ctx, &conf, fn, fn.InstBuild())
	if err != nil {
		ctxlog(ctx).Errorf("remove deploy error: %s", err.Error())
		goto out
	}

	if orig_state == swy.DBFuncStateBld {
		err = dbFuncSetState(ctx, fn, swy.DBFuncStateStr)
		if err == nil {
			err = swk8sRun(ctx, &conf, fn, fn.Inst())
		}
	} else /* Upd */ {
		err = dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
		if err == nil {
			err = swk8sUpdate(ctx, &conf, fn)
		}
	}
	if err != nil {
		goto out_nok8s
	}

	return nil

out:
	er2 = swk8sRemove(ctx, &conf, fn, fn.InstBuild())
out_nok8s:
	if orig_state == swy.DBFuncStateBld || er2 != nil {
		ctxlog(ctx).Debugf("Setting stalled state")
		dbFuncSetState(ctx, fn, swy.DBFuncStateStl);
	} else /* Upd */ {
		// Keep fn ready with the original commit of
		// the repo checked out
		dbFuncSetState(ctx, fn, swy.DBFuncStateRdy)
	}
	return fmt.Errorf("buildFunction: %s", err.Error())
}

func runFunctionOnce(ctx context.Context, fn *FunctionDesc) {
	ctxlog(ctx).Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(ctx, fn, "oneshot", map[string]string{})
	ctxlog(ctx).Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(ctx, &conf, fn, fn.Inst())
	dbFuncSetState(ctx, fn, swy.DBFuncStateStl);
}
