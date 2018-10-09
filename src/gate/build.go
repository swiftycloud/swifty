package main

import (
	"fmt"
	"context"
	"strconv"
	"swifty/apis"
	"swifty/common/http"
)

func tryBuildFunction(ctx context.Context, fn *FunctionDesc, suf string) error {
	b, addr := rtNeedToBuild(&fn.Code)
	if !b {
		return nil
	}

	return buildFunction(ctx, addr, fn, suf)
}

func buildFunction(ctx context.Context, addr string, fn *FunctionDesc, suf string) error {
	var wd_result swyapi.WdogFunctionRunResult

	traceFnEvent(ctx, "build", fn)

	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Address: "http://" + addr + ":" + strconv.Itoa(conf.Wdog.Port) + "/v1/run",
				Timeout: 120,
			},
			&swyapi.WdogFunctionBuild{
				Sources: fn.srcDir(""),
				Suff: suf,
			})
	if err != nil {
		ctxlog(ctx).Errorf("Error building function: %s", err.Error())
		return fmt.Errorf("Can't build function")
	}

	err = xhttp.RResp(resp, &wd_result)
	if err != nil {
		ctxlog(ctx).Errorf("Can't get build result back: %s", err.Error())
		return fmt.Errorf("Error building function")
	}

	if wd_result.Code != 0 {
		logSaveResult(ctx, fn.Cookie, "build", wd_result.Stdout, wd_result.Stderr)
		return fmt.Errorf("Error building function")
	}

	return nil
}

func BuilderInit(ctx context.Context) error {
	buildIps, err := k8sGetBuildPods(ctx)
	if err != nil {
		return err
	}

	for l, rt := range(rt_handlers) {
		if !rt.Build {
			continue
		}

		if !rt.Devel || ModeDevel {
			ip, ok := buildIps[l]
			if !ok {
				return fmt.Errorf("No builder for %s", l)
			}

			ctxlog(ctx).Debugf("Set %s as builder for %s", ip, l)
			rt.BuildIP = ip
		}
	}

	return nil
}
