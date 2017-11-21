package main

type getRunCmd func(*FnScriptDesc) []string

type rt_info struct {
	WPath	string
	Build	[]string
	Run	getRunCmd
}

var py_info = rt_info {
	WPath: "/function",
	Run:	func(scr *FnScriptDesc) []string { return []string{"python", scr.Run} },
}

var golang_info = rt_info {
	WPath:	"/go/src/function",
	Build:	[]string{"go", "build"},
	Run:	func(*FnScriptDesc) []string { return []string{"function"} },
}

var swift_info = rt_info {
	WPath:	"/function",
	Build:	[]string{"swift", "build"},
	Run:	func(scr *FnScriptDesc) []string { return []string{"./.build/debug/" + scr.Run} },
}

var nodejs_info = rt_info {
	WPath:	"/function",
	Run:	func(scr *FnScriptDesc) []string { return []string{"node", scr.Run} },
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
}

func RtBuilding(lang string) bool {
	return RtBuildCmd(lang) != nil
}

func RtBuildCmd(lang string) []string {
	return rt_handlers[lang].Build
}

func RtGetWdogPath(fn *FunctionDesc) string {
	return rt_handlers[fn.Script.Lang].WPath
}

func RtRunCmd(scr *FnScriptDesc) []string {
	return rt_handlers[scr.Lang].Run(scr)
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
