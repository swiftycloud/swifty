package main

import (
	"os/exec"
	"bytes"
	"fmt"
	"os"

	"swifty/apis"
)

/*
 * All functions sit at /go/src/swycode/
 * Runner sits at /go/src/swyrunner/
 */
const goScript = "/go/src/swyrunner/script.go"

func doBuildGo(params *swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error) {
	os.Remove(goScript)
	srcdir := params.Sources
	err := os.Symlink("/go/src/swycode/" + srcdir + "/script" + params.Suff + ".go", goScript)
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run go build on %s", srcdir)
	cmd := exec.Command("go", "build", "-o", "../swycode/" + srcdir + "/runner" + params.Suff)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = "/go/src/swyrunner"
	err = cmd.Run()
	os.Remove(goScript)

	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.WdogFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

/*
 * All functions sit at /swift/swycode/
 * Runner sits at /swift/runner/
 */
const swiftScript = "/swift/runner/Sources/script.swift"

func doBuildSwift(params *swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error) {
	os.Remove(swiftScript)
	srcdir := params.Sources
	err := os.Symlink("/swift/swycode/" + srcdir + "/script" + params.Suff + ".swift", swiftScript)
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run swift build on %s", srcdir)
	cmd := exec.Command("swift", "build", "--build-path", "../swycode/" + srcdir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = "/swift/runner"
	err = cmd.Run()
	os.Remove(swiftScript)
	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.WdogFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	err = os.Rename("/swift/swycode/debug/function", "/swift/swycode/debug/runner" + params.Suff)
	if err != nil {
		return nil, fmt.Errorf("Can't rename binary: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

