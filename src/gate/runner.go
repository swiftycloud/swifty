package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"errors"
	"fmt"

	"../apis/apps"
	"../common"
)

func doRun(cookie, event string, args []string) (int, string, error) {
	link := dbBalancerLinkFindByCookie(cookie)
	if link == nil {
		return -1, "", fmt.Errorf("Can't find balancer for %s", cookie)
	}

	return talkToLink(link, cookie, event, args)
}

func talkToLink(link *BalancerLink, cookie, event string, args []string) (int, string, error) {
	log.Debugf("RUN %s(%s)", cookie, strings.Join(args, ","))

	var wd_result swyapi.SwdFunctionRunResult
	var resp *http.Response
	var resp_body []byte
	var err error

	if link.CntRS == 0 {
		err = fmt.Errorf("No available pods found")
		goto out
	}

	resp, err = swy.HTTPMarshalAndPost(
			&swy.RestReq{
				Address: "http://" + link.VIP() + "/v1/run",
				Timeout: 120,
			},
			&swyapi.SwdFunctionRun{
				PodToken:	cookie,
				Args:		args,
			})
	if err != nil {
		goto out
	}

	resp_body, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		err = errors.New("Can't read reply")
		goto out
	}

	err = json.Unmarshal(resp_body, &wd_result)
	if err != nil {
		err = fmt.Errorf("Unmarshal error %s", err.Error())
		goto out
	}

	logSaveResult(cookie, event, wd_result.Stdout, wd_result.Stderr)
	log.Debugf("RETurn %s: %d out[%s] err[%s]", cookie,
			wd_result.Code, wd_result.Stdout, wd_result.Stderr)
	return wd_result.Code, wd_result.Return, nil

out:
	return -1, "", fmt.Errorf("RUN error %s", err.Error())
}

func buildFunction(fn *FunctionDesc) error {
	var err error
	var code, orig_state int

	log.Debugf("build RUN %s", fn.SwoId.Str())
	link := dbBalancerLinkFindByDepname(fn.InstBuild().DepName())
	if link == nil {
		err = fmt.Errorf("Can't find build balancer for %s", fn.SwoId.Str())
		goto out
	}

	code, _, err = talkToLink(link, fn.Cookie, "build", []string{})
	log.Debugf("build %s finished", fn.SwoId.Str())
	logSaveEvent(fn, "built", "")
	if err != nil {
		goto out
	}

	if code != 0 {
		err = fmt.Errorf("Build finished with %d", code)
		goto out
	}

	err = swk8sRemove(&conf, fn, fn.InstBuild())
	if err != nil {
		log.Errorf("remove deploy error: %s", err.Error())
		goto out
	}

	orig_state = fn.State
	if orig_state == swy.DBFuncStateBld {
		err = dbFuncSetState(fn, swy.DBFuncStateBlt)
		if err == nil {
			err = swk8sRun(&conf, fn, fn.Inst())
		}
	} else {
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
	swk8sRemove(&conf, fn, fn.InstBuild())
out_nok8s:
	if orig_state == swy.DBFuncStateBld {
		dbFuncSetState(fn, swy.DBFuncStateStl);
	} else {
		// Keep fn ready with the original commit of
		// the repo checked out
		dbFuncSetState(fn, swy.DBFuncStateRdy)
	}
	return fmt.Errorf("buildFunction: %s", err.Error())
}

func runFunctionOnce(fn *FunctionDesc) {
	log.Debugf("oneshot RUN for %s", fn.SwoId.Str())
	doRun(fn.Cookie, "oneshot", RtRunCmd(&fn.Code))
	log.Debugf("oneshor %s finished", fn.SwoId.Str())

	swk8sRemove(&conf, fn, fn.Inst())
	dbFuncSetState(fn, swy.DBFuncStateStl);
}
