package main

var ruby_info = langInfo {
	Ext:		"rb",
	CodePath:	"/function",
	VArgs:		[]string{"ruby", "--version"},
	PList:		func() []string {
		return GetLines("ruby", "gem", "list")
	},
}

