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

	pfx := "/swift/swycode/" + srcdir
	err = os.Rename(pfx + "/debug/function", pfx + "/runner" + params.Suff)
	if err != nil {
		return nil, fmt.Errorf("Can't rename binary: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

const monoRunner = "/mono/runner/runner.cs"
const monoXStream = "/mono/runner/XStream.dll"

func doBuildMono(params *swyapi.WdogFunctionBuild) (*swyapi.WdogFunctionRunResult, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	srcdir := "/mono/functions/" + params.Sources
	log.Debugf("Run mono build on %s", srcdir)
	fnScript := srcdir + "/script" + params.Suff + ".cs"
	fnBinary := srcdir + "/runner" + params.Suff + ".exe"
	cmd := exec.Command("csc", monoRunner, fnScript, "-m:FR", "-r:" + monoXStream, "-out:" + fnBinary)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if exit, code := get_exit_code(err); exit {
			return &swyapi.WdogFunctionRunResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
		}

		return nil, fmt.Errorf("Can't build: %s", err.Error())
	}

	return &swyapi.WdogFunctionRunResult{Code: 0, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
