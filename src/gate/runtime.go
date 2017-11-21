package main

import (
	"os"
	"fmt"
	"io/ioutil"
)

type getRunCmd func(*FnCodeDesc) []string
type prepSources func(*FnCodeDesc, string) error
type getPath func(*FnCodeDesc, bool) string

type rt_info struct {
	Wdir		getPath
	CodePath	string
	Ext		string
	Build		[]string
	Run		getRunCmd
	Prep		prepSources
}

func pyPrepSources(scr *FnCodeDesc, dir string) error {
	if scr.Function == "" {
		return nil
	}

	/* For scripts with sources need to prepare them for import */
	nf, err := os.Create(dir + "/__init__.py")
	if err != nil {
		return err
	}

	scr.Script = "main.py"
	nf.Close()
	return nil
}

func pyWdir(scr *FnCodeDesc, build bool) string {
	if scr.Function == "" {
		return "/function/code"
	} else {
		return "/function"
	}
}

func pyRun(scr *FnCodeDesc) []string {
	if scr.Function == "" {
		return []string{"python", scr.Script}
	} else {
		return []string{"python", "main.py", scr.Function}
	}
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function/code",
	Run:		pyRun,
	Prep:		pyPrepSources,
	Wdir:		pyWdir,
}

func goPrepSources(scr *FnCodeDesc, dir string) error {
	if scr.Function == "" {
		return nil
	}

	code := fmt.Sprintf("package swyfn\nfunc Function(args map[string]string) { %s(args) }\n", scr.Function)
	return ioutil.WriteFile(dir + "/swymain.go", []byte(code), 0444)
}

func goRun(scr *FnCodeDesc) []string {
	return []string{"swycode"}
}

func goWdir(scr *FnCodeDesc, build bool) string {
	if scr.Function == "" || !build {
		return "/go/src/swycode"
	} else {
		return "/go/src/main"
	}
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		[]string{"go", "build", "-o", "../swycode/swycode"},
	Run:		goRun,
	Prep:		goPrepSources,
	Wdir:		goWdir,
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/function",
	Build:		[]string{"swift", "build"},
	Run:		func(scr *FnCodeDesc) []string { return []string{"./.build/debug/" + scr.Script} },
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
	Run:		func(scr *FnCodeDesc) []string { return []string{"node", scr.Script} },
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
}

func RtBuilding(scr *FnCodeDesc) bool {
	return RtBuildCmd(scr) != nil
}

func RtBuildCmd(scr *FnCodeDesc) []string {
	return rt_handlers[scr.Lang].Build
}

/* Path where the watchdog will cd to */
func RtWdir(scr *FnCodeDesc, build bool) string {
	h := rt_handlers[scr.Lang]
	if h.Wdir == nil {
		return h.CodePath
	} else {
		return h.Wdir(scr, build)
	}
}

/* Path where the sources would appear in container */
func RtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func RtRunCmd(scr *FnCodeDesc) []string {
	return rt_handlers[scr.Lang].Run(scr)
}

func RtDefaultScriptName(scr *FnCodeDesc) string {
	return "script." + rt_handlers[scr.Lang].Ext
}

func RtPrepareSources(scr *FnCodeDesc, dir string) error {
	var err error
	fn := rt_handlers[scr.Lang].Prep
	if fn != nil {
		err = fn(scr, dir)
	}
	return err
}

func RtGetFnResources(fn *FunctionDesc) map[string]string {
	ret := make(map[string]string)
	ret["cpu.max"] = "1"
	ret["cpu.min"] = "500m"
	if fn.Size.Mem == "" {
		ret["mem.max"] = "128Mi"
		ret["mem.min"] = "64Mi"
	} else {
		ret["mem.max"] = fn.Size.Mem
		ret["mem.min"] = fn.Size.Mem
	}
	return ret
}
