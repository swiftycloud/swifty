package main

import (
	"errors"
	"strings"
	"os/exec"
	"path/filepath"
	"os"

	"swifty/common"
)

func goList(dir string) ([]string, error) {
	stuff := []string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, "/.git") {
			path, _ = filepath.Rel(dir, path) // Cut the packages folder
			path = filepath.Dir(path)       // Cut the .git one
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

func goInfo() (string, []string, error) {
	v, err := exec.Command("go", "version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := goList("/go/src")
	if err != nil {
		return "", nil, err
	}

	return string(v), ps, nil
}

func goPackages(tenant string) ([]string, error) {
	return goList("/go-pkg/" + tenant + "/golang/src")
}

const (
        goOsArch string = "linux_amd64" /* FIXME -- run go env and parse */
)

func goInstall(tenant, name string) error {
	if strings.Contains(name, "...") {
		return errors.New("No wildcards (at least yet)")
	}

	cmd := exec.Command("go", "get", name)
	env := []string{}
	for _, oe := range os.Environ() {
		if strings.HasPrefix(oe, "GOPATH=") {
			env = append(env, "GOPATH=/go-pkg/" + tenant + "/golang")
		} else {
			env = append(env, oe)
		}
	}
	cmd.Env = env
	return cmd.Run()
}

func goRemove(tenant, name string) error {
	if strings.Contains(name, "..") {
		return errors.New("Bad package name")
	}

	d := "/go-pkg/" + tenant + "/golang"
	st, err := os.Stat(d + "/src/" + name + "/.git")
	if err != nil || !st.IsDir() {
		return errors.New("Package not installed")
	}

	err = os.Remove(d + "/pkg/" + goOsArch + "/" + name + ".a")
	if err != nil {
		log.Errorf("Can't remove %s' package %s: %s", tenant, name, err.Error())
		return errors.New("Error removing pkg")
	}

	x, err := xh.DropDir(d, "/src/" + name)
	if err != nil {
		log.Errorf("Can't remove %s' sources %s (%s): %s", tenant, name, x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func pyInfo() (string, []string, error) {
	v, err := exec.Command("python3", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := exec.Command("pip3", "list", "--format", "freeze").Output()
	if err != nil {
		return "", nil, err
	}

	return string(v), xh.GetLines(ps), nil
}

func xpipPackages(tenant string) ([]string, error) {
	ps, err := exec.Command("python3", "/usr/bin/xpip.py", tenant, "list").Output()
	if err != nil {
		return nil, err
	}

	return xh.GetLines(ps), nil
}

func pipInstall(tenant, name string) error {
	return exec.Command("pip", "install", "--root", "/packages/" + tenant + "/python", name).Run()
}

func xpipRemove(tenant, name string) error {
	return exec.Command("python3", "/usr/bin/xpip.py", tenant, "remove", name).Run()
}

func nodeInfo() (string, []string, error) {
	v, err := exec.Command("node", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	out, err := exec.Command("npm", "list").Output()
	if err != nil {
		return "", nil, err
	}

	o := xh.GetLines(out)
	ret := []string{}
	if len(o) > 0 {
		for _, p := range(o[1:]) {
			ps := strings.Fields(p)
			ret = append(ret, ps[len(ps)-1])
		}
	}

	return string(v), ret, nil
}

func nodeModules(tenant string) ([]string, error) {
	stuff := []string{}

	d := "/packages/" + tenant + "/nodejs/node_modules"
	dir, err := os.Open(d)
	if err != nil {
		return nil, errors.New("Error accessing node_modules")
	}

	ents, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return nil, errors.New("Error reading node_modules")
	}

	for _, sd := range ents {
		_, err := os.Stat(d + "/" + sd + "/package.json")
		if err == nil {
			stuff = append(stuff, sd)
		}
	}

	return stuff, nil
}

func npmInstall(tenant, name string) error {
	cmd := exec.Command("npm", "install", "--no-save", name)
	cmd.Dir = "/packages/" + tenant + "/nodejs"
	return cmd.Run()
}

func nodeRemove(tenant, name string) error {
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		return errors.New("Bad package name")
	}

	d := "/packages/" + tenant + "/nodejs/node_modules"
	_, err := os.Stat(d + "/" + name + "/package.json")
	if err != nil {
		return errors.New("Package not installed")
	}

	x, err := xh.DropDir(d, name)
	if err != nil {
		log.Errorf("Can't remove %s' sources %s (%s): %s", tenant, name, x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func rubyInfo() (string, []string, error) {
	v, err := exec.Command("ruby", "--version").Output()
	if err != nil {
		return "", nil, err
	}

	ps, err := exec.Command("gem", "list").Output()
	if err != nil {
		return "", nil, err
	}

	return string(v), xh.GetLines(ps), nil
}
