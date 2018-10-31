package main

import (
	"path/filepath"
	"swifty/apis"
	"strconv"
	"swifty/common/http"
	"context"
)

type langInfo struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
	ServiceIP	string

	LInfo		*swyapi.LangInfo

	Install		func(context.Context, SwoId) error
	Remove		func(context.Context, SwoId) error
	List		func(context.Context, string) ([]string, error)

	BuildPkgPath	func(SwoId) string
	RunPkgPath	func(SwoId) (string, string)
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

func getLangInfos(wl string) {
	for l, h := range rt_handlers {
		if wl != "*" && wl != l {
			continue
		}

		li := getInfo(l, h)
		if li == nil {
			continue
		}

		glog.Debugf("Set %s lang info: %v", l, li)
		h.LInfo = li
	}
}

func RtInit() {
	glog.Debugf("Will detect rt languages in the background")
	go getLangInfos("*")
	addSysctl("lang_info_refresh", func() string { return "set language name or * here" },
		func(v string) error {
			getLangInfos(v)
			return nil
		},
	)

}

func getInfo(l string, rh *langInfo) *swyapi.LangInfo {
	var result swyapi.LangInfo

	resp, err := xhttp.Req(
			&xhttp.RestReq{
				Method:  "GET",
				Address: rtService(rh, "info"),
				Timeout: 120,
			}, nil)
	if err != nil {
		glog.Errorf("Error getting info from %s: %s", l, err.Error())
		return nil
	}

	err = xhttp.RResp(resp, &result)
	if err != nil {
		glog.Errorf("Can't parse %s info result: %s", l, err.Error())
		return nil
	}

	return &result
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

func rtSetService(lang, ip string) {
	rt_handlers[lang].ServiceIP= ip
}

func rtService(rh *langInfo, call string) string {
	return "http://" + rh.ServiceIP + ":" + strconv.Itoa(conf.Wdog.Port) + "/v1/" + call
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
	return lh.LInfo
}

func packagesDir() string {
	return conf.Wdog.Volume + "/" + PackagesSubdir
}
