package swy

import (
	"go.uber.org/zap"

	"gopkg.in/yaml.v2"
	"encoding/json"
	"crypto/rand"
	"io/ioutil"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"errors"
	"regexp"
	"bytes"
	"time"
	"fmt"
	"os"
	"io"
)

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

// Function names, middleware databes tables names
// must be limited with @MaxNameLengthUsr and match
// NameSymsAllowed.
const MaxNameLengthUsr int = 50

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

func CheckFunName(name string) error {
	return CheckName(name, MaxNameLengthUsr)
}

func ProjectSymsAllowed(proj string) bool {
	re := regexp.MustCompile("[^(a-z)(A-Z)(0-9)_]")
	return !re.MatchString(proj)
}

func CheckProjectId(project string) error {
	if project == "" {
		return errors.New("Empty Project detected")
	}

	if ProjectSymsAllowed(project) == false {
		return fmt.Errorf("Project %s is not allowed", project)
	}

	if len(project) > 64 {
		return fmt.Errorf("Project %s is too long (max 64 allowed)", project)
	}

	return nil
}

func ValidateProjectAndFuncName(project string, funcname string) error {
	var err error

	err = CheckProjectId(project)
	if err == nil {
		err = CheckFunName(funcname)
	}

	return err
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

func HTTPReadAndUnmarshalReq(r *http.Request, data interface{}) error {
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		swylog.Errorf("\tCan't parse request: %s", err.Error())
		return err
	}

	err = json.Unmarshal(body, data)
	if err != nil {
		swylog.Errorf("\tUnmarshal error: %s", err.Error())
		return err
	}

	return nil
}

func HTTPReadAndUnmarshalResp(r *http.Response, data interface{}) error {
	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		swylog.Errorf("\tCan't parse request: %s", err.Error())
		return err
	}

	err = json.Unmarshal(body, data)
	if err != nil {
		swylog.Errorf("\tUnmarshal error: %s", err.Error())
		return err
	}

	return nil
}

func HTTPMarshalAndWrite(w http.ResponseWriter, data interface{}) error {
	jdata, err := json.Marshal(data)
	if err != nil {
		swylog.Errorf("\tMarshal error: %s", err.Error())
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(jdata)

	return nil
}

type RestReq struct {
	Method		string
	Address		string
	Timeout		time.Duration
	Headers		map[string]string
	Success		int
}

func HTTPMarshalAndPost(req *RestReq, data interface{}) (*http.Response, error) {
	if req.Timeout == 0 {
		req.Timeout = 15
	}

	var c = &http.Client{
		Timeout: time.Second * req.Timeout,
	}

	var req_body io.Reader

	if data != nil {
		jdata, err := json.Marshal(data)
		if err != nil {
			swylog.Errorf("\tMarshal error: %s", err.Error())
			return nil, err
		}

		req_body = bytes.NewBuffer(jdata)
	}

	if req.Method == "" {
		req.Method = "POST"
	}

	r, err := http.NewRequest(req.Method, req.Address, req_body)
	if err != nil {
		swylog.Errorf("\thttp.NewRequest error: %s", err.Error())
		return nil, err
	}

	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	for hk, hv := range req.Headers {
		r.Header.Set(hk, hv)
	}

	rsp, err := c.Do(r)
	if err != nil {
		swylog.Errorf("\thttp.Do() error %s", err.Error())
		return nil, err
	}

	if req.Success == 0 {
		req.Success = http.StatusOK
	}

	if rsp.StatusCode != req.Success {
		err = fmt.Errorf("\tResponse is not OK: %d", rsp.StatusCode)
		return rsp, err
	}

	return rsp, nil
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

func DropDir(dir, subdir string) {
	nname, err := ioutil.TempDir(dir, ".rm")
	if err != nil {
		swylog.Errorf("leaking %s: %s", subdir, err.Error())
		return
	}

	err = os.Rename(dir + "/" + subdir, nname + "/_" /* Why _ ? Why not...*/)
	if err != nil {
		swylog.Errorf("can't move repo clone: %s", err.Error())
		return
	}

	swylog.Debugf("Will remove %s/%s (via %s)", dir, subdir, nname)
	go func() {
		err = os.RemoveAll(nname)
		if err != nil {
			swylog.Errorf("can't remove %s (%s): %s", nname, subdir, err.Error())
		}
	}()
}
