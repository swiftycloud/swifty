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
		return nil, errors.New("Can't find HOME dir")
	}

	path += "/" + secretDir
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("Can't find secrets dir %s: %s", path, err.Error())
	}
	if st.Mode() & os.ModePerm != 0700 {
		return nil, fmt.Errorf("Secrets dir %s has unsafe perms (want 0700 got %#o)",
				path, st.Mode() & os.ModePerm)
	}

	path += "/" + name
	st, err = os.Stat(path)
	if err != nil {
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

	return ret, nil
}
