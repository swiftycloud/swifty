package main

import (
	"os/exec"
	"os"
	"bytes"
	"errors"
	"strings"
	"context"
)

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
	Install:	goInstall,
	BuildPkgPath:	goPkgPath,
}

func goInstall(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if strings.Contains(id.Name, "...") {
		return errors.New("No wildcards (at least yet)")
	}

	tgt_dir := packagesDir() + "/" + id.Tennant + "/golang"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/go", rtLangImage("golang"), "go", "get", id.Name}
	ctxlog(ctx).Debugf("Running docker %v", args)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logSaveResult(ctx, id.PCookie(), "pkg_install", stdout.String(), stderr.String())
		return errors.New("Error installing pkg")
	}

	return nil
}

func goPkgPath(id SwoId) string {
	/*
	 * Build dep mounts volume's packages subdir to /go-pkg
	 * Wdog builder sets GOPATH to /go:/<this-string>
	 */
	return "/go-pkg/" + id.Tennant + "/golang"
}
