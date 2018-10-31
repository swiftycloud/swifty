package main

import (
	"strings"
	"context"
	"bytes"
	"os"
	"os/exec"
	"errors"
	"swifty/common"
)

var nodejs_info = langInfo {
	Ext:		"js",
	CodePath:	"/function",

	/*
	 * Install -- use npm install
	 * List    -- list top-level dirs with package.json inside
	 * Remove  -- manualy remove the dir
	 */
	Install:	npmInstall,
	Remove:		nodeRemove,
	RunPkgPath:	nodeModules,
}

func nodeModules(id SwoId) (string, string) {
	/*
	 * Node's runner-js.sh sets /home/packages/node_modules as NODE_PATH
	 */
	return packagesDir() + "/" + id.Tennant + "/nodejs", "/home/packages"
}

func nodeRemove(ctx context.Context, id SwoId) error {
	if strings.Contains(id.Name, "..") || strings.Contains(id.Name, "/") {
		return errors.New("Bad package name")
	}

	d := packagesDir() + "/" + id.Tennant + "/nodejs/node_modules"
	_, err := os.Stat(d + "/" + id.Name + "/package.json")
	if err != nil {
		return errors.New("Package not installed")
	}

	x, err := xh.DropDir(d, id.Name)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove %s' sources (%s): %s", id.Str(), x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func npmInstall(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	tgt_dir := packagesDir() + "/" + id.Tennant + "/nodejs"
	os.MkdirAll(tgt_dir, 0755)
	/*
	 * Node's container sets HOME to /home/swifty and npm installs stuff
	 * there as it happens to be it's wdir for this launch
	 */
	args := []string{"run", "--rm", "-v", tgt_dir + ":/home/swifty", rtLangImage("nodejs"),
				"npm", "install", "--no-save", id.Name}
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
