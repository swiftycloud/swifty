package main

import (
	"os/exec"
	"swifty/apis"
)

var ruby_info = langInfo {
	Ext:		"rb",
	CodePath:	"/function",
	Info:		rubyInfo,
}

func rubyInfo() *swyapi.LangInfo {
	args := []string{"run", "--rm", rtLangImage("ruby"), "ruby", "--version"}
	vout, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil
	}

	ps := GetLines("ruby", "gem", "list")
	if ps == nil {
		return nil
	}

	return &swyapi.LangInfo {
		Version:	string(vout),
		Packages:	ps,
	}
}
