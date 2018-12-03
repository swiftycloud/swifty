/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"go/token"
	"go/parser"
	"go/ast"
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
const goBodyFile = "body"
const goBody = "/go/src/swyrunner/" + goBodyFile + ".go"

func checkFileHasType(fname, typ string) bool {
	fset := token.NewFileSet()

	f, err := parser.ParseFile(fset, fname, nil, 0)
	if err != nil {
		return false
	}

	for _, d := range f.Decls {
		x, ok := d.(*ast.GenDecl)
		if ok && x.Tok == token.TYPE && len(x.Specs) > 0 {
			s, ok := x.Specs[0].(*ast.TypeSpec)
			if ok && s.Name != nil && s.Name.Name == typ {
				return true
			}
		}
	}

	return false
}

func doBuildGo(params *swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error) {
	os.Remove(goScript)
	os.Remove(goBody)

	srcdir := params.Sources
	err := os.Symlink("/go/src/swycode/" + srcdir + "/script" + params.Suff + ".go", goScript)
	if err != nil {
		return nil, fmt.Errorf("Can't symlink code: %s", err.Error())
	}

	if !checkFileHasType(goScript, "Body") {
		log.Debugf("No Body type found, doing synmlink")
		err := os.Symlink(goBodyFile, goBody)
		if err != nil {
			return nil, fmt.Errorf("Can't symlink body: %s", err.Error())
		}
	} else {
		log.Debugf("Body type found, using it")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Run go build on %s (+%s)", srcdir, params.Packages)
	cmd := exec.Command("go", "build", "-o", "../swycode/" + srcdir + "/runner" + params.Suff)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = "/go/src/swyrunner"
	if params.Packages != "" {
		cmd.Env = append(os.Environ(), "GOPATH=/go:" + params.Packages)
	}
	err = cmd.Run()
	os.Remove(goScript)
	os.Remove(goBody)

	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.WdogFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

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
