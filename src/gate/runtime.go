package main

type rt_info struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function",
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
	Devel:		true,
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/function",
	Build:		true,
	Devel:		true,
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
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
	return rt_handlers[scr.Lang].Build
}

/* Path where the sources would appear in container */
func RtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func RtDefaultScriptName(scr *FnCodeDesc) string {
	return "script." + rt_handlers[scr.Lang].Ext
}
