package swy

import (
	"go.uber.org/zap"

	"gopkg.in/yaml.v2"
	"crypto/rand"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"errors"
	"regexp"
	"bytes"
	"time"
	"fmt"
	"os"
)

func MakeAdminURL(clienturl, admport string) string {
	return strings.Split(clienturl, ":")[0] + ":" + admport
}

var swylog = zap.NewNop().Sugar()

func InitLogger(logger *zap.SugaredLogger) {
	swylog = logger
}

var Letters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")

func Retry(callback func(interface{}) error, data interface{}, attempts int, sleep time.Duration) error {
	var err error

	for i := 0; i < attempts; i++ {
		err = callback(data)
		if err == nil {
			return nil
		} else {
			time.Sleep(sleep)
		}
	}
	return fmt.Errorf("Retry: %s", err.Error())
}

func Retry10(callback func(interface{}) error, data interface{}) error {
	return Retry(callback, data, 100, 100 * time.Millisecond)
}

func NameSymsAllowed(name string) bool {
	re := regexp.MustCompile("[^(a-z)(A-Z)(0-9)_]")
	return !re.MatchString(name)
}

func CheckName(name string, limit int) error {
	if name == "" {
		return errors.New("Empty name detected")
	}

	if NameSymsAllowed(name) == false {
		return fmt.Errorf("Name %s is not allowed", name)
	}

	if len(name) > limit {
		return fmt.Errorf("Name %s is too long (max %d allowed)",
					name, limit)
	}

	return nil
}

func GenRandId(length int) (string, error) {
	idx := make([]byte, length)
	pass:= make([]byte, length)
	_, err := rand.Read(idx)
	if err != nil {
		swylog.Errorf("Can't generate password: %s", err.Error())
		return "", err
	}

	for i, j := range idx {
		pass[i] = Letters[int(j) % len(Letters)]
	}

	return string(pass), nil
}

func SafeEnv(env_name string, defaul_value string) string {
	v, found := os.LookupEnv(env_name)
	if found == false {
		return defaul_value
	}
	return v
}

func ReadYamlConfig(path string, c interface{}) error {
	yamlFile, err := ioutil.ReadFile(path)
	if err == nil {
		err = yaml.Unmarshal(yamlFile, c)
		return err
	}
	return err
}

func WriteYamlConfig(path string, c interface{}) error {
	bytes, err := yaml.Marshal(c)
	if err == nil {
		return ioutil.WriteFile(path, bytes, 0600)
	}
	return err

}

// "ip:port" or ":port" expected
func GetIPPort(str string) (string, int32) {
	var port int = int(-1)
	var ip string = ""

	v := strings.Split(str, ":")
	if len(v) == 1 {
		port, _ = strconv.Atoi(v[0])
	} else if len(v) == 2 {
		port, _ = strconv.Atoi(v[1])
		ip = v[0]
	}
	return ip, int32(port)
}

func MakeIPPort(ip string, port int32) string {
	str := strconv.Itoa(int(port))
	return ip + ":" + str
}

func Exec(exe string, args []string) (bytes.Buffer, bytes.Buffer, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var cmd *exec.Cmd
	var err error

	cmd = exec.Command(exe, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return stdout, stderr, fmt.Errorf("runCmd: %s", err.Error())
	}

	return stdout, stderr, nil
}

func DropDir(dir, subdir string) error {
	_, err := os.Stat(dir + "/" + subdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("Can't stat %s%s: %s", dir, subdir, err.Error())
	}

	nname, err := ioutil.TempDir(dir, ".rm")
	if err != nil {
		return fmt.Errorf("leaking %s: %s", subdir, err.Error())
	}

	err = os.Rename(dir + "/" + subdir, nname + "/_" /* Why _ ? Why not...*/)
	if err != nil {
		return fmt.Errorf("can't move repo clone: %s", err.Error())
	}

	swylog.Debugf("Will remove %s/%s (via %s)", dir, subdir, nname)
	go func() {
		err = os.RemoveAll(nname)
		if err != nil {
			swylog.Errorf("can't remove %s (%s): %s", nname, subdir, err.Error())
		}
	}()

	return nil
}
