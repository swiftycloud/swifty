package main

import (
	"path/filepath"
	"os/exec"
	"../apis/apps"
)

type rt_info struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
	BuildIP		string
	Version		string
	VArgs		[]string
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function",
	VArgs:		[]string{"python3", "--version"},
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
	VArgs:		[]string{"go", "version"},
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/swift/swycode",
	Build:		true,
	VArgs:		[]string{"swift", "--version"},
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
	VArgs:		[]string{"node", "--version"},
}

var ruby_info = rt_info {
	Ext:		"rb",
	CodePath:	"/function",
	VArgs:		[]string{"ruby", "--version"},
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

func RtInit() {
	glog.Debugf("Will detect rt languages in the background")
	go func() {
		for l, h := range rt_handlers {
			args := append([]string{"run", "--rm", RtLangImage(l)}, h.VArgs...)
			out, err := exec.Command("docker", args...).Output()
			if err != nil {
				glog.Debugf("Cannot detect %s version", l)
				continue
			}

			h.Version = string(out)
		}
	}()
}

func RtLangImage(lng string) string {
	p := conf.Wdog.ImgPref
	if p == "" {
		p = "swifty"
	}
	return p + "/" + lng
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

func RtLangInfo(lh *rt_info) *swyapi.LangInfo {
	ret := &swyapi.LangInfo{
		Version:	lh.Version,
	}
	return ret
}
