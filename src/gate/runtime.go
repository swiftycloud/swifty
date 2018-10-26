package main

import (
	"path/filepath"
	"os/exec"
	"strings"
	"swifty/apis"
	"context"
)

type langInfo struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
	BuildIP		string
	Version		string
	VArgs		[]string
	Packages	[]string
	PList		func() []string

	Install		func(context.Context, SwoId) error
	Remove		func(context.Context, SwoId) error
	List		func(context.Context, string) ([]string, error)

	BuildPkgPath	func(SwoId) string
	RunPkgPath	func(SwoId) (string, string)
}

func GetLines(lng string, args ...string) []string {
	cmd := append([]string{"run", "--rm", rtLangImage(lng)}, args...)
	out, err := exec.Command("docker", cmd...).Output()
	if err != nil {
		return []string{}
	}

	sout := strings.TrimSpace(string(out))
	return strings.Split(sout, "\n")
}

var rt_handlers = map[string]*langInfo {
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
			args := append([]string{"run", "--rm", rtLangImage(l)}, h.VArgs...)
			out, err := exec.Command("docker", args...).Output()
			if err != nil {
				glog.Debugf("Cannot detect %s version", l)
				continue
			}

			h.Version = string(out)
		}
	}()
	go func() {
		for _, h := range rt_handlers {
			if h.PList == nil {
				continue
			}

			h.Packages = h.PList()
		}
	}()
}

func rtLangImage(lng string) string {
	return conf.Wdog.ImgPref + "/" + lng
}

func rtLangDetect(fname string) string {
	e := filepath.Ext(fname)
	return extmap[e]
}

func rtLangEnabled(lang string) bool {
	h, ok := rt_handlers[lang]
	return ok && (ModeDevel || !h.Devel)
}

func rtNeedToBuild(scr *FnCodeDesc) (bool, *langInfo) {
	rh := rt_handlers[scr.Lang]
	return rh.Build, rh
}

func rtSetBuilder(lang, ip string) {
	rt_handlers[lang].BuildIP = ip
}

/* Path where the sources would appear in container */
func rtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func rtScriptName(scr *FnCodeDesc, suff string) string {
	/* This should be in sync with wdog's startQnR and builders */
	return "script" + suff + "." + rt_handlers[scr.Lang].Ext
}

func rtPackages(id SwoId, lang string)  (string, string, bool) {
	h := rt_handlers[lang]
	if h.RunPkgPath != nil {
		h, m := h.RunPkgPath(id)
		return h, m, true
	} else {
		return "", "", false
	}
}

func (lh *langInfo)info() *swyapi.LangInfo {
	return &swyapi.LangInfo{
		Version:	lh.Version,
		Packages:	lh.Packages,
	}
}

func packagesDir() string {
	return conf.Wdog.Volume + "/" + PackagesSubdir
}
