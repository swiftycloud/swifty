package main

import (
	"os"
	"fmt"
	"time"
	"flag"
	"bytes"
	"errors"
	"strings"
	"net/http"
	"swifty/apis"
	"swifty/common"
	"encoding/base64"
)

func fatal(err error) {
	fmt.Printf("==================[ FAIL ]=====================\n")
	fmt.Printf("ERROR: %s\n", err.Error())
	os.Exit(1)
}

func doRun(cln *swyapi.Client, id string, src *swyapi.FunctionSources, retrt string) {
	var res swyapi.WdogFunctionRunResult

	fmt.Printf("Running FN (custom code: %v)\n", src != nil)
	cln.Req1("POST", "functions/" + id + "/run", http.StatusOK,
			&swyapi.FunctionRun {
				Args: map[string]string { "name": "xyz" },
				Src: src,
			}, &res)

	fmt.Printf("\tRun result: %d.[%s]\n", res.Code, res.Return)

	if res.Code != 0 {
		fatal(errors.New("Code not 0"))
	}

	if res.Return != retrt {
		fatal(errors.New("Return not match"))
	}
}

func doWait(cln *swyapi.Client, id, version string) {
	fmt.Printf("Waiting FN to come up\n")
	cln.Req2("POST", "functions/" + id + "/wait",
			&swyapi.FunctionWait { Version: version, Timeout: 10000 },
			http.StatusOK, 300)
}

func runFunction(cln *swyapi.Client, prj string, lang string, ld *lDesc) error {
	var ifo swyapi.FunctionInfo

	fmt.Printf("---- Adding FN\n")
	cln.Functions().Add(&swyapi.FunctionAdd {
		Name:		"test.echo",
		Project:	prj,
		Code:		swyapi.FunctionCode { Lang: lang, },
	}, &ifo)

	doWait(cln, ifo.Id, "0")

	doRun(cln, ifo.Id, nil, ld.echo_result)

	fmt.Printf("---- Changing sources\n")
	var src swyapi.FunctionSources
	cln.Functions().Prop(ifo.Id, "sources", &src)
	data, err := base64.StdEncoding.DecodeString(src.Code)
	if err != nil {
		fatal(err)
	}
	data = bytes.Replace(data, []byte("world"), []byte(lang), 1)
	src.Code = base64.StdEncoding.EncodeToString(data)

	doRun(cln, ifo.Id, &src, strings.Replace(ld.echo_result, "world", lang, 1))

	fmt.Printf("---- Checking original again\n")
	doRun(cln, ifo.Id, nil, ld.echo_result)

	fmt.Printf("Removing echo FN\n")
	/* XXX -- k8s sometimes refuses to */
	time.Sleep(50 * time.Millisecond)
	cln.Functions().Del(ifo.Id)

	return nil
}

type lDesc struct {
	echo_result string
}

var langs = map[string]*lDesc {
	"golang": &lDesc{ echo_result: "\"Hello, world\"" },
	"python": &lDesc{ echo_result: "\"Hello, world\"" },
	"nodejs": &lDesc{},
	"ruby":   &lDesc{ echo_result: "\"Hello, world\"" },
	"swift":  &lDesc{ echo_result: "{\"msg\":\"Hello, world\"" },
	"csharp": &lDesc{ echo_result: "{\"msg\":\"Hello, world!\"}" },
}

func runFunctions(cln *swyapi.Client, prj, lang string) {
	if lang != "" {
		ld, ok := langs[lang]
		if !ok {
			return
		}

		runFunction(cln, prj, lang, ld)
	} else {
		for l, ld := range langs {
			fmt.Printf("=== Testing %s ===\n", l)
			runFunction(cln, prj, l, ld)
		}
	}

	fmt.Printf("==================[ PASS ]=====================\n")
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

func main() {
	var creds, pass, lang string
	var clnv bool
	flag.StringVar(&creds, "l", "", "Loging")
	flag.StringVar(&pass, "p", "", "Password (opt)")
	flag.StringVar(&lang, "L", "", "Language (opt)")
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
	runFunctions(cln, prj, lang)
}
