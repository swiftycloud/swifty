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

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
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
 func RtGetFnResources(fn *FunctionDesc) []string {
	 /* XXX Get from FN */
	 return []string {
		 "mem.lim": "16Mi",
		 "mem.req": "8Mi",
		 "cpu.lim": "1",
		 "cpu.lim": "1",
	 }
 }
