package swy

import (
	"gopkg.in/yaml.v2"
	"crypto/rand"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"bytes"
	"time"
	"net"
	"fmt"
	"os"
)

func MakeAdminURL(clienturl, admport string) string {
	return strings.Split(clienturl, ":")[0] + ":" + admport
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

func GenRandId(length int) (string, error) {
	idx := make([]byte, length)
	pass:= make([]byte, length)
	_, err := rand.Read(idx)
	if err != nil {
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

type XCreds struct {
	User    string
	Pass    string
	Host    string
	Port    string
	Domn	string
}

func (xc *XCreds)Addr() string {
	return xc.Host + ":" + xc.Port
}

func (xc *XCreds)AddrP(port string) string {
	return xc.Host + ":" + port
}

func (xc *XCreds)URL() string {
	s := xc.User + ":" + xc.Pass + "@" + xc.Host + ":" + xc.Port
	if xc.Domn != "" {
		s += "/" + xc.Domn
	}
	return s
}

func (xc *XCreds)Resolve() {
	if net.ParseIP(xc.Host) == nil {
		ips, err := net.LookupIP(xc.Host)
		if err == nil && len(ips) > 0 {
			xc.Host = ips[0].String()
		}
	}
}

func ParseXCreds(url string) *XCreds {
	xc := &XCreds{}
	/* user:pass@host:port */
	x := strings.SplitN(url, ":", 2)
	xc.User = x[0]
	x = strings.SplitN(x[1], "@", 2)
	xc.Pass = x[0]
	x = strings.SplitN(x[1], ":", 2)
	xc.Host = x[0]
	x = strings.SplitN(x[1], "/", 2)
	xc.Port = x[0]
	if len(x) > 1 {
		xc.Domn = x[1]
	}

	return xc
}
