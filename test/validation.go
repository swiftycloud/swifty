package main

import (
	"swifty/apis"
	"swifty/common"
	"os"
	"fmt"
	"time"
	"errors"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"encoding/base64"
)

func fatal(err error) {
	fmt.Printf("ERROR: %s\n", err.Error())
	os.Exit(1)
}

func encodeFile(file string) string {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		fatal(fmt.Errorf("Can't read file sources: %s", err.Error()))
	}

	return base64.StdEncoding.EncodeToString(data)
}

func doRun(cln *swyapi.Client, id string, src *swyapi.FunctionSources) (string, error) {
	var res swyapi.WdogFunctionRunResult

	fmt.Printf("Running FN (custom code: %v)\n", src != nil)
	err := cln.Req1("POST", "functions/" + id + "/run", http.StatusOK,
			&swyapi.WdogFunctionRun {
				Args: map[string]string { "name": "xyz" },
				Src: src,
			}, &res)
	if err != nil {
		return "", err
	}

	fmt.Printf("\tRun result: [%s]\n", res.Return)
	var resd map[string]string
	err = json.Unmarshal([]byte(res.Return), &resd)
	if err != nil {
		return "", err
	}

	return resd["message"], nil
}

func doWait(cln *swyapi.Client, id, version string) error {
	_, err := cln.Req2("POST", "functions/" + id + "/wait",
			&swyapi.FunctionWait { Version: version, Timeout: 10000 },
			http.StatusOK, 300)
	return err
}

var stdrep string

func runRepos(cln *swyapi.Client, prj string) error {
	fmt.Printf("Adding GH account\n")
	var ai map[string]string
	err := cln.Req1("POST", "accounts", http.StatusOK, map[string]string {
				"name": "swiftycloud",
				"type": "github",
			}, &ai)
	if err != nil {
		return err
	}

	fmt.Printf("Acc %s created\n", ai["id"])
	fmt.Printf("Listing repos\n")

	var reps []*swyapi.RepoInfo

	err = cln.Req1("GET", "repos", http.StatusOK, nil, &reps)
	if err != nil  {
		return err
	}

	for _, rep := range reps {
		fmt.Printf("%s\n", rep.URL)
		if rep.URL == "https://github.com/swiftycloud/swifty.demo" {
			if stdrep != "" {
				return errors.New("Duplicate std repo")
			}
			stdrep = rep.Id
		}
	}

	fmt.Printf("Found stdrepo %s\n", stdrep)
	cln.Req1("DELETE", "accounts/" + ai["id"], http.StatusOK, nil, nil)

	return nil
}

func runFunctions(cln *swyapi.Client, prj string) error {
	var err error
	var ifo swyapi.FunctionInfo

	src1 := swyapi.FunctionSources{}
	if stdrep == "" {
		fmt.Printf("Using \"code\" source1\n")
		src1.Type = "code"
		src1.Code = encodeFile("test/functions/python/helloworld.py")
	} else {
		fmt.Printf("Using \"git\" source1\n")
		src1.Type = "git"
		src1.Repo = stdrep + "/functions/python/helloworld.py"
	}

	fmt.Printf("Adding echo FN\n")
	err = cln.Req1("POST", "functions", http.StatusOK, &swyapi.FunctionAdd {
		Name:		"test.echo",
		Project:	prj,
		Code:		swyapi.FunctionCode {
			Lang:		"python",
		},
		Sources:	src1,
	}, &ifo)
	if err != nil {
		return err
	}

	fmt.Printf("Waiting FN to come up\n")
	err = doWait(cln, ifo.Id, "0")
	if err != nil {
		return err
	}

	src2 := &swyapi.FunctionSources {
		Type:	"code",
		Code:	encodeFile("test/functions/python/helloworld2.py"),
	}

	_, err = doRun(cln, ifo.Id, src2)
	if err != nil {
		return err
	}

	_, err = doRun(cln, ifo.Id, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Updating FN src\n")
	err = cln.Req1("PUT", "functions/" + ifo.Id + "/sources", http.StatusOK, src2, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Waiting FN to update\n")
	err = doWait(cln, ifo.Id, "1")
	if err != nil {
		return err
	}

	_, err = doRun(cln, ifo.Id, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Removing echo FN\n")
	err = cln.Req1("DELETE", "functions/" + ifo.Id, http.StatusOK, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func runAaaS(cln *swyapi.Client, prj string) error {
	var err error
	var di swyapi.DeployInfo

	fmt.Printf("Turning on AaaS\n")
	err = cln.Req1("POST", "auths", http.StatusOK, &swyapi.AuthAdd { Name: "test", Project: prj }, &di)
	if err != nil {
		return err
	}

again:
	fmt.Printf("Getting deloy info\n")
	err = cln.Req1("GET", "auths/" + di.Id + "?details=1", http.StatusOK, nil, &di)
	if err != nil {
		return err
	}

	for _, item := range di.Items {
		if item.Type != "function" {
			continue
		}
		if item.State == "dead" {
			/* FIXME -- deploy starts item after some time, not
			 * immediately and till that it appears as "dead"
			 */
			time.Sleep(10 * time.Millisecond)
			goto again
		}

		fmt.Printf("Waiting fn %s (%s) to come up\n", item.Name, item.Id)
		err = doWait(cln, item.Id, "0")
		if err != nil {
			return err
		}
	}

	fmt.Printf("Removing AaaS\n")
	err = cln.Req1("DELETE", "auths/" + di.Id, http.StatusOK, nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func mkClient() (*swyapi.Client, string) {
	login := xh.ParseXCreds(os.Args[2])
	fmt.Printf("Will test %s@%s:%s (project %s)\n", login.User, login.Host, login.Port, login.Domn)

	swyclient := swyapi.MakeClient(login.User, login.Pass, login.Host, login.Port)
	swyclient.NoTLS()
	swyclient.Direct()
	swyclient.Verbose()

	return swyclient, login.Domn
}

type test struct {
	name	string
	run	func(*swyapi.Client, string) error
}

var tests = []*test {
	{ name: "repos",	run: runRepos },
	{ name: "functions",	run: runFunctions },
	{ name: "aaas",		run: runAaaS },
}

func main() {
	fmt.Printf("Run %s tests on %s\n", os.Args[1], os.Args[2])
	cln, prj := mkClient()
	for _, t := range tests {
		if os.Args[1] != "*" && os.Args[1] != t.name {
			continue
		}

		fmt.Printf("==========================[ %s ]========================\n", t.name)
		err := t.run(cln, prj)
		if err != nil {
			fmt.Printf("Error running test: %s\n", err.Error())
			fmt.Printf("==================[ FAIL ]=====================\n")
			break
		}

		fmt.Printf("==================[ PASS ]=====================\n")
	}
}
