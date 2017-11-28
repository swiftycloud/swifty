package main

import (
	"encoding/json"
	"encoding/base64"
	"path/filepath"
	"io/ioutil"
	"net/http"
	"strings"
	"flag"
	"fmt"
	"os"

	"../common"
	"../apis/apps"
)

type LoginInfo struct {
	Host		string		`yaml:"host"`
	Port		string		`yaml:"port"`
	Token		string		`yaml:"token"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"pass"`
}

type YAMLConf struct {
	Login		LoginInfo	`yaml:"login"`
}

var conf YAMLConf

func make_faas_req_x(url string, in interface{}, succ_code int) (*http.Response, error) {
	var address string = "http://" + conf.Login.Host + ":" + conf.Login.Port + "/v1/" + url
	var cb swy.HTTPMarshalAndPostCB = func(r *http.Request) error {
			if conf.Login.Token != "" {
				r.Header.Set("X-Auth-Token",
						conf.Login.Token)
			}
			return nil
	}
	return swy.HTTPMarshalAndPost2(address, in, cb, succ_code)
}

func faas_login() string {
	resp, err := make_faas_req_x("login", swyapi.UserLogin {
			UserName: conf.Login.User, Password: conf.Login.Pass,
		}, http.StatusOK)
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
	make_faas_req2(url, in, out, http.StatusOK)
}

func make_faas_req2(url string, in interface{}, out interface{}, succ_code int) {
	first_attempt := true
again:
	resp, err := make_faas_req_x(url, in, succ_code)
	if err != nil {
		if resp == nil {
			panic(err)
		}

		resp.Body.Close()
		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			first_attempt = false
			refresh_token("")
			goto again
		}

		panic(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	/* Here we have http.StatusOK */
	defer resp.Body.Close()

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

func list_users() {
	var uss []swyapi.UserInfo
	make_faas_req("users", swyapi.ListUsers{}, &uss)

	for _, u := range uss {
		fmt.Printf("%s (%s)\n", u.Id, u.Name)
	}
}

func add_user(id, name, pass string) {
	make_faas_req2("adduser", swyapi.AddUser{Id: id, Pass:pass, Name: name},
		nil, http.StatusCreated)
}

func del_user(id string) {
	make_faas_req2("deluser", swyapi.UserInfo{Id: id}, nil, http.StatusNoContent)
}

func set_password(id, pass string) {
	make_faas_req2("setpass", swyapi.UserLogin{UserName: id, Password: pass},
		nil, http.StatusCreated)
}

func show_user_info(id string) {
	var ui swyapi.UserInfo
	make_faas_req("userinfo", swyapi.UserInfo{Id: id}, &ui)
	fmt.Printf("Name: %s\n", ui.Name)
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

	fmt.Printf("Lang:   %s\n", ifo.Code.Lang)
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
		fmt.Printf("URL:    http://%s:%s%s\n", conf.Login.Host, conf.Login.Port, ifo.URL)
	}
	fmt.Printf("Called: %d\n", ifo.Stats.Called)
}

func detect_language(repo string) string {
	panic("can't detect function language")
}

func add_function(project, name, lang, src, mwares, event string) {
	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

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
	}

	if lang == "auto" {
		lang = detect_language(src)
	}

	code.Lang = lang


	mw := strings.Split(mwares, ",")

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
			Project: project,
			FuncName: name,
			Sources: sources,
			Code: code,
			Mware: mw,
			Event: evt,
		}, nil)

}

func run_function(project, name string, args []string) {
	var rres swyapi.FunctionRunResult
	make_faas_req("function/run",
		swyapi.FunctionRun{ Project: project, FuncName: name, Args: args, }, &rres)

	fmt.Printf("code: %d\n", rres.Code)
	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func update_function(project, name string) {
	make_faas_req("function/update",
		swyapi.FunctionUpdate{ Project: project, FuncName: name, }, nil)

}

func del_function(project, name string) {
	make_faas_req("function/remove",
		swyapi.FunctionRemove{ Project: project, FuncName: name }, nil)
}

func show_logs(project, name string) {
	var res []swyapi.FunctionLogEntry
	make_faas_req("function/logs",
		swyapi.FunctionID{ Project: project, FuncName: name, }, &res)

	for _, le := range res {
		fmt.Printf("%s %s/%s: %s\n", le.Ts, le.Commit[:8], le.Event, le.Text)
	}
}

func list_mware(project string) {
	var mws []swyapi.MwareItem
	make_faas_req("mware/list", swyapi.MwareList{ Project: project, }, &mws)

	fmt.Printf("%-20s%-10s%s\n", "NAME", "ID", "OPTIONS")
	for _, mw := range mws {
		fmt.Printf("%-20s%-10s%s\n", mw.ID, mw.Type, mw.JSettings)
	}
}

func add_mware(project, id, typ string) {
	req := swyapi.MwareAdd { Project: project, ID: id, Type: typ }
	make_faas_req("mware/add", req, nil)
}

func del_mware(project, id string) {
	make_faas_req("mware/remove",
		swyapi.MwareRemove{ Project: project, ID: id, }, nil)
}

func show_mware_env(project, mwid string) {
	var r swyapi.MwareCinfoResp

	make_faas_req("mware/cinfo", swyapi.MwareCinfo { Project: project, MwId: mwid }, &r)
	for _, e := range r.Envs {
		fmt.Printf("%s\n", e)
	}
}

func login() {
	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	err := swy.ReadYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		fmt.Printf("Login first\n")
		os.Exit(1)
	}
}

func make_login(creds string) {
	//
	// Login string is user:pass@host:port
	//
	// swifty.user:swifty@10.94.96.216:8686
	//
	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	x := strings.SplitN(creds, ":", 2)
	conf.Login.User = x[0]
	x = strings.SplitN(x[1], "@", 2)
	conf.Login.Pass = x[0]
	x = strings.SplitN(x[1], ":", 2)
	conf.Login.Host = x[0]
	conf.Login.Port = x[1]

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

	conf.Login.Token = faas_login()

	err := swy.WriteYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		panic("Can't write swifty.conf")
	}
}

const (
	CMD_LOGIN string	= "login"
	CMD_PS string		= "ps"
	CMD_LS string		= "ls"
	CMD_INF string		= "inf"
	CMD_ADD string		= "add"
	CMD_RUN string		= "run"
	CMD_UPD string		= "upd"
	CMD_DEL string		= "del"
	CMD_LOGS string		= "logs"
	CMD_MLS string		= "mls"
	CMD_MADD string		= "madd"
	CMD_MDEL string		= "mdel"
	CMD_MENV string		= "menv"
	CMD_LUSR string		= "uls"
	CMD_UADD string		= "uadd"
	CMD_UDEL string		= "udel"
	CMD_PASS string		= "pass"
	CMD_UINF string		= "uinf"
)

var cmdOrder = []string {
	CMD_LOGIN,
	CMD_PS,
	CMD_LS,
	CMD_INF,
	CMD_ADD,
	CMD_RUN,
	CMD_UPD,
	CMD_DEL,
	CMD_LOGS,
	CMD_MLS,
	CMD_MADD,
	CMD_MDEL,
	CMD_MENV,
	CMD_LUSR,
	CMD_UADD,
	CMD_UDEL,
	CMD_PASS,
	CMD_UINF,
}

type cmdDesc struct {
	opts	*flag.FlagSet
	pargs	[]string
	project	string
}

var cmdMap = map[string]*cmdDesc {
	CMD_LOGIN:	&cmdDesc{ opts: flag.NewFlagSet(CMD_LOGIN, flag.ExitOnError) },
	CMD_PS:		&cmdDesc{ opts: flag.NewFlagSet(CMD_PS, flag.ExitOnError) },
	CMD_LS:		&cmdDesc{ opts: flag.NewFlagSet(CMD_LS, flag.ExitOnError) },
	CMD_INF:	&cmdDesc{ opts: flag.NewFlagSet(CMD_INF, flag.ExitOnError) },
	CMD_ADD:	&cmdDesc{ opts: flag.NewFlagSet(CMD_ADD, flag.ExitOnError) },
	CMD_RUN:	&cmdDesc{ opts: flag.NewFlagSet(CMD_RUN, flag.ExitOnError) },
	CMD_UPD:	&cmdDesc{ opts: flag.NewFlagSet(CMD_UPD, flag.ExitOnError) },
	CMD_DEL:	&cmdDesc{ opts: flag.NewFlagSet(CMD_DEL, flag.ExitOnError) },
	CMD_LOGS:	&cmdDesc{ opts: flag.NewFlagSet(CMD_LOGS, flag.ExitOnError) },
	CMD_MLS:	&cmdDesc{ opts: flag.NewFlagSet(CMD_MLS, flag.ExitOnError) },
	CMD_MADD:	&cmdDesc{ opts: flag.NewFlagSet(CMD_MADD, flag.ExitOnError) },
	CMD_MDEL:	&cmdDesc{ opts: flag.NewFlagSet(CMD_MDEL, flag.ExitOnError) },
	CMD_MENV:	&cmdDesc{ opts: flag.NewFlagSet(CMD_MENV, flag.ExitOnError) },
	CMD_LUSR:	&cmdDesc{ opts: flag.NewFlagSet(CMD_LUSR, flag.ExitOnError) },
	CMD_UADD:	&cmdDesc{ opts: flag.NewFlagSet(CMD_UADD, flag.ExitOnError) },
	CMD_UDEL:	&cmdDesc{ opts: flag.NewFlagSet(CMD_UDEL, flag.ExitOnError) },
	CMD_PASS:	&cmdDesc{ opts: flag.NewFlagSet(CMD_PASS, flag.ExitOnError) },
	CMD_UINF:	&cmdDesc{ opts: flag.NewFlagSet(CMD_UINF, flag.ExitOnError) },
}

func bindCmdUsage(cmd string, args []string, help string, wp bool) {
	cd := cmdMap[cmd]
	if wp {
		cd.opts.StringVar(&cd.project, "proj", "", "Project to work on")
	}

	cd.pargs = args
	cd.opts.Usage = func() {
		var astr string
		if len(args) != 0 {
			astr = " <" + strings.Join(args, "> <") + ">"
		}
		fmt.Fprintf(os.Stderr, "%s%s\n\t%s\n", cmd, astr, help)
		cd.opts.PrintDefaults()
	}
}

func main() {
	var lang, src, mware, event, name, pass string

	bindCmdUsage(CMD_LOGIN,	[]string{"USER:PASS@HOST:PORT"}, "Login into the system", false)

	bindCmdUsage(CMD_PS,	[]string{}, "List projects", false)

	bindCmdUsage(CMD_LS,	[]string{}, "List functions", true)
	bindCmdUsage(CMD_INF,	[]string{"NAME"}, "Function info", true)
	cmdMap[CMD_ADD].opts.StringVar(&lang, "lang", "auto", "Language")
	cmdMap[CMD_ADD].opts.StringVar(&src, "src", ".", "Repository")
	cmdMap[CMD_ADD].opts.StringVar(&mware, "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_ADD].opts.StringVar(&event, "event", "", "Event this fn is to start")
	bindCmdUsage(CMD_ADD,	[]string{"NAME"}, "Add a function", true)
	bindCmdUsage(CMD_RUN,	[]string{"NAME", "ARGUMENTS..."}, "Run a function", true)
	bindCmdUsage(CMD_UPD,	[]string{"NAME"}, "Update a function", true)
	bindCmdUsage(CMD_DEL,	[]string{"NAME"}, "Delete a function", true)
	bindCmdUsage(CMD_LOGS,	[]string{"NAME"}, "Show function logs", true)

	bindCmdUsage(CMD_MLS,	[]string{}, "List middleware", true)
	bindCmdUsage(CMD_MADD,	[]string{"ID", "TYPE"}, "Add middleware", true)
	bindCmdUsage(CMD_MDEL,	[]string{"ID"}, "Delete middleware", true)
	bindCmdUsage(CMD_MENV,	[]string{"ID"}, "Show middleware environment variables", true)

	bindCmdUsage(CMD_LUSR,	[]string{}, "List users", false)
	cmdMap[CMD_UADD].opts.StringVar(&name, "name", "", "User name")
	cmdMap[CMD_UADD].opts.StringVar(&pass, "pass", "", "User password")
	bindCmdUsage(CMD_UADD,	[]string{"UID"}, "Add user", false)
	bindCmdUsage(CMD_UDEL,	[]string{"UID"}, "Del user", false)
	cmdMap[CMD_PASS].opts.StringVar(&pass, "pass", "", "New password")
	bindCmdUsage(CMD_PASS,	[]string{"UID"}, "Set password", false)
	bindCmdUsage(CMD_UINF,	[]string{"UID"}, "Get user info", false)

	flag.Usage = func() {
		for _, v := range cmdOrder {
			cmdMap[v].opts.Usage()
		}
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	cd, ok := cmdMap[os.Args[1]]
	if !ok {
		flag.Usage()
		os.Exit(1)
	}

	npa := len(cd.pargs) + 2
	cd.opts.Parse(os.Args[npa:])

	if cmdMap[CMD_LOGIN].opts.Parsed() {
		make_login(os.Args[2])
		return
	}

	login()

	if cmdMap[CMD_PS].opts.Parsed() {
		list_projects()
		return
	}

	if cmdMap[CMD_LS].opts.Parsed() {
		list_functions(cd.project)
		return
	}

	if cmdMap[CMD_INF].opts.Parsed() {
		info_function(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_ADD].opts.Parsed() {
		add_function(cd.project, os.Args[2], lang, src, mware, event)
		return
	}

	if cmdMap[CMD_RUN].opts.Parsed() {
		run_function(cd.project, os.Args[2], os.Args[3:])
		return
	}

	if cmdMap[CMD_UPD].opts.Parsed() {
		update_function(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_DEL].opts.Parsed() {
		del_function(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_LOGS].opts.Parsed() {
		show_logs(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_MLS].opts.Parsed() {
		list_mware(cd.project)
		return
	}

	if cmdMap[CMD_MADD].opts.Parsed() {
		add_mware(cd.project, os.Args[2], os.Args[3])
		return
	}

	if cmdMap[CMD_MDEL].opts.Parsed() {
		del_mware(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_MENV].opts.Parsed() {
		show_mware_env(cd.project, os.Args[2])
		return
	}

	if cmdMap[CMD_LUSR].opts.Parsed() {
		list_users()
		return
	}

	if cmdMap[CMD_UADD].opts.Parsed() {
		add_user(os.Args[2], name, pass)
		return
	}

	if cmdMap[CMD_UDEL].opts.Parsed() {
		del_user(os.Args[2])
		return
	}

	if cmdMap[CMD_PASS].opts.Parsed() {
		set_password(os.Args[2], pass)
		return
	}

	if cmdMap[CMD_UINF].opts.Parsed() {
		show_user_info(os.Args[2])
		return
	}
}
