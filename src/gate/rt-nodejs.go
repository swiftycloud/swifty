package main

import (
	"strings"
	"context"
	"bytes"
	"os"
	"os/exec"
	"errors"
)

var nodejs_info = langInfo {
	Ext:		"js",
	CodePath:	"/function",
	VArgs:		[]string{"node", "--version"},
	PList:		func() []string {
		o := GetLines("nodejs", "npm", "list")
		ret := []string{}
		if len(o) > 0 {
			for _, p := range(o[1:]) {
				ps := strings.Fields(p)
				ret = append(ret, ps[len(ps)-1])
			}
		}
		return ret
	},

	Install:	npmInstall,
	RunPkgPath:	nodeModules,
}

func nodeModules(id SwoId) (string, string) {
	/*
	 * Node's runner-js.sh sets /home/packages/node_modules as NODE_PATH
	 */
	return packagesDir() + "/" + id.Tennant + "/nodejs", "/home/packages"
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

