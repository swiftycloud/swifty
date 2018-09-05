package main

import (
	"fmt"
	"context"
	"strconv"
	"../apis"
	"../common/http"
)

func tryBuildFunction(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, suf string) error {
	b, addr := RtNeedToBuild(&fn.Code)
	if !b {
		return nil
	}

	return buildFunction(ctx, conf, addr, fn, suf)
}

func buildFunction(ctx context.Context, conf *YAMLConf, addr string, fn *FunctionDesc, suf string) error {
	var wd_result swyapi.SwdFunctionRunResult

	ctxlog(ctx).Debugf("Building function in %s", fn.srcPath(""))

	resp, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + ":" + strconv.Itoa(conf.Wdog.Port) + "/v1/run",
				Timeout: 120,
			},
			&swyapi.SwdFunctionBuild{
				Sources: fn.srcDir(""),
				Suff: suf,
			})
	if err != nil {
		ctxlog(ctx).Errorf("Error building function: %s", err.Error())
		return fmt.Errorf("Can't build function")
	}

	err = swyhttp.ReadAndUnmarshalResp(resp, &wd_result)
	if err != nil {
		ctxlog(ctx).Errorf("Can't get build result back: %s", err.Error())
		return fmt.Errorf("Error building function")
	}

	if wd_result.Code != 0 {
		logSaveResult(ctx, fn.Cookie, "build", wd_result.Stdout, wd_result.Stderr)
		return fmt.Errorf("Error building function")
	}

	ctxlog(ctx).Debugf("Function built OK")
	return nil
}

func BuilderInit(conf *YAMLConf) error {
	buildIps, err := swk8sGetBuildPods()
	if err != nil {
		return err
	}

	for l, rt := range(rt_handlers) {
		if !rt.Build {
			continue
		}

		if !rt.Devel || SwyModeDevel {
			ip, ok := buildIps[l]
			if !ok {
				return fmt.Errorf("No builder for %s", l)
			}

			glog.Debugf("Set %s as builder for %s", ip, l)
			rt.BuildIP = ip
		}
	}

	return nil
}
