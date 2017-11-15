package main

import (
	"encoding/json"
	"encoding/base64"
	"path/filepath"
	"io/ioutil"
	"net/http"
	"strings"
	"bytes"
	"flag"
	"fmt"
	"os"

	"../apis/apps"
)

type login_info struct {
	Proj  string `json:"proj"`
	Host  string `json:"host"`
	Port  string `json:"port"`
	Token string `json:"token"`
	User  string `json:"user"`
	Pass  string `json:"pass"`
}

var cur_login login_info

func SafeEnv(env_name string, defaul_value string) string {
	v, found := os.LookupEnv(env_name)
	if found == false {
		return defaul_value
	}
	return v
}

func make_faas_req_x(url string, in interface{}) (*http.Response, error) {
	clnt := &http.Client{}

	body, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest("POST", "http://" + cur_login.Host + ":" + cur_login.Port + "/v1/" + url, bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json")
	if cur_login.Token != "" {
		req.Header.Set("X-Subject-Token", cur_login.Token)
	}

	return clnt.Do(req)
}

func faas_login() string {
	resp, err := make_faas_req_x("user/login", swyapi.UserLogin {
			UserName: cur_login.User, Password: cur_login.Pass,
		})
	if err != nil {
		panic(err)
	}

	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		panic("No auth token from server")
	}

	return token
}

func make_faas_req(url string, in interface{}, out interface{}) {
	first_attempt := true
again:
	resp, err := make_faas_req_x(url, in)
	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			first_attempt = false
			refresh_token("")
			goto again
		}

		panic(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	if out != nil {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		err = json.Unmarshal(body, out)
		if err != nil {
			panic(err)
		}
	}
}

func list_projects() {
	var ps []swyapi.ProjectItem
	make_faas_req("project/list", swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func list_functions(project string) {
	var fns []swyapi.FunctionItem
	make_faas_req("function/list", swyapi.FunctionList{ Project: project, }, &fns)

	fmt.Printf("%-20s%-10s\n", "NAME", "STATE")
	for _, fn := range fns {
		fmt.Printf("%-20s%-12s\n", fn.FuncName, fn.State)
	}
}

func info_function(project, name string) {
	var ifo swyapi.FunctionInfo
	make_faas_req("function/info", swyapi.FunctionID{ Project: project, FuncName: name}, &ifo)

	fmt.Printf("Lang:   %s\n", ifo.Script.Lang)
	fmt.Printf("Commit: %s\n", ifo.Commit[:8])
	fmt.Printf("State:  %s\n", ifo.State)
	if len(ifo.Mware) > 0 {
		fmt.Printf("Mware:  %s\n", strings.Join(ifo.Mware, ", "))
	}
	if ifo.Event.Source != "" {
		estr := ifo.Event.Source
		if ifo.Event.Source == "url" {
			/* nothing */
		} else if ifo.Event.CronTab != "" {
			estr += ":" + ifo.Event.CronTab
		} else if ifo.Event.MwareId != "" {
			estr += ":" + ifo.Event.MwareId
			if ifo.Event.MQueue != "" {
				estr += ":" + ifo.Event.MQueue
			}
		} else {
			estr += "UNKNOWN"
		}
		fmt.Printf("Event:  %s\n", estr)
	}
	if ifo.URL != "" {
		fmt.Printf("URL:    http://%s\n", ifo.URL)
	}
}

func detect_language(repo string) string {
	panic("can't detect function language")
}

func detect_script(repo string) string {
	panic("can't detect function script")
}

func add_function(name, lang, src, run, mwares, event string) {
	sources := swyapi.FunctionSources{}

	st, err := os.Stat(src)
	if err != nil {
		panic("Can't stat sources path")
	}

	if st.IsDir() {
		repo, err := filepath.Abs(src)
		if err != nil {
			panic("Can't get abs path for repo")
		}

		fmt.Printf("Will add git repo %s\n", repo)
		sources.Type = "git"
		sources.Repo = repo
	} else {
		data, err := ioutil.ReadFile(src)
		if err != nil {
			panic("Can't read file sources")
		}

		enc := base64.StdEncoding.EncodeToString(data)

		fmt.Printf("Will add file %s\n", src)
		sources.Type = "code"
		sources.Code = enc
		run = filepath.Base(src)
	}

	if lang == "auto" {
		lang = detect_language(src)
	}

	if run == "auto" {
		run = detect_script(src)
	}

	mw := []swyapi.MwareItem{}
	if mwares != "" {
		for _, mware := range strings.Split(mwares, ",") {
			mws := strings.SplitN(mware, ":", 2)
			mw = append(mw, swyapi.MwareItem{ Type: mws[0], ID: mws[1], })
		}
	}

	evt := swyapi.FunctionEvent {}
	if event != "" {
		mwe := strings.Split(event, ":")
		evt.Source = mwe[0]
		if evt.Source == "url" {
			/* nothing */
		} else if evt.Source == "mware" {
			evt.MwareId = mwe[1]
			evt.MQueue = mwe[2]
		} else {
			/* FIXME -- CRONTAB */
			panic("Unknown event string")
		}
	}

	make_faas_req("function/add",
		swyapi.FunctionAdd{
			Project: cur_login.Proj,
			FuncName: name,
			Sources: sources,
			Script: swyapi.FunctionScript {
				Lang: lang,
				Run: run,
			},
			Mware: mw,
			Event: evt,
		}, nil)

}

func run_function(name string, args []string) {
	var rres swyapi.FunctionRunResult
	make_faas_req("function/run",
		swyapi.FunctionRun{ Project: cur_login.Proj, FuncName: name, Args: args, }, &rres)

	fmt.Printf("code: %d\n", rres.Code);
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func update_function(name string) {
	make_faas_req("function/update",
		swyapi.FunctionUpdate{ Project: cur_login.Proj, FuncName: name, }, nil)

}

func del_function(name string) {
	make_faas_req("function/remove",
		swyapi.FunctionRemove{ Project: cur_login.Proj, FuncName: name }, nil)
}

func show_logs(name string) {
	var res []swyapi.FunctionLogEntry
	make_faas_req("function/logs",
		swyapi.FunctionID{ Project: cur_login.Proj, FuncName: name, }, &res)

	for _, le := range res {
		fmt.Printf("%s %s/%s: %s\n", le.Ts, le.Commit[:8], le.Event, le.Text)
	}
}

func list_mware(project string) {
	var mws []swyapi.MwareGetItem
	make_faas_req("mware/list", swyapi.MwareList{ Project: project, }, &mws)

	fmt.Printf("%-20s%-10s%s\n", "NAME", "ID", "OPTIONS")
	for _, mw := range mws {
		fmt.Printf("%-20s%-10s%s\n", mw.ID, mw.Type, mw.JSettings)
	}
}

func add_mware(mwares []string) {
	req := swyapi.MwareAdd { Project: cur_login.Proj }

	for _, mw := range mwares {
		mws := strings.SplitN(mw, ":", 2)
		fmt.Printf("Will add %s (%s) mware\n", mws[1], mws[0])
		req.Mware = append(req.Mware, swyapi.MwareItem {
					Type: mws[0],
					ID: mws[1],
				})
	}

	make_faas_req("mware/add", req, nil)
}

func del_mware(mwares []string) {
	make_faas_req("mware/remove",
		swyapi.MwareRemove{ Project: cur_login.Proj, MwareIDs: mwares, }, nil)
}

func show_mware_env(mwid string) {
	var r swyapi.MwareCinfoResp

	make_faas_req("mware/cinfo", swyapi.MwareCinfo { Project: cur_login.Proj, MwId: mwid }, &r)
	for _, e := range r.Envs {
		fmt.Printf("%s\n", e)
	}
}

func login() {
	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	data, err := ioutil.ReadFile(home + "/.swifty.conf")
	if err != nil {
		fmt.Printf("Login first\n")
		os.Exit(1)
	}

	err = json.Unmarshal(data, &cur_login)
	if err != nil {
		panic("Bad swifty.conf")
	}
}

func make_login(creds string) {
	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	/* Login string is user:pass@host:port/project */
	/* FIXME -- add user */
	a := strings.SplitN(creds, "@", 2) /* a = user:pass , host:port/project */
	b := strings.SplitN(a[1], "/", 2)  /* b = host:port , project */
	c := strings.SplitN(b[0], ":", 2)  /* c = host, port */
	d := strings.SplitN(a[0], ":", 2)  /* d = user, pass */

	cur_login.Host = c[0]
	cur_login.Port = c[1]
	cur_login.Proj = b[1]
	cur_login.User = d[0]
	cur_login.Pass = d[1]

	refresh_token(home)
}

func refresh_token(home string) {
	if home == "" {
		var found bool
		home, found = os.LookupEnv("HOME")
		if !found {
			panic("No HOME dir set")
		}
	}

	cur_login.Token = faas_login()

	data, err := json.Marshal(&cur_login)
	if err != nil {
		panic("Can't marshal login info")
	}

	err = ioutil.WriteFile(home + "/.swifty.conf", data, 0600)
	if err != nil {
		panic("Can't write swifty.conf")
	}
}

func main() {
	if len(os.Args) <= 1 {
		goto usage
	}

	if os.Args[1] == "login" {
		make_login(os.Args[2])
		return
	}

	login()

	if os.Args[1] == "ps" {
		list_projects()
		return
	}

	if os.Args[1] == "ls" {
		proj := cur_login.Proj
		if len(os.Args) > 2 {
			proj = os.Args[2]
		}
		list_functions(proj)
		return
	}

	if os.Args[1] == "inf" {
		var proj, fnam string

		if len(os.Args) > 3 {
			proj = os.Args[2]
			fnam = os.Args[3]
		} else if len(os.Args) > 2 {
			proj = cur_login.Proj
			fnam = os.Args[2]
		} else {
			goto usage
		}

		info_function(proj, fnam)
		return
	}

	if os.Args[1] == "add" {
		var lang, src, run, mware, event string

		flag.StringVar(&lang, "lang", "auto", "language")
		flag.StringVar(&src, "src", ".", "repository")
		flag.StringVar(&run, "run", "", "script to run")
		flag.StringVar(&mware, "mw", "", "mware to use, comma-separated")
		flag.StringVar(&event, "event", "", "event this fn is to start")
		flag.CommandLine.Parse(os.Args[3:])

		add_function(os.Args[2], lang, src, run, mware, event)
		return
	}

	if os.Args[1] == "run" {
		run_function(os.Args[2], os.Args[3:])
		return
	}

	if os.Args[1] == "upd" {
		update_function(os.Args[2])
		return
	}

	if os.Args[1] == "del" {
		del_function(os.Args[2])
		return
	}

	if os.Args[1] == "logs" {
		show_logs(os.Args[2])
		return
	}

	if os.Args[1] == "mls" {
		proj := cur_login.Proj
		if len(os.Args) > 2 {
			proj = os.Args[2]
		}
		list_mware(proj)
		return
	}

	if os.Args[1] == "madd" {
		add_mware(os.Args[2:])
		return
	}

	if os.Args[1] == "mdel" {
		del_mware(os.Args[2:])
		return
	}

	if os.Args[1] == "menv" {
		show_mware_env(os.Args[2])
		return
	}

usage:
	fmt.Printf("Actions:\n")
	fmt.Printf("\t\tlogin USER@HOST:PORT/PROJECT\n")
	fmt.Printf("\t\tps\n")
	fmt.Printf("\tOn functions:\n")
	fmt.Printf("\t\tls [PROJECT]\n")
	fmt.Printf("\t\tinf [PROJECT] NAME\n");
	fmt.Printf("\t\tadd NAME [-lang L] [-run S] [-src P] [-mw MW,...]\n")
	fmt.Printf("\t\trun NAME <args>\n")
	fmt.Printf("\t\tupd NAME\n")
	fmt.Printf("\t\tdel NAME\n")
	fmt.Printf("\t\tlogs NAME\n")
	fmt.Printf("\tOn middleware:\n")
	fmt.Printf("\t\tmls [PROJECT]\n")
	fmt.Printf("\t\tmadd TYPE:NAME ...\n")
	fmt.Printf("\t\tmdel NAME ...\n")
	fmt.Printf("\t\tmenv NAME\n")
}
