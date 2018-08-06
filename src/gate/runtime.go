package main

import (
	"path/filepath"
)

type rt_info struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
	BuildIP		string
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function",
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/swift/swycode",
	Build:		true,
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
}

var ruby_info = rt_info {
	Ext:		"rb",
	CodePath:	"/function",
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
	"ruby":		&ruby_info,
}
var extmap map[string]string

func init() {
	extmap = make(map[string]string)
	for l, d := range rt_handlers {
		extmap["." + d.Ext] = l
	}
}

func RtLangDetect(fname string) string {
	e := filepath.Ext(fname)
	return extmap[e]
}

func RtLangEnabled(lang string) bool {
	h, ok := rt_handlers[lang]
	return ok && (SwyModeDevel || !h.Devel)
}

func RtNeedToBuild(scr *FnCodeDesc) (bool, string) {
	rh := rt_handlers[scr.Lang]
	return rh.Build, rh.BuildIP
}

func RtSetBuilder(lang, ip string) {
	rt_handlers[lang].BuildIP = ip
}

/* Path where the sources would appear in container */
func RtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func RtDefaultScriptName(scr *FnCodeDesc) string {
	return "script." + rt_handlers[scr.Lang].Ext
}
