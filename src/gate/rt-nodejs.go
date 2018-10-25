package main

import (
	"strings"
)

var nodejs_info = langInfo {
	Ext:		"js",
	CodePath:	"/function",
	VArgs:		[]string{"node", "--version"},
	PList:		func() []string {
		o := GetLines("nodejs", "npm", "list")
		ret := []string{}
		if len(o) > 0 {
			for _, p := range(o[1:]) {
				ps := strings.Fields(p)
				ret = append(ret, ps[len(ps)-1])
			}
		}
		return ret
	},
}

