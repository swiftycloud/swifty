package swysec

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"fmt"
	"errors"
)

const secretDir string = ".swysecrets"

func ReadSecrets(name string) (map[string]string, error) {
	path, ok := os.LookupEnv("HOME")
	if !ok {
		return nil, errors.New("Can't find home dir")
	}

	path += "/" + secretDir
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("Can't find secrets dir: %s", err.Error())
	}
	if st.Mode() & os.ModePerm != 0700 {
		return nil, fmt.Errorf("Secrets dir has unsafe perms (%o want 0700)", st.Mode())
	}

	path += "/" + name
	st, err = os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("Can't find secrets dir: %s", err.Error())
	}
	if st.Mode() & os.ModePerm != 0600 {
		return nil, errors.New("Secrets file has unsafe perms (want 0600)")
	}

	secrets, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Can't read secrets: %s", err.Error())
	}

	var ret map[string]string

	err = yaml.Unmarshal(secrets, &ret)
	if err != nil {
		return nil, fmt.Errorf("Error parsing secrets: %s", err.Error())
	}

	return ret, nil
}
