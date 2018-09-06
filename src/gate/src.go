package main

import (
	"gopkg.in/mgo.v2/bson"
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
	"../apis"
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
	get func (context.Context, *swyapi.FunctionSources, string, string) error
} {
	"git":		{ get: getFileFromRepo, },
	"code":		{ get: getFileFromReq, },
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
	fn.Src.Version = zeroVersion
	return writeSources(ctx, fn, src, "")
}

func getSources(ctx context.Context, fn *FunctionDesc) ([]byte, error) {
	codeFile := fn.srcPath("") + "/" + RtScriptName(&fn.Code, "")
	return ioutil.ReadFile(codeFile)
}

func updateSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	ov, _ := strconv.Atoi(fn.Src.Version)
	fn.Src.Version = strconv.Itoa(ov + 1)
	return writeSources(ctx, fn, src, "")
}

func writeSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources, suff string) error {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", src.Type)
	}

	return srch.get(ctx, src, fn.srcPath(""), RtScriptName(&fn.Code, suff))
}

func writeTempSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) (string, error) {
	/* Call to this fn is locked per-tenant, so ... */
	return "tmp", writeSources(ctx, fn, src, "tmp")
}

func bgClone(rd *RepoDesc, ac *AccDesc, rh *repoHandler) {
	ctx, done := mkContext("::gitclone")
	defer done(ctx)

	commit, err := rh.clone(ctx, rd, ac)
	if err != nil {
		/* FIXME -- keep logs and show them user */
		dbUpdatePart(ctx, rd, bson.M{ "state": swy.DBRepoStateStl })
		return
	}

	t := time.Now()
	dbUpdatePart(ctx, rd, bson.M{
					"state": swy.DBRepoStateRdy,
					"commit": commit,
					"last_pull": &t,
				})
}

func cloneGit(ctx context.Context, rd *RepoDesc, ac *AccDesc) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		ctxlog(ctx).Errorf("repo for %s is already there", rd.SwoId.Str())
		return "", fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		ctxlog(ctx).Errorf("can't create %s: %s", clone_to, err.Error())
		return "", err
	}

	curl := rd.URL()

	if ac != nil {
		if ac.Type != "github" {
			return "", errors.New("Corrupted acc type")
		}

		t, err := ac.Secrets["token"].value()
		if err != nil {
			return "", err
		}

		if t != "" && strings.HasPrefix(curl, "https://") {
			curl = "https://" + ac.SwoId.Name + ":" + t + "@" + curl[8:]
		}
	}

	ctxlog(ctx).Debugf("Git clone %s -> %s", curl, clone_to)

	cmd := exec.Command("git", "-C", clone_to, "clone", "--depth=1", curl, ".")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't clone %s -> %s: %s (%s:%s)",
				rd.URL(), clone_to, err.Error(),
				stdout.String(), stderr.String())
		return "", err
	}

	return gitCommit(clone_to)
}

func writeSourceFile(ctx context.Context, to, script string, data []byte) error {
	err := os.MkdirAll(to, 0750)
	if err != nil {
		ctxlog(ctx).Error("Can't mkdir sources: %s", err.Error())
		return errors.New("FS error")
	}

	err = ioutil.WriteFile(to + "/" + script, data, 0600)
	if err != nil {
		ctxlog(ctx).Error("Can't write sources: %s", err.Error())
		return errors.New("FS error")
	}

	return nil
}

func ctxRepoId(ctx context.Context, rid string) bson.M {
	return  bson.M{
		"tennant": bson.M { "$in": []string{gctx(ctx).Tenant, "*"}},
		"_id": bson.ObjectIdHex(rid),
	}
}

func getFileFromRepo(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	ids := strings.SplitN(src.Repo, "/", 2)
	if len(ids) != 2 || !bson.IsObjectIdHex(ids[0]) {
		return errors.New("Bad repo file ID")
	}

	var rd RepoDesc
	err := dbFind(ctx, ctxRepoId(ctx, ids[0]), &rd)
	if err != nil {
		return err
	}

	fnCode, err := ioutil.ReadFile(rd.clonePath() + "/" + ids[1])
	if err != nil {
		return err
	}

	return writeSourceFile(ctx, to, script, fnCode)
}

func getFileFromReq(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	data, err := base64.StdEncoding.DecodeString(src.Code)
	if err != nil {
		return fmt.Errorf("Error decoding sources")
	}

	return writeSourceFile(ctx, to, script, data)
}

func GCOldSources(ctx context.Context, fn *FunctionDesc, ver string) {
	np, err := swy.DropDirPrep(conf.Wdog.Volume, fn.srcDir(ver))
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

func removeSources(ctx context.Context, fn *FunctionDesc) error {
	sd := fn.srcRoot()

	td, err := swy.DropDir(conf.Home + "/" + CloneDir, sd)
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

