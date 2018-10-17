package main

import (
	"fmt"
	"bytes"
	"strings"
	"os/exec"
	"os"
	"context"
	"net/http"
	"io"
	"time"
	"io/ioutil"
	"encoding/base64"
	"strconv"
	"errors"
	"swifty/common"
	"swifty/common/xwait"
	"swifty/apis"
)

func (fn *FunctionDesc)srcRoot() string {
	return fn.Tennant + "/" + fn.Project + "/" + fn.Name
}

func (fn *FunctionDesc)srcDir(version string) string {
	if version == "" {
		version = fn.Src.Version
	}
	return fn.srcRoot() + "/" + version
}

func (fn *FunctionDesc)srcPath(version string) string {
	return conf.Wdog.Volume + "/" + fn.srcDir(version)
}

func cloneDir() string {
	return conf.Home + "/" + CloneDir
}

func gitCommit(dir string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command("git", "-C", dir, "log", "-n1", "--pretty=format:%H")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return stdout.String(), nil
}

var srcHandlers = map[string] struct {
	put func (context.Context, *swyapi.FunctionSources, string, string) error
} {
	"git":		{ put: putFileFromRepo, },
	"code":		{ put: putFileFromReq, },
	"url":		{ put: putFileFromUrl, },
}

func checkVersion(ctx context.Context, fn *FunctionDesc, version string, versions []string) (bool, error) {
	cver, _ := strconv.Atoi(version)
	for _, v := range versions {
		/* For files we just generate sequential numbers */
		hver, _ := strconv.Atoi(v)
		if cver <= hver {
			return true, nil
		}
	}

	return false, nil
}

func putSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	fn.Src = FnSrcDesc{ Version: zeroVersion }
	return putStdSources(ctx, fn, src)
}

func getSources(ctx context.Context, fn *FunctionDesc) ([]byte, error) {
	codeFile := fn.srcPath("") + "/" + rtScriptName(&fn.Code, "")
	return ioutil.ReadFile(codeFile)
}

func updateSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	ov, _ := strconv.Atoi(fn.Src.Version)
	fn.Src = FnSrcDesc{ Version: strconv.Itoa(ov + 1) }
	return putStdSources(ctx, fn, src)
}

func putStdSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	err := putSourceFile(ctx, fn, src, "")
	if err != nil {
		return err
	}

	if src.Type == "git" && src.Sync {
		ids := strings.SplitN(src.Repo, "/", 2)
		fn.Src.Repo = ids[0]
		fn.Src.File = ids[1]
	}

	return nil
}

func putTempSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) (string, error) {
	/* Call to this fn is locked per-tenant, so ... */
	return "tmp", putSourceFile(ctx, fn, src, "tmp")
}

func putSourceFile(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources, suff string) error {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", src.Type)
	}

	return srch.put(ctx, src, fn.srcPath(""), rtScriptName(&fn.Code, suff))
}

func writeSourceFile(ctx context.Context, to, script string, data io.Reader) error {
	err := os.MkdirAll(to, 0750)
	if err != nil {
		ctxlog(ctx).Error("Can't mkdir sources: %s", err.Error())
		return errors.New("FS error")
	}

	f, err := os.OpenFile(to + "/" + script, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		ctxlog(ctx).Error("Can't create sources: %s", err.Error())
		return errors.New("FS error")
	}

	_, err = io.Copy(f, data)
	if err != nil {
		ctxlog(ctx).Error("Can't write sources: %s", err.Error())
		return errors.New("FS error")
	}

	return nil
}

func putFileFromRepo(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	f, err := repoOpenFile(ctx, src.Repo)
	if err != nil {
		ctxlog(ctx).Errorf("Can't read file %s: %s", src.Repo, err.Error())
		return err
	}

	defer f.Close()

	return writeSourceFile(ctx, to, script, f)
}

func putFileFromReq(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	data, err := base64.StdEncoding.DecodeString(src.Code)
	if err != nil {
		return fmt.Errorf("Error decoding sources")
	}

	return writeSourceFile(ctx, to, script, bytes.NewReader(data))
}

func putFileFromUrl(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	resp, err := http.DefaultClient.Get(src.URL)
	if err != nil {
		return fmt.Errorf("Error GET-ing file")
	}

	defer resp.Body.Close()

	return writeSourceFile(ctx, to, script, resp.Body)
}

func GCOldSources(ctx context.Context, fn *FunctionDesc, ver string) {
	np, err := xh.DropDirPrep(conf.Wdog.Volume, fn.srcDir(ver))
	if err != nil {
		ctxlog(ctx).Errorf("Leaking %s sources till FN removal (err %s)", ver, err.Error())
		return
	}

	if np == "" {
		return
	}

	w := xwait.Prepare(fn.Cookie)
	cookie := fn.Cookie

	go func() {
		tmo := 16 * 60 * time.Second
		ctx, done := mkContext("::gcoldsource")
		defer done(ctx)

		for {
			vers, err := dbBalancerListVersions(ctx, cookie)
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
				xh.DropDirComplete(np)
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

func removeSources(ctx context.Context, fn *FunctionDesc) error {
	sd := fn.srcRoot()

	_, err := xh.DropDir(conf.Home + "/" + CloneDir, sd)
	if err != nil {
		return err
	}

	_, err = xh.DropDir(conf.Wdog.Volume, sd)
	if err != nil {
		return err
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

