package main

type getRunCmd func(*FunctionDesc) []string

type rt_info struct {
	WPath	string
	Build	string
	Run	getRunCmd
}

var py_info = rt_info {
	WPath: "/function",
	Run:	func(fn *FunctionDesc) []string { return []string{"python", fn.Script.Run} },
}

var golang_info = rt_info {
	WPath:	"/go/src/function",
	Build:	"go build",
	Run:	func(*FunctionDesc) []string { return []string{"function"} },
}

var swift_info = rt_info {
	WPath:	"/function",
	Build:	"swift build",
	Run:	func(fn *FunctionDesc) []string { return []string{"./.build/debug/" + fn.Script.Run} },
}

var nodejs_info = rt_info {
	WPath:	"/function",
	Run:	func(fn *FunctionDesc) []string { return []string{"node", fn.Script.Run} },
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
}

func RtBuilding(lang string) bool {
	return len(RtBuildCmd(lang)) > 0
}

func RtBuildCmd(lang string) string {
	return rt_handlers[lang].Build
}

func RtGetWdogPath(fn *FunctionDesc) string {
	return rt_handlers[fn.Script.Lang].WPath
}

func RtRunCmd(fn *FunctionDesc) []string {
	return rt_handlers[fn.Script.Lang].Run(fn)
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
