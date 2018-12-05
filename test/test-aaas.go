package main

import (
	"os"
	"fmt"
	"time"
	"flag"
	"errors"
	"net/http"
	"io/ioutil"
	"swifty/apis"
	"swifty/common"
	"encoding/json"
)

func fatal(err error) {
	fmt.Printf("==================[ FAIL ]=====================\n")
	fmt.Printf("ERROR: %s\n", err.Error())
	os.Exit(1)
}

func mkClient(creds, pass string) (*swyapi.Client, string) {
	login := xh.ParseXCreds(creds)
	if pass != "" {
		login.Pass = pass
	}

	fmt.Printf("Will test %s@%s:%s (project %s)\n", login.User, login.Host, login.Port, login.Domn)

	swyclient := swyapi.MakeClient(login.User, login.Pass, login.Host, login.Port)
	swyclient.NoTLS()
	swyclient.Direct()
	swyclient.OnError(fatal)

	return swyclient, login.Domn
}

func testAuth(url string) {
	fmt.Printf("------------ Checking aaas.base ----------------\n")
	r, err := http.Get(url + "/signup?userid=abc&password=ABC")
	if err != nil {
		fatal(err)
	}

	r, err = http.Get(url + "/signin?userid=abc&password=ABC")
	if err != nil {
		fatal(err)
	}

	defer r.Body.Close()
	dat, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Signed in: [%s]\n", string(dat))

	var adat map[string]string
	err = json.Unmarshal(dat, &adat)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Token: %s\n", adat["token"])
}

func doWait(cln *swyapi.Client, id, version string) {
	fmt.Printf("Waiting FN to come up\n")
	cln.Req2("POST", "functions/" + id + "/wait",
			&swyapi.FunctionWait { Version: version, Timeout: 10000 },
			http.StatusOK, 300)
}

func waitAaaas(cln *swyapi.Client, id string) string {
	for i := 0; i < 16; i++ {
		var di swyapi.DeployInfo
		cln.Get("auths/" + id, http.StatusOK, &di)

		for _, it := range di.Items {
			fmt.Printf("%v\n", it)
			if it.Type == "function" && it.Name == "test.base" && it.State != "dead" {
				doWait(cln, it.Id, "0")

				var ifo swyapi.FunctionInfo
				cln.Functions().Get(it.Id, &ifo)
				if ifo.URL == "" {
					fatal(errors.New("URL is absent for .base auth fn"))
				}
				return ifo.URL
			}
		}

		/* FIXME -- no way to wait for needed fns reliably */
		time.Sleep(time.Second)

	}

	fatal(errors.New("Base FN not started for too long"))
	return ""
}

func testAaaas(cln *swyapi.Client, prj string) {
	var di swyapi.DeployInfo
	cln.Add("auths", http.StatusOK, &swyapi.AuthAdd { Name: "test" }, &di)
	fmt.Printf("Created %s auth\n", di.Id)

	url := waitAaaas(cln, di.Id)
	fmt.Printf("AUTH URL is %s\n", url)

	testAuth(url)

	fmt.Printf("Removing auth\n")
	/* XXX -- k8s sometimes refuses to */
	time.Sleep(50 * time.Millisecond)
	cln.Del("auths/" + di.Id, http.StatusOK)

	fmt.Printf("==================[ PASS ]=====================\n")
}

func main() {
	var creds, pass string
	var clnv bool

	flag.StringVar(&creds, "l", "", "Loging")
	flag.StringVar(&pass, "p", "", "Password (opt)")
	flag.BoolVar(&clnv, "v",  false, "Verbose client (opt)")
	flag.Parse()

	if creds == "" {
		flag.Usage()
		return
	}

	cln, prj := mkClient(creds, pass)
	if clnv {
		cln.Verbose()
	}

	testAaaas(cln, prj)
}
