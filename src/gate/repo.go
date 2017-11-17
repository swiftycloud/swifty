package main

import (
	"fmt"
	"bytes"
	"os/exec"
	"os"
	"io"
	"io/ioutil"
	"encoding/base64"
	"../common"
)

func fnRepoClone(fn *FunctionDesc, prefix string) string {
	return prefix + "/" + fn.Tennant + "/" + fn.Project + "/" + fn.Name
}

func fnRepoCheckout(conf *YAMLConf, fn *FunctionDesc) string {
	return fnRepoClone(fn, conf.Daemon.Sources.Share) + "/" + fn.Src.Commit
}

func checkoutSources(fn *FunctionDesc) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	share_to := "?"

	cloned_to := fnRepoClone(fn, conf.Daemon.Sources.Clone)
	cmd := exec.Command("git", "-C", cloned_to, "log", "-n1", "--pretty=format:%H")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		goto co_err
	}

	fn.Src.Commit = stdout.String()

	// Bring the necessary deps
	err = update_deps(fn.Script.Lang, cloned_to)
	if err != nil {
		goto co_err
	}

	// Now put the sources into shared place
	share_to = fnRepoCheckout(&conf, fn)

	log.Debugf("Checkout %s to %s", fn.Src.Commit[:12], share_to)
	err = copy_git_files(cloned_to, share_to)
	if err != nil {
		goto co_err
	}
	return nil

co_err:
	log.Errorf("can't checkout sources to %s: %s",
			share_to, err.Error())
	return err
}

var srcHandlers = map[string] struct {
	get func (*FunctionDesc) error
	update func (*FunctionDesc) error
} {
	"git": {
		get:	cloneGitRepo,
		update:	updateGitRepo,
	},

	"code": {
		get:	getFileFromReq,
		update:	updateFileFromReq,
	},
}

func getSources(fn *FunctionDesc) error {
	srch, ok := srcHandlers[fn.Src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", fn.Src.Type)
	}

	return srch.get(fn)
}

func cloneGitRepo(fn *FunctionDesc) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := fnRepoClone(fn, conf.Daemon.Sources.Clone)
	log.Debugf("Git clone %s -> %s", fn.Src.Repo, clone_to)

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		log.Errorf("repo for %s is already there", fn.SwoId.Str())
		return fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		log.Errorf("can't create %s: %s", clone_to, err.Error())
		return err
	}

	cmd := exec.Command("git", "-C", clone_to, "clone", fn.Src.Repo, ".")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		log.Errorf("can't clone %s -> %s: %s (%s:%s)",
				fn.Src.Repo, clone_to, err.Error(),
				stdout.String(), stderr.String())
		return err
	}

	return checkoutSources(fn)
}

func getFileFromReq(fn *FunctionDesc) error {
	fn.Src.Commit = noCommit

	to := fnRepoCheckout(&conf, fn)
	err := os.MkdirAll(to, 0750)
	if err != nil {
		return fmt.Errorf("Can't put sources")
	}

	data, err := base64.StdEncoding.DecodeString(fn.Src.Code)
	if err != nil {
		return fmt.Errorf("Error decoding sources")
	}

	err = ioutil.WriteFile(to + "/" + fn.Script.Run, data, 0600)
	if err != nil {
		return fmt.Errorf("Can't write source file")
	}

	return nil
}

func updateSources(fn *FunctionDesc) error {
	srch, ok := srcHandlers[fn.Src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", fn.Src.Type)
	}

	return srch.update(fn)
}

func updateGitRepo(fn *FunctionDesc) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := fnRepoClone(fn, conf.Daemon.Sources.Clone)
	log.Debugf("Git pull %s", clone_to)

	cmd := exec.Command("git", "-C", clone_to, "pull")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Errorf("can't pull %s -> %s: %s (%s:%s)",
				fn.Src.Repo, clone_to, err.Error(),
				stdout.String(), stderr.String())
		return err
	}

	return checkoutSources(fn)
}

func updateFileFromReq(fn *FunctionDesc) error {
	log.Errorf("No update of direct code possible (yet)")
	return fmt.Errorf("Can't update code from req")
}

func cleanRepo(fn *FunctionDesc) {
	sd := fnRepoClone(fn, "")

	clone_to := conf.Daemon.Sources.Clone
	swy.DropDir(clone_to, sd)

	share_to := conf.Daemon.Sources.Share
	swy.DropDir(share_to, sd)
}

func update_deps(lang, repo_path string) error {
	// First -- check git submodules
	_, err := os.Stat(repo_path + "/.gitmodules")
	if err == nil {
		err = update_git_submodules(repo_path)
	} else if !os.IsNotExist(err) {
		err = fmt.Errorf("Can't update git submodules: %s", err.Error())
	} else {
		err = nil
	}

	if err != nil {
		log.Error("Can't update git submodules")
		return err
	}

	return nil
}

func update_git_submodules(repo_path string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	log.Debugf("Updating git submodules @%s", repo_path)

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

