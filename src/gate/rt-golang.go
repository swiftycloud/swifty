package main

const (
	goOsArch string = "linux_amd64" /* FIXME -- run go env and parse */
)

var golang_info = langInfo {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,

	/*
	 * Install -- call go get <name>
	 * List    -- check for .git subdirs in a tree
	 * Remove  -- manually remove the whole dir (and .a from pkg)
	 */
	BuildPkgPath:	goPkgPath,
}

func goPkgPath(id SwoId) string {
	/*
	 * Build dep mounts volume's packages subdir to /go-pkg
	 * Wdog builder sets GOPATH to /go:/<this-string>
	 */
	return "/go-pkg/" + id.Tennant + "/golang"
}
