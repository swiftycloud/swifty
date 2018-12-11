/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

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
	ctxlog(ctx).Debugf("Building %s (%s)", fn.SwoId.Str(), suf)

	breq := &swyapi.WdogFunctionBuild {
		Sources:	fn.srcDir(""),
		Suff:		suf,
	}

	if rh.BuildPkgPath != nil {
		breq.Packages = rh.BuildPkgPath(fn.SwoId)
	}

	gateBuilds.WithLabelValues(fn.Code.Lang, "start").Inc()
	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Address: rtService(rh, "build"),
				Timeout: 120,
			}, breq)
	if err != nil {
		gateBuilds.WithLabelValues(fn.Code.Lang, "error").Inc()
		ctxlog(ctx).Errorf("Error building function: %s", err.Error())
		return fmt.Errorf("Can't build function")
	}

	err = xhttp.RResp(resp, &wd_result)
	if err != nil {
		gateBuilds.WithLabelValues(fn.Code.Lang, "error2").Inc()
		ctxlog(ctx).Errorf("Can't get build result back: %s", err.Error())
		return fmt.Errorf("Error building function")
	}

	if wd_result.Code != 0 {
		gateBuilds.WithLabelValues(fn.Code.Lang, "fail").Inc()
		logSaveResult(ctx, fn.Cookie, "build", wd_result.Stdout, wd_result.Stderr)
		return fmt.Errorf("Error building function")
	}

	return nil
}
