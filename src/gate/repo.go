package main

import (
	"fmt"
	"bytes"
	"strings"
	"os/exec"
	"os"
	"context"
	"io"
	"time"
	"io/ioutil"
	"encoding/base64"
	"strconv"
	"errors"
	"../common"
	"../common/xwait"
	"../apis/apps"
)

func fnCodeDir(fn *FunctionDesc) string {
	return fn.Tennant + "/" + fn.Project + "/" + fn.Name
}

func fnCodeVersionDir(fn *FunctionDesc, version string) string {
	return fnCodeDir(fn) + "/" + version
}

func fnCodeLatestDir(fn *FunctionDesc) string {
	return fnCodeVersionDir(fn, fn.Src.Version)
}

func fnCodeVersionPath(conf *YAMLConf, fn *FunctionDesc, version string) string {
	return conf.Wdog.Volume + "/" + fnCodeVersionDir(fn, version)
}

func fnCodeLatestPath(conf *YAMLConf, fn *FunctionDesc) string {
	return fnCodeVersionPath(conf, fn, fn.Src.Version)
}

func fnRepoClone(fn *FunctionDesc) string {
	return conf.Daemon.Sources.Clone + "/" + fnCodeDir(fn)
}

func checkoutSources(ctx context.Context, fn *FunctionDesc) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	share_to := "?"

	cloned_to := fnRepoClone(fn)
	cmd := exec.Command("git", "-C", cloned_to, "log", "-n1", "--pretty=format:%H")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		goto co_err
	}

	fn.Src.Version = stdout.String()

	// Bring the necessary deps
	err = update_deps(ctx, cloned_to)
	if err != nil {
		goto co_err
	}

	// Now put the sources into shared place
	share_to = fnCodeLatestPath(&conf, fn)

	ctxlog(ctx).Debugf("Checkout %s to %s", fn.Src.Version[:12], share_to)
	err = copy_git_files(cloned_to, share_to)
	if err != nil {
		goto co_err
	}

	return nil

co_err:
	ctxlog(ctx).Errorf("can't checkout sources to %s: %s",
			share_to, err.Error())
	return err
}

var srcHandlers = map[string] struct {
	get func (context.Context, *FunctionDesc) error
	update func (context.Context, *FunctionDesc, *swyapi.FunctionSources) error
	check func (string, []string) bool
} {
	"git": {
		get:	cloneGitRepo,
		update:	updateGitRepo,
	},

	"code": {
		get:	getFileFromReq,
		update:	updateFileFromReq,
		check:	checkFileVersion,
	},

	"swage": {
		get:	swageFile,
	},
}

func checkFileVersion(version string, versions []string) bool {
	cver, _ := strconv.Atoi(version)
	for _, v := range versions {
		/* For files we just generate sequential numbers */
		hver, _ := strconv.Atoi(v)
		if cver <= hver {
			return true
		}
	}

	return false
}

func checkVersion(ctx context.Context, fn *FunctionDesc, version string, versions []string) bool {
	srch, _ := srcHandlers[fn.Src.Type]
	return srch.check(version, versions)
}

func getSources(ctx context.Context, fn *FunctionDesc) error {
	srch, ok := srcHandlers[fn.Src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", fn.Src.Type)
	}

	return srch.get(ctx, fn)
}

func cloneGitRepo(ctx context.Context, fn *FunctionDesc) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if !SwyModeDevel {
		return fmt.Errorf("Disabled sources type git")
	}

	clone_to := fnRepoClone(fn)
	ctxlog(ctx).Debugf("Git clone %s -> %s", fn.Src.Repo, clone_to)

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		ctxlog(ctx).Errorf("repo for %s is already there", fn.SwoId.Str())
		return fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		ctxlog(ctx).Errorf("can't create %s: %s", clone_to, err.Error())
		return err
	}

	cmd := exec.Command("git", "-C", clone_to, "clone", fn.Src.Repo, ".")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't clone %s -> %s: %s (%s:%s)",
				fn.Src.Repo, clone_to, err.Error(),
				stdout.String(), stderr.String())
		return err
	}

	return checkoutSources(ctx, fn)
}

func writeSource(ctx context.Context, fn *FunctionDesc, codeb64 string) error {
	data, err := base64.StdEncoding.DecodeString(codeb64)
	if err != nil {
		return fmt.Errorf("Error decoding sources")
	}

	return writeSourceRaw(ctx, fn, data)
}

func writeSourceRaw(ctx context.Context, fn *FunctionDesc, data []byte) error {
	to := fnCodeLatestPath(&conf, fn)
	err := os.MkdirAll(to, 0750)
	if err != nil {
		ctxlog(ctx).Error("Can't mkdir sources: %s", err.Error())
		return errors.New("FS error")
	}

	script := RtDefaultScriptName(&fn.Code)

	err = ioutil.WriteFile(to + "/" + script, data, 0600)
	if err != nil {
		ctxlog(ctx).Error("Can't write sources: %s", err.Error())
		return errors.New("FS error")
	}

	return nil
}

func swageFile(ctx context.Context, fn *FunctionDesc) error {
	if fn.Src.swage == nil {
		return errors.New("No swage params")
	}

	tf := fn.Src.swage.Template
	if strings.Contains(tf, "/") {
		return errors.New("Bad swage name")
	}

	fnCode, err := ioutil.ReadFile(conf.Swage + "/" + fn.Code.Lang + "/" + tf + ".sw")
	if err != nil {
		return errors.New("Can't read swage")
	}

	for k, v := range fn.Src.swage.Params {
		fnCode = bytes.Replace(fnCode, []byte(k), []byte(v), -1)
	}

	fn.Src.Type = "code"
	fn.Src.Version = zeroVersion

	return writeSourceRaw(ctx, fn, fnCode)
}

func getFileFromReq(ctx context.Context, fn *FunctionDesc) error {
	fn.Src.Version = zeroVersion
	return writeSource(ctx, fn, fn.Src.Code)
}

func updateSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	if fn.Src.Type != src.Type {
		return errors.New("Bad source type")
	}

	srch, ok := srcHandlers[fn.Src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", fn.Src.Type)
	}

	return srch.update(ctx, fn, src)
}

func GCOldSources(ctx context.Context, fn *FunctionDesc, ver string) {
	np, err := swy.DropDirPrep(conf.Wdog.Volume, fnCodeVersionDir(fn, ver))
	if err != nil {
		ctxlog(ctx).Errorf("Leaking %s sources till FN removal (err %s)", ver, err.Error())
		return
	}

	if np == "" {
		return
	}

	w := xwait.Prepare(fn.Cookie)
	cookie := fn.Cookie
	ctxlog(ctx).Debugf("Will remove %s's sources after a while via %s", ver, np)

	go func() {
		tmo := 16 * 60 * time.Second
		ctx, done := mkContext("::gcoldsource")
		defer done(ctx)

		for {
			vers, err := dbBalancerRSListVersions(ctx, cookie)
			if err != nil {
				break /* What to do? */
			}

			found := false
			for _, v := range(vers) {
				if ver == v {
					found = true
					break
				}
			}

			if !found {
				ctxlog(ctx).Debugf("Dropping %s.%s sources", cookie, ver)
				swy.DropDirComplete(np)
				break
			}

			if w.Wait(&tmo) {
				ctxlog(ctx).Errorf("Leaking %s sources till FN removal (tmo)", ver)
				break
			}
		}

		w.Done()
	}()
}

func updateGitRepo(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := fnRepoClone(fn)
	ctxlog(ctx).Debugf("Git pull %s", clone_to)

	cmd := exec.Command("git", "-C", clone_to, "pull")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't pull %s -> %s: %s (%s:%s)",
				fn.Src.Repo, clone_to, err.Error(),
				stdout.String(), stderr.String())
		return err
	}

	return checkoutSources(ctx, fn)
}

func updateFileFromReq(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	ov, _ := strconv.Atoi(fn.Src.Version)
	fn.Src.Version = strconv.Itoa(ov + 1)

	return writeSource(ctx, fn, src.Code)
}

func cleanRepo(ctx context.Context, fn *FunctionDesc) error {
	sd := fnCodeDir(fn)

	td, err := swy.DropDir(conf.Daemon.Sources.Clone, sd)
	if err != nil {
		return err
	}

	if td != "" {
		ctxlog(ctx).Debugf("Will remove %s repo clone via %s", fn.SwoId.Str(), td)
	}

	td, err = swy.DropDir(conf.Wdog.Volume, sd)
	if err != nil {
		return err
	}

	if td != "" {
		ctxlog(ctx).Debugf("Will remove %s sources via %s", fn.SwoId.Str(), td)
	}

	return nil
}

func update_deps(ctx context.Context, repo_path string) error {
	// First -- check git submodules
	_, err := os.Stat(repo_path + "/.gitmodules")
	if err == nil {
		err = update_git_submodules(ctx, repo_path)
	} else if !os.IsNotExist(err) {
		err = fmt.Errorf("Can't update git submodules: %s", err.Error())
	} else {
		err = nil
	}

	if err != nil {
		ctxlog(ctx).Error("Can't update git submodules")
		return err
	}

	return nil
}

func update_git_submodules(ctx context.Context, repo_path string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	ctxlog(ctx).Debugf("Updating git submodules @%s", repo_path)

	cmd := exec.Command("git", "-C", repo_path, "submodule", "init")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return err
	}

	cmd = exec.Command("git", "-C", repo_path, "submodule", "update")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

// Checkout helpers -- this code just copies the tree around skipping
// the .git ones everywhere.
func copy_git_files(from, to string) error {
	st, err := os.Stat(from)
	if err != nil {
		return co_err(from, "stat", err)
	}

	err = os.MkdirAll(to, st.Mode())
	if err != nil {
		return co_err(to, "mkdirall", err)
	}

	return copy_dir(from, to)
}

func copy_dir(from, to string) error {
	dir, err := os.Open(from)
	if err != nil {
		return co_err(from, "opendir", err)
	}

	ents, err := dir.Readdir(-1)
	dir.Close() // This keeps minimum fds across recursion below
	if err != nil {
		return co_err(from, "readdir", err)
	}

	for _, ent := range ents {
		ff := from + "/" + ent.Name()
		ft := to + "/" + ent.Name()

		if ent.IsDir() {
			if ent.Name() == ".git" {
				continue
			}
			err = os.Mkdir(ft, ent.Mode())
			if err != nil {
				return co_err(ft, "mkdir", err)
			}

			err = copy_dir(ff, ft)
		} else {
			mode := ent.Mode()
			if mode & os.ModeType == 0 {
				err = copy_file(ff, ft, mode)
			} else {
				err = copy_node(ff, ft, mode)
			}
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func copy_file(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return co_err(from, "open", err)
	}
	defer in.Close()

	out, err := os.Create(to)
	if err != nil {
		return co_err(to, "create", err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return co_err(to, "copy", err)
	}

	err = os.Chmod(to, mode)
	if err != nil {
		return co_err(to, "chmod", err)
	}

	return nil
}

func copy_node(from, to string, mode os.FileMode) error {
	if mode & os.ModeSymlink != 0 {
		tgt, err := os.Readlink(from)
		if err != nil {
			return co_err(from, "readlink", err)
		}

		err = os.Symlink(tgt, to)
		if err != nil {
			return co_err(to, "symlink", err)
		}

		return nil
	}

	return fmt.Errorf("Unsupported mode (%s)", from)
}

func co_err(fn, reason string, o_err error) error {
	return fmt.Errorf("Error on %s (%s): %s", reason, fn, o_err.Error())
}

