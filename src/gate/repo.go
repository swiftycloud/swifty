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
	"path/filepath"
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

func cloneDir() string {
	return conf.Home + "/" + CloneDir
}

var repStates = map[int]string {
	swy.DBRepoStateCln:	"cloning",
	swy.DBRepoStateStl:	"stalled",
	swy.DBRepoStateRem:	"removing",
	swy.DBRepoStateRdy:	"ready",
}

type RepoDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Type		string		`bson:"type"`
	State		int		`bson:"state"`
	Commit		string		`bson:"commit,omitempty"`
	UserData	string		`bson:"userdata,omitempty"`
}

func (rd *RepoDesc)path() string {
	return rd.SwoId.Tennant + "/" + rd.ObjID.Hex()
}

func (rd *RepoDesc)clonePath() string {
	return cloneDir() + "/" + rd.path()
}

func (rd *RepoDesc)URL() string { return rd.SwoId.Name }

func getRepoDesc(id *SwoId, params *swyapi.RepoAdd) *RepoDesc {
	return &RepoDesc {
		SwoId:		*id,
		Type:		params.Type,
		UserData:	params.UserData,
	}
}

func (rd *RepoDesc)toInfo(ctx context.Context, conf *YAMLConf, details bool) (*swyapi.RepoInfo, *swyapi.GateErr) {
	r := &swyapi.RepoInfo {
		ID:		rd.ObjID.Hex(),
		Type:		rd.Type,
		URL:		rd.URL(),
		State:		repStates[rd.State],
		Commit:		rd.Commit,
	}

	if details {
		r.UserData = rd.UserData
	}

	return r, nil
}

func (rd *RepoDesc)Attach(ctx context.Context, conf *YAMLConf) (string, *swyapi.GateErr) {
	rd.ObjID = bson.NewObjectId()
	rd.State = swy.DBRepoStateCln

	if rd.Type != "github" {
		return "", GateErrM(swy.GateBadRequest, "Unsupported repo type")
	}

	err := dbInsert(ctx, rd)
	if err != nil {
		return "", GateErrD(err)
	}

	go cloneRepo(rd)

	return rd.ObjID.Hex(), nil
}

func (rd *RepoDesc)Detach(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	err := dbRepoDeactivate(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	rd.State = swy.DBRepoStateRem

	_, err = swy.DropDir(cloneDir(), rd.path())
	if err != nil {
		return GateErrE(swy.GateFsError, err)
	}

	err = dbRemoveId(ctx, &RepoDesc{}, rd.ObjID)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (rd *RepoDesc)listFiles(ctx context.Context) ([]string, *swyapi.GateErr) {
	searchDir := rd.clonePath()
	fileList := []string{}
	err := filepath.Walk(searchDir, func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			if f.Name() == ".git" {
				return filepath.SkipDir
			}

			return nil
		}

		path, _ = filepath.Rel(searchDir, path)
		fileList = append(fileList, path)
		return nil
	})

	if err != nil {
		return nil, GateErrE(swy.GateFsError, err)
	}

	return fileList, nil
}

func (rd *RepoDesc)pull(ctx context.Context) *swyapi.GateErr {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()
	ctxlog(ctx).Debugf("Git pull %s", clone_to)

	cmd := exec.Command("git", "-C", clone_to, "pull")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't pull %s -> %s: %s (%s:%s)",
			rd.URL(), clone_to, err.Error(),
			stdout.String(), stderr.String())
		return GateErrE(swy.GateGenErr, err)
	}

	return nil
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
	get func (context.Context, *FunctionDesc, *swyapi.FunctionSources) error
} {
	"git":		{ get: getFileFromRepo, },
	"code":		{ get: getFileFromReq, },
	"swage":	{ get: swageFile, },
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

func getSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", src.Type)
	}

	fn.Src.Version = zeroVersion
	return srch.get(ctx, fn, src)
}

func updateSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", src.Type)
	}

	ov, _ := strconv.Atoi(fn.Src.Version)
	fn.Src.Version = strconv.Itoa(ov + 1)

	return srch.get(ctx, fn, src)
}

func cloneRepo(rd *RepoDesc) {
	ctx, done := mkContext("::gitclone")
	defer done(ctx)

	nst := swy.DBRepoStateRdy

	commit, err := rd.Clone(ctx)
	if err != nil {
		/* FIXME -- keep logs and show them user */
		nst = swy.DBRepoStateStl
	}

	dbUpdateId(ctx, rd.ObjID, bson.M{ "state": nst, "commit": commit }, &RepoDesc{})
}

func (rd *RepoDesc)Clone(ctx context.Context) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()
	ctxlog(ctx).Debugf("Git clone %s -> %s", rd.URL(), clone_to)

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		ctxlog(ctx).Errorf("repo for %s is already there", rd.SwoId.Str())
		return "", fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		ctxlog(ctx).Errorf("can't create %s: %s", clone_to, err.Error())
		return "", err
	}

	cmd := exec.Command("git", "-C", clone_to, "clone", rd.URL(), ".")
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

func swageFile(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	if src.Swage == nil {
		return errors.New("No swage params")
	}

	tf := src.Swage.Template
	if strings.Contains(tf, "/") {
		return errors.New("Bad swage name")
	}

	fnCode, err := ioutil.ReadFile(conf.Home + "/" + SwageDir + "/" + fn.Code.Lang + "/" + tf + ".sw")
	if err != nil {
		return errors.New("Can't read swage")
	}

	for k, v := range src.Swage.Params {
		fnCode = bytes.Replace(fnCode, []byte(k), []byte(v), -1)
	}

	return writeSourceRaw(ctx, fn, fnCode)
}

func getFileFromRepo(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	ids := strings.SplitN(src.Repo, "/", 2)
	if len(ids) != 2 || !bson.IsObjectIdHex(ids[0]) {
		return errors.New("Bad repo file ID")
	}

	var rd RepoDesc
	err := dbFind(ctx, ctxObjId(ctx, ids[0]), &rd)
	if err != nil {
		return err
	}

	fnCode, err := ioutil.ReadFile(rd.clonePath() + "/" + ids[1])
	if err != nil {
		return err
	}

	return writeSourceRaw(ctx, fn, fnCode)
}

func getFileFromReq(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	return writeSource(ctx, fn, src.Code)
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

func cleanRepo(ctx context.Context, fn *FunctionDesc) error {
	sd := fnCodeDir(fn)

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

