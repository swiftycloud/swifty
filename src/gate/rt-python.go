package main

import (
	"context"
	"bytes"
	"os"
	"errors"
	"os/exec"
)

var py_info = langInfo {
	Ext:		"py",
	CodePath:	"/function",
	VArgs:		[]string{"python3", "--version"},
	PList:		func() []string {
		return GetLines("python", "pip3", "list", "--format", "freeze")
	},

	Install:	pipInstall,
	RunPkgPath:	pyPackages,
}

func pyPackages(id SwoId) (string, string) {
	return packagesDir() + "/" + id.Tennant + "/python", "/packages"
}

func pipInstall(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tgt_dir := packagesDir() + "/" + id.Tennant + "/python"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/packages", rtLangImage("python"),
				"pip", "install", "--root", "/packages", id.Name}
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
