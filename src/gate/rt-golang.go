package main

import (
	"os/exec"
	"os"
	"bytes"
	"context"
)

var golang_info = langInfo {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
	VArgs:		[]string{"go", "version"},

	Install:	goInstall,
	Remove:		goRemove,
	PkgPath	:	goPkgPath,
}

func goInstall(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tgt_dir := conf.Wdog.Volume + "/packages/" + id.Tennant + "/golang"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/go", rtLangImage("golang"), "go", "get", id.Name}
	ctxlog(ctx).Debugf("Running docker %v", args)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logSaveResult(ctx, id.PCookie(), "pkg_install", stdout.String(), stderr.String())
		return err
	}

	return nil
}

func goRemove(id SwoId) error {
	return nil
}

func goPkgPath(id SwoId) string {
	return "/go-pkg/" + id.Tennant + "/golang"
}
