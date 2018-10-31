package main

import (
	"fmt"
	"context"
	"swifty/apis"
	"swifty/common/http"
)

func tryBuildFunction(ctx context.Context, fn *FunctionDesc, suf string) error {
	b, rh := rtNeedToBuild(&fn.Code)
	if !b {
		return nil
	}

	return buildFunction(ctx, rh, fn, suf)
}

func buildFunction(ctx context.Context, rh *langInfo, fn *FunctionDesc, suf string) error {
	var wd_result swyapi.WdogFunctionRunResult

	traceFnEvent(ctx, "build", fn)

	breq := &swyapi.WdogFunctionBuild {
		Sources:	fn.srcDir(""),
		Suff:		suf,
	}

	if rh.BuildPkgPath != nil {
		breq.Packages = rh.BuildPkgPath(fn.SwoId)
	}

	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Address: rtService(rh, "build"),
				Timeout: 120,
			}, breq)
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
