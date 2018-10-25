package main

var py_info = langInfo {
	Ext:		"py",
	CodePath:	"/function",
	VArgs:		[]string{"python3", "--version"},
	PList:		func() []string {
		return GetLines("python", "pip3", "list", "--format", "freeze")
	},
}
