package xh

import (
	"path/filepath"
	"io/ioutil"
	"syscall"
	"strings"
	"fmt"
	"os"
)

func GetDirDU(dir string) (uint64, error) {
	var size uint64

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		stat, _ := info.Sys().(*syscall.Stat_t)
		size += uint64(stat.Blocks << 9)
		return nil
	})

	return size, err
}

func DropDir(dir, subdir string) (string, error) {
	nn, err := DropDirPrep(dir, subdir)
	if err != nil {
		return "", err
	}

	DropDirComplete(nn)
	return nn, nil
}

func DropDirPrep(dir, subdir string) (string, error) {
	_, err := os.Stat(dir + "/" + subdir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("Can't stat %s%s: %s", dir, subdir, err.Error())
	}

	nname, err := ioutil.TempDir(dir, ".rm")
	if err != nil {
		return "", fmt.Errorf("leaking %s: %s", subdir, err.Error())
	}

	err = os.Rename(dir + "/" + subdir, nname + "/" + strings.Replace(subdir, "/", "_", -1))
	if err != nil {
		return "", fmt.Errorf("can't move repo clone: %s", err.Error())
	}

	return nname, nil
}

func DropDirComplete(nname string) {
	go os.RemoveAll(nname)
}
