package main

type getRunCmd func(*FnCodeDesc) []string

type rt_info struct {
	WPath	string
	Build	[]string
	Run	getRunCmd
}

var py_info = rt_info {
	WPath: "/function",
	Run:	func(scr *FnCodeDesc) []string { return []string{"python", scr.Run} },
}

var golang_info = rt_info {
	WPath:	"/go/src/function",
	Build:	[]string{"go", "build"},
	Run:	func(*FnCodeDesc) []string { return []string{"function"} },
}

var swift_info = rt_info {
	WPath:	"/function",
	Build:	[]string{"swift", "build"},
	Run:	func(scr *FnCodeDesc) []string { return []string{"./.build/debug/" + scr.Run} },
}

var nodejs_info = rt_info {
	WPath:	"/function",
	Run:	func(scr *FnCodeDesc) []string { return []string{"node", scr.Run} },
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
