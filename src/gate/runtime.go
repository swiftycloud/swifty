package main

import (
	"os"
	"io/ioutil"
)

type prepSources func(*FnCodeDesc, string) error

type rt_info struct {
	Wdir		string
	CodePath	string
	Ext		string
	Build		[]string
	Run		[]string
	Prep		prepSources
	Devel		bool
}

func pyPrepSources(scr *FnCodeDesc, dir string) error {
	/* For scripts with sources need to prepare them for import */
	nf, err := os.Create(dir + "/__init__.py")
	if err != nil {
		return err
	}

	nf.Close()
	return nil
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function/code",
	Wdir:		"/function",
	Run:		[]string{"python", "main.py"},
	Prep:		pyPrepSources,
}

func goPrepSources(scr *FnCodeDesc, dir string) error {
	code := "package swyfn\nfunc SwyMain(args map[string]string) { main(args) }\n"
	return ioutil.WriteFile(dir + "/swymain.go", []byte(code), 0444)
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Wdir:		"/go/src/main",
	Build:		[]string{"go", "build", "-o", "../swycode/swycode"},
	Run:		[]string{"swycode"},
	Prep:		goPrepSources,
	Devel:		true,
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/function",
	Build:		[]string{"swift", "build"},
	Run:		[]string{"./.build/debug/"},
	Devel:		true,
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
	Run:		[]string{"node", "main.js"},
	Devel:		true,
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
}

func RtLangEnabled(lang string) bool {
	h, ok := rt_handlers[lang]
	return ok && (SwyModeDevel || !h.Devel)
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
	if h.Wdir == "" {
		return h.CodePath
	} else {
		return h.Wdir
	}
}

/* Path where the sources would appear in container */
func RtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func RtRunCmd(scr *FnCodeDesc) []string {
	return rt_handlers[scr.Lang].Run
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
