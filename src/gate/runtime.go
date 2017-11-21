package main

type getRunCmd func(*FnCodeDesc) []string

type rt_info struct {
	WPath	string
	Ext	string
	Build	[]string
	Run	getRunCmd
}

var py_info = rt_info {
	WPath: "/function",
	Ext:	"py",
	Run:	func(scr *FnCodeDesc) []string { return []string{"python", scr.Script} },
}

var golang_info = rt_info {
	WPath:	"/go/src/function",
	Ext:	"go",
	Build:	[]string{"go", "build"},
	Run:	func(*FnCodeDesc) []string { return []string{"function"} },
}

var swift_info = rt_info {
	WPath:	"/function",
	Ext:	"swift",
	Build:	[]string{"swift", "build"},
	Run:	func(scr *FnCodeDesc) []string { return []string{"./.build/debug/" + scr.Script} },
}

var nodejs_info = rt_info {
	WPath:	"/function",
	Ext:	"js",
	Run:	func(scr *FnCodeDesc) []string { return []string{"node", scr.Script} },
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

func RtGetWdogPath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].WPath
}

func RtRunCmd(scr *FnCodeDesc) []string {
	return rt_handlers[scr.Lang].Run(scr)
}

func RtDefaultScriptName(scr *FnCodeDesc) string {
	return "script." + rt_handlers[scr.Lang].Ext
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
