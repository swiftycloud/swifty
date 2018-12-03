/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"os/exec"
	"bytes"
	"fmt"
	"os"

	"swifty/apis"
)

const lBuilder = "/usr/bin/build_runner.sh"

func doBuildCommon(params *swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run build on %s", params.Sources)
	cmd := exec.Command(lBuilder)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	penv := []string {
		"SWD_SOURCES=" + params.Sources,
		"SWD_SUFFIX=" + params.Suff,
		"SWD_PACKAGES=" + params.Packages,
	}
	cmd.Env = append(os.Environ(), penv...)
	err := cmd.Run()
	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.WdogFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
