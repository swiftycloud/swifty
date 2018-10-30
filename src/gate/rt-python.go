package main

import (
	"context"
	"bytes"
	"os"
	"strings"
	"errors"
	"swifty/apis"
	"os/exec"
)

var py_info = langInfo {
	Ext:		"py",
	CodePath:	"/function",
	Info:		pyInfo,

	/*
	 * Install -- call pip install
	 * List    -- use pkg_resources, but narrow down it to the /packages
	 * Remove  -- use pip remove, but pre-check that the package is in /packages
	 */
	Install:	pipInstall,
	Remove:		xpipRemove,
	List:		xpipList,
	RunPkgPath:	pyPackages,
}

func pyInfo() *swyapi.LangInfo {
	args := []string{"run", "--rm", rtLangImage("python"), "python3", "--version"}
	vout, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil
	}

	ps := GetLines("python", "pip3", "list", "--format", "freeze")
	if ps == nil {
		return nil
	}

	return &swyapi.LangInfo {
		Version:	string(vout),
		Packages:	ps,
	}
}

func pyPackages(id SwoId) (string, string) {
	/* Python runner adds /packages/* to sys.path for every dir met in there */
	return packagesDir() + "/" + id.Tennant + "/python", "/packages"
}

func xpipList(ctx context.Context, tenant string) ([]string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tgt_dir := packagesDir() + "/" + tenant + "/python"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/packages", rtLangImage("python"),
				"python3", "/usr/bin/xpip.py", "list"}
	ctxlog(ctx).Debugf("Running docker %v", args)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, errors.New("Error listing pkg")
	}

	sout := strings.TrimSpace(stdout.String())
	return strings.Split(sout, "\n"), nil
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

func xpipRemove(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tgt_dir := packagesDir() + "/" + id.Tennant + "/python"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/packages", rtLangImage("python"),
				"python3", "/usr/bin/xpip.py", "remove", id.Name}
	ctxlog(ctx).Debugf("Running docker %v", args)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logSaveResult(ctx, id.PCookie(), "pkg_remove", stdout.String(), stderr.String())
		return errors.New("Error removing pkg")
	}

	return nil
}
