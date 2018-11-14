/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xsecret

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
	"os"
	"fmt"
	"errors"
)

type Store interface {
	Get(string) (string, error)
}

type FileSecrets map[string]string

func (fs FileSecrets)Get(name string) (string, error) {
	sv, ok := fs[name]
	if !ok {
		return "", errors.New("No such secret")
	}

	return sv, nil
}

type EnvSecrets struct {
	pfx	string
}

func (es EnvSecrets)Get(name string) (string, error) {
	v := os.Getenv(es.pfx + name)
	if v == "" {
		return "", errors.New("No such secret")
	}

	return v, nil
}

const secretDir string = ".swysecrets"

func Init(name string) (Store, error) {
	path, ok := os.LookupEnv("HOME")
	if !ok {
		return nil, errors.New("Can't find HOME dir")
	}

	path += "/" + secretDir
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			/* Likely, we will be provided with environment. */
			return &EnvSecrets{pfx: strings.ToUpper(name) + "_"}, nil
		}

		return nil, fmt.Errorf("Can't find secrets dir %s: %s", path, err.Error())
	}
	if st.Mode() & os.ModePerm != 0700 {
		return nil, fmt.Errorf("Secrets dir %s has unsafe perms (want 0700 got %#o)",
				path, st.Mode() & os.ModePerm)
	}

	path += "/" + name
	st, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			/* Likely, we will be provided with environment. */
			return &EnvSecrets{pfx: strings.ToUpper(name) + "_"}, nil
		}

		return nil, fmt.Errorf("Can't find secrets file %s: %s", path, err.Error())
	}
	if st.Mode() & os.ModePerm != 0600 {
		return nil, fmt.Errorf("Secrets file %s has unsafe perms (want 0600 got %#o)",
					path, st.Mode() & os.ModePerm)
	}

	secrets, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Can't read secrets %s: %s", path, err.Error())
	}

	var ret map[string]string

	err = yaml.Unmarshal(secrets, &ret)
	if err != nil {
		return nil, fmt.Errorf("Error parsing secrets %s: %s", path, err.Error())
	}

	return FileSecrets(ret), nil
}
