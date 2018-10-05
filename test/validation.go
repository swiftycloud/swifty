package main

import (
	"../src/apis"
	"../src/common"
	"os"
	"fmt"
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
	var res swyapi.SwdFunctionRunResult

	fmt.Printf("Running FN (custom code: %v)\n", src != nil)
	err := cln.Req1("POST", "functions/" + id + "/run", http.StatusOK,
			&swyapi.SwdFunctionRun {
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

func runFunctions(cln *swyapi.Client, prj string) error {
	var err error
	var ifo swyapi.FunctionInfo

	fmt.Printf("Adding echo FN\n")
	err = cln.Req1("POST", "functions", http.StatusOK, &swyapi.FunctionAdd {
		Name:		"test.echo",
		Project:	prj,
		Code:		swyapi.FunctionCode {
			Lang:		"python",
		},
		Sources:	swyapi.FunctionSources {
			Type:	"code",
			Code:	encodeFile("functions/python/helloworld.py"),
		},
	}, &ifo)
	if err != nil {
		return err
	}

	fmt.Printf("Waiting FN to come up\n")
	_, err = cln.Req2("POST", "functions/" + ifo.Id + "/wait",
			&swyapi.FunctionWait { Version: "0", Timeout: 10000 },
			http.StatusOK, 300)
	if err != nil {
		return err
	}

	src2 := &swyapi.FunctionSources {
		Type:	"code",
		Code:	encodeFile("functions/python/helloworld2.py"),
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
	_, err = cln.Req2("POST", "functions/" + ifo.Id + "/wait",
			&swyapi.FunctionWait { Version: "1", Timeout: 10000 },
			http.StatusOK, 300)
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

func mkClient() (*swyapi.Client, string) {
	login := xh.ParseXCreds(os.Args[1])
	fmt.Printf("Will test %s@%s:%s (project %s)\n", login.User, login.Host, login.Port, login.Domn)

	swyclient := swyapi.MakeClient(login.User, login.Pass, login.Host, login.Port)
	swyclient.NoTLS()
	swyclient.Direct()

	return swyclient, login.Domn
}

type test struct {
	name	string
	run	func(*swyapi.Client, string) error
}

var tests = []*test {
	{ name: "functions",	run: runFunctions },
}

func main() {
	cln, prj := mkClient()
	for _, t := range tests {
		fmt.Printf("==========================[ %s ]========================\n", t.name)
		err := t.run(cln, prj)
		if err != nil {
			fmt.Printf("==================[ FAIL ]=====================\n")
			break
		}

		fmt.Printf("==================[ PASS ]=====================\n")
	}
}
