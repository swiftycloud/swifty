/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xh

import (
	"crypto/sha256"
	"encoding/hex"
	"gopkg.in/yaml.v2"
	"crypto/rand"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"bytes"
	"fmt"
	"os"
)

func MakeEndpoint(addr string) string {
	if !(strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://")) {
		addr = "https://" + addr
	}
	return addr
}

func Sha256sum(s []byte) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func Cookify(val string) string {
	h := sha256.New()
	h.Write([]byte(val))
	return hex.EncodeToString(h.Sum(nil))
}

func CookifyS(vals ...string) string {
	h := sha256.New()
	for _, v := range vals {
		h.Write([]byte(v + "::"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var Letters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")

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
	}
	return err
}

func WriteYamlConfig(path string, c interface{}) error {
	bytes, err := yaml.Marshal(c)
	if err == nil {
		err = ioutil.WriteFile(path, bytes, 0600)
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

func Fortune() string {
	var fort []byte
	fort, err := exec.Command("fortune", "fortunes").Output()
	if err == nil {
		return string(fort)
	} else {
		return ""
	}
}

func GetLines(data []byte) []string {
        sout := strings.TrimSpace(string(data))
        return strings.Split(sout, "\n")
}
