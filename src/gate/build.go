package main

import (
	"fmt"
	"context"
	"strconv"
	"../apis/apps"
	"../common/http"
)

func buildFunction(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	var wd_result swyapi.SwdFunctionRunResult

	b, addr := RtNeedToBuild(&fn.Code)
	if !b {
		return nil
	}

	ctxlog(ctx).Debugf("Building function in %s", fnRepoCheckout(conf, fn))

	resp, err := swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address: "http://" + addr + ":" + strconv.Itoa(conf.Wdog.Port) + "/v1/run",
				Timeout: 120,
			},
			&swyapi.SwdFunctionRun{
				PodToken:	fn.Code.Lang + "-build",
				Args:		map[string]string {
					"sources": fnCodePathV(fn),
				},
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
		logSaveResult(fn.Cookie, "build", wd_result.Stdout, wd_result.Stderr)
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