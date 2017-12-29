package main

import (
	"net/http"
	"fmt"

	"../apis/apps"
	"../common"
	"../common/http"
)

func doRun(fn *FunctionDesc, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	link := dbBalancerLinkFindByCookie(fn.Cookie)
	if link == nil {
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	return doRunLink(link, nil, fn.Cookie, event, args)
}

func doRunLink(link *BalancerLink, fmd *FnMemData,
		cookie, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	if link.CntRS == 0 {
		return nil, fmt.Errorf("No available pods found")
	}

	return doRunIp(link.VIP(), fmd, cookie, event, args)
}

func doRunIp(VIP string, fmd *FnMemData, cookie, event string, args map[string]string) (*swyapi.SwdFunctionRunResult, error) {
	log.Debugf("RUN %s %s (%v)", cookie, event, args)

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
	log.Debugf("RETurn %s: %d out[%s] err[%s]", cookie,
			wd_result.Code, wd_result.Stdout, wd_result.Stderr)

	return &wd_result, nil

out:
	return nil, fmt.Errorf("RUN error %s", err.Error())
}

func buildFunction(fn *FunctionDesc) error {
	var err, er2 error
	var orig_state int
	var res *swyapi.SwdFunctionRunResult

	orig_state = fn.State
	log.Debugf("build RUN %s", fn.SwoId.Str())
	link := dbBalancerLinkFindByDepname(fn.InstBuild().DepName())
	if link == nil {
		err = fmt.Errorf("Can't find build balancer for %s", fn.SwoId.Str())
		goto out
	}

	res, err = doRunLink(link, nil, fn.Cookie, "build", map[string]string{})
	log.Debugf("build %s finished", fn.SwoId.Str())
	logSaveEvent(fn, "built", "")
	if err != nil {
		goto out
	}

	if res.Code != 0 {
		err = fmt.Errorf("Build finished with %d", res.Code)
		goto out
	}

	err = swk8sRemove(&conf, fn, fn.InstBuild())
	if err != nil {
		log.Errorf("remove deploy error: %s", err.Error())
		goto out
	}

	if orig_state == swy.DBFuncStateBld {
		err = dbFuncSetState(fn, swy.DBFuncStateStr)
		if err == nil {
			err = swk8sRun(&conf, fn, fn.Inst())
		}
	} else /* Upd */ {
		err = dbFuncSetState(fn, swy.DBFuncStateRdy)
		if err == nil {
			err = swk8sUpdate(&conf, fn)
		}
	}
	if err != nil {
		goto out_nok8s
	}

	return nil

out:
	er2 = swk8sRemove(&conf, fn, fn.InstBuild())
out_nok8s:
	if orig_state == swy.DBFuncStateBld || er2 != nil {
		log.Debugf("Setting stalled state")
		dbFuncSetState(fn, swy.DBFuncStateStl);
	} else /* Upd */ {
		// Keep fn ready with the original commit of
		// the repo checked out
		dbFuncSetState(fn, swy.DBFuncStateRdy)
	}
	return fmt.Errorf("buildFunction: %s", err.Error())
}

func runFunctionOnce(fn *FunctionDesc) {
	log.Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(fn, "oneshot", map[string]string{})
	log.Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(&conf, fn, fn.Inst())
	dbFuncSetState(fn, swy.DBFuncStateStl);
}
