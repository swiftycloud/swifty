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

func doRun(fi *FnInst, event string, args []string) (int, string, string, error) {
	log.Debugf("RUN %s(%s)", fi.fn.SwoId.Str(), strings.Join(args, ","))

	var wd_result swyapi.SwdFunctionRunResult
	var resp *http.Response
	var link *BalancerLink
	var resp_body []byte
	var err error

	link = dbBalancerLinkFind(fi.DepName())
	if link == nil {
		err = fmt.Errorf("Can't find balancer link %s", fi.DepName())
		goto out
	}

	if link.NumRS == 0 {
		err = fmt.Errorf("No available pods found")
		goto out
	}

	resp, err = swy.HTTPMarshalAndPostTimeout("http://" + link.VIP() + "/v1/function/run",
				120,
				&swyapi.SwdFunctionRun{
					PodToken:	fi.fn.Cookie,
					Args:		args,
				}, nil)
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

	logSaveResult(fi.fn, event, wd_result.Stdout, wd_result.Stderr)
	log.Debugf("RETurn %s: %d out[%s] err[%s]", fi.fn.SwoId.Str(),
			wd_result.Code, wd_result.Stdout, wd_result.Stderr)
	return wd_result.Code, wd_result.Stdout, wd_result.Stderr, nil

out:
	return 0, "", "", fmt.Errorf("RUN error %s", err.Error())
}

func buildFunction(fn *FunctionDesc) error {
	var err error
	var orig_state int

	build_cmd := strings.Split(RtBuildCmd(fn.Script.Lang), " ")
	log.Debugf("build RUN %s args %v", fn.SwoId.Str(), build_cmd[1:])
	code, _, stderr, err := doRun(fn.InstBuild(), "build", build_cmd[1:])
	log.Debugf("build %s finished", fn.SwoId.Str())
	logSaveEvent(fn, "built", "")
	if err != nil {
		goto out
	}

	if code != 0 || stderr != "" {
		err = fmt.Errorf("stderr: %s", stderr)
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