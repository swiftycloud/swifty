package main

import (
	"os/exec"
	"os"
	"bytes"
	"errors"
	"strings"
	"context"
	"swifty/common"
	"path/filepath"
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
	Remove:		goRemove,
	List:		goList,
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

func goRemove(ctx context.Context, id SwoId) error {
	if strings.Contains(id.Name, "..") {
		return errors.New("Bad package name")
	}

	d := packagesDir() + "/" + id.Tennant + "/golang"
	st, err := os.Stat(d + "src/" + id.Name + "/.git")
	if err != nil || !st.IsDir() {
		return errors.New("Package not installed")
	}

	err = os.Remove(d + "/pkg/" + goOsArch + "/" + id.Name + ".a")
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove %s' package: %s", id.Str(), err.Error())
		return errors.New("Error removing pkg")
	}

	x, err := xh.DropDir(d, "src/" + id.Name)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove %s' sources (%s): %s", id.Str(), x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func goList(ctx context.Context, tenant string) ([]string, error) {
	stuff := []string{}

	d := packagesDir() + "/" + tenant + "/golang/src"
	err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, "/.git") {
			path, _ = filepath.Rel(d, path)	// Cut the packages folder
			path = filepath.Dir(path)	// Cut the .git one
			stuff = append(stuff, path)
			return filepath.SkipDir
		}

		return nil
	})

	if err != nil {
		return nil, errors.New("Error listing packages")
	}

	return stuff, nil
}

func goPkgPath(id SwoId) string {
	/*
	 * Build dep mounts volume's packages subdir to /go-pkg
	 * Wdog builder sets GOPATH to /go:/<this-string>
	 */
	return "/go-pkg/" + id.Tennant + "/golang"
}
