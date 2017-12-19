package main

import (
	"encoding/base64"
	"path/filepath"
	"io/ioutil"
	"net/http"
	"strings"
	"strconv"
	"time"
	"flag"
	"fmt"
	"os"

	"../common"
	"../common/http"
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

func fatal(err error) {
	fmt.Printf("ERROR: %s\n", err.Error())
	os.Exit(1)
}

func make_faas_req_x(url string, in interface{}, succ_code int) (*http.Response, error) {
	var address string = "http://" + conf.Login.Host + ":" + conf.Login.Port + "/v1/" + url

	h := make(map[string]string)
	if conf.Login.Token != "" {
		h["X-Auth-Token"] = conf.Login.Token
	}

	return swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Address:	address,
				Headers:	h,
				Success:	succ_code,
			}, in)
}

func faas_login() string {
	resp, err := make_faas_req_x("login", swyapi.UserLogin {
			UserName: conf.Login.User, Password: conf.Login.Pass,
		}, http.StatusOK)
	if err != nil {
		fatal(err)
	}

	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		fatal(fmt.Errorf("No auth token from server"))
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
			fatal(err)
		}

		resp.Body.Close()
		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			first_attempt = false
			refresh_token("")
			goto again
		}

		fatal(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	/* Here we have http.StatusOK */
	defer resp.Body.Close()

	if out != nil {
		err = swyhttp.ReadAndUnmarshalResp(resp, out)
		if err != nil {
			fatal(err)
		}
	}
}

func list_users(args []string, opts [8]string) {
	var uss []swyapi.UserInfo
	make_faas_req("users", swyapi.ListUsers{}, &uss)

	for _, u := range uss {
		fmt.Printf("%s (%s)\n", u.Id, u.Name)
	}
}

func add_user(args []string, opts [8]string) {
	make_faas_req2("adduser", swyapi.AddUser{Id: args[0], Pass: opts[1], Name: opts[0]},
		nil, http.StatusCreated)
}

func del_user(args []string, opts [8]string) {
	make_faas_req2("deluser", swyapi.UserInfo{Id: args[0]}, nil, http.StatusNoContent)
}

func set_password(args []string, opts [8]string) {
	make_faas_req2("setpass", swyapi.UserLogin{UserName: args[0], Password: opts[0]},
		nil, http.StatusCreated)
}

func show_user_info(args []string, opts [8]string) {
	var ui swyapi.UserInfo
	make_faas_req("userinfo", swyapi.UserInfo{Id: args[0]}, &ui)
	fmt.Printf("Name: %s\n", ui.Name)
}

func list_projects(args []string, opts [8]string) {
	var ps []swyapi.ProjectItem
	make_faas_req("project/list", swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func list_functions(project string, args []string, opts [8]string) {
	var fns []swyapi.FunctionItem
	make_faas_req("function/list", swyapi.FunctionList{ Project: project, }, &fns)

	fmt.Printf("%-20s%-10s\n", "NAME", "STATE")
	for _, fn := range fns {
		fmt.Printf("%-20s%-12s\n", fn.FuncName, fn.State)
	}
}

func info_function(project string, args []string, opts [8]string) {
	var ifo swyapi.FunctionInfo
	make_faas_req("function/info", swyapi.FunctionID{ Project: project, FuncName: args[0]}, &ifo)
	ver := ifo.Version
	if len(ver) > 8 {
		ver = ver[:8]
	}

	fmt.Printf("Lang:        %s\n", ifo.Code.Lang)
	fmt.Printf("Version:     %s\n", ver)
	fmt.Printf("State:       %s\n", ifo.State)
	if len(ifo.Mware) > 0 {
		fmt.Printf("Mware:       %s\n", strings.Join(ifo.Mware, ", "))
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
				estr += ":q=" + ifo.Event.MQueue
			}
			if ifo.Event.S3Bucket != "" {
				estr += ":b=" + ifo.Event.S3Bucket
			}
		} else {
			estr += "UNKNOWN"
		}
		fmt.Printf("Event:       %s\n", estr)
	}
	if ifo.URL != "" {
		fmt.Printf("URL:         http://%s:%s%s\n", conf.Login.Host, conf.Login.Port, ifo.URL)
	}
	fmt.Printf("Timeout:     %dms\n", ifo.Size.Timeout)
	if ifo.Size.Rate != 0 {
		fmt.Printf("Rate:        %d:%d\n", ifo.Size.Rate, ifo.Size.Burst)
	}
	fmt.Printf("Memory:      %dMi\n", ifo.Size.Memory)
	fmt.Printf("Called:      %d\n", ifo.Stats.Called)
	if ifo.Stats.Called != 0 {
		lc, _ := time.Parse(time.UnixDate, ifo.Stats.LastCall)
		since := time.Since(lc)
		since -= since % time.Second
		fmt.Printf("Last run:    %s ago\n", since.String())
	}
}

func detect_language(repo string) string {
	fatal(fmt.Errorf("can't detect function language"))
	return ""
}

func encodeFile(file string) string {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		fatal(fmt.Errorf("Can't read file sources: %s", err.Error()))
	}

	return base64.StdEncoding.EncodeToString(data)
}

func parse_rate(val string) (uint, uint) {
	var rate, burst uint64
	var err error

	rl := strings.Split(val, ":")
	rate, err = strconv.ParseUint(rl[0], 10, 32)
	if err != nil {
		fatal(fmt.Errorf("Bad rate value %s: %s", rl[0], err.Error()))
	}
	if len(rl) > 1 {
		burst, err = strconv.ParseUint(rl[1], 10, 32)
		if err != nil {
			fatal(fmt.Errorf("Bad burst value %s: %s", rl[1], err.Error()))
		}
	}

	return uint(rate), uint(burst)
}

func add_function(project string, args []string, opts [8]string) {
	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

	st, err := os.Stat(opts[1])
	if err != nil {
		fatal(fmt.Errorf("Can't stat sources path"))
	}

	if st.IsDir() {
		repo, err := filepath.Abs(opts[1])
		if err != nil {
			fatal(fmt.Errorf("Can't get abs path for repo"))
		}

		fmt.Printf("Will add git repo %s\n", repo)
		sources.Type = "git"
		sources.Repo = repo
	} else {
		fmt.Printf("Will add file %s\n", opts[1])
		sources.Type = "code"
		sources.Code = encodeFile(opts[1])
	}

	if opts[0] == "auto" {
		opts[0] = detect_language(opts[1])
	}

	code.Lang = opts[0]

	mw := strings.Split(opts[2], ",")

	evt := swyapi.FunctionEvent {}
	if opts[3] != "" {
		mwe := strings.Split(opts[3], ":")
		evt.Source = mwe[0]
		if evt.Source == "url" {
			/* nothing */
		} else if evt.Source == "mware" {
			evt.MwareId = mwe[1]
			mwi := strings.SplitN(mwe[2], "=", 2)
			switch mwi[0] {
			case "q":
				evt.MQueue = mwe[1]
			case "b":
				evt.S3Bucket = mwi[1]
			default:
				fatal(fmt.Errorf("Unknown mware event id %s", mwi[0]))
			}
		} else {
			/* FIXME -- CRONTAB */
			fatal(fmt.Errorf("Unknown event string"))
		}
	}

	req := swyapi.FunctionAdd{
		Project: project,
		FuncName: args[0],
		Sources: sources,
		Code: code,
		Mware: mw,
		Event: evt,
	}

	if opts[4] != "" {
		req.Size.Timeout, err = strconv.ParseUint(opts[4], 10, 64)
		if err != nil {
			fatal(fmt.Errorf("Bad tmo value %s: %s", opts[4], err.Error()))
		}
	}

	if opts[5] != "" {
		req.Size.Rate, req.Size.Burst = parse_rate(opts[5])
	}

	make_faas_req("function/add", req, nil)

}

func run_function(project string, args []string, opts [8]string) {
	var rres swyapi.FunctionRunResult
	var argmap = make(map[string]string)

	for _, arg := range args[1:] {
		a := strings.SplitN(arg, "=", 2)
		argmap[a[0]] = a[1]
	}

	make_faas_req("function/run",
		swyapi.FunctionRun{ Project: project, FuncName: args[0], Args: argmap, }, &rres)

	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func update_function(project string, args []string, opts [8]string) {
	req := swyapi.FunctionUpdate {
		Project: project,
		FuncName: args[0],
	}

	if opts[0] != "" {
		req.Code = encodeFile(opts[0])
	}

	if opts[1] != "" {
		var err error
		req.Size = &swyapi.FunctionSize {}
		req.Size.Timeout, err = strconv.ParseUint(opts[1], 10, 64)
		if err != nil {
			fatal(fmt.Errorf("Bad tmo value %s: %s", opts[4], err.Error()))
		}
	}

	if opts[2] != "" {
		if req.Size == nil {
			req.Size = &swyapi.FunctionSize {}
		}

		req.Size.Rate, req.Size.Burst = parse_rate(opts[2])
	}

	make_faas_req("function/update", req, nil)

}

func del_function(project string, args []string, opts [8]string) {
	make_faas_req("function/remove",
		swyapi.FunctionRemove{ Project: project, FuncName: args[0] }, nil)
}

func show_code(project string, args []string, opts [8]string) {
	var res swyapi.FunctionSources
	make_faas_req("function/code",
		swyapi.FunctionXID{ Project: project, FuncName: args[0], Version: opts[0], }, &res)

	data, err := base64.StdEncoding.DecodeString(res.Code)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s", data)
}

func show_logs(project string, args []string, opts [8]string) {
	var res []swyapi.FunctionLogEntry
	make_faas_req("function/logs",
		swyapi.FunctionID{ Project: project, FuncName: args[0], }, &res)

	for _, le := range res {
		fmt.Printf("%36s%8s: %s\n", le.Ts, le.Event, le.Text)
	}
}

func list_mware(project string, args []string, opts [8]string) {
	var mws []swyapi.MwareItem
	make_faas_req("mware/list", swyapi.MwareList{ Project: project, }, &mws)

	fmt.Printf("%-20s%-10s%s\n", "NAME", "TYPE", "OPTIONS")
	for _, mw := range mws {
		fmt.Printf("%-20s%-10s%s\n", mw.ID, mw.Type, "")
	}
}

func add_mware(project string, args []string, opts [8]string) {
	req := swyapi.MwareAdd { Project: project, ID: args[0], Type: args[1] }
	make_faas_req("mware/add", req, nil)
}

func del_mware(project string, args []string, opts [8]string) {
	make_faas_req("mware/remove",
		swyapi.MwareRemove{ Project: project, ID: args[0], }, nil)
}

func mware_types(args []string, opts [8]string) {
	var r []string

	make_faas_req("mware/types", nil, &r)
	for _, mt := range r {
		fmt.Printf("%s\n", mt)
	}
}

func login() {
	home, found := os.LookupEnv("HOME")
	if !found {
		fatal(fmt.Errorf("No HOME dir set"))
	}

	err := swy.ReadYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		fatal(fmt.Errorf("Login first"))
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
		fatal(fmt.Errorf("No HOME dir set"))
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
			fatal(fmt.Errorf("No HOME dir set"))
		}
	}

	conf.Login.Token = faas_login()

	err := swy.WriteYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		fatal(fmt.Errorf("Can't write swifty.conf: %s", err.Error()))
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
	CMD_CODE string		= "code"
	CMD_MLS string		= "mls"
	CMD_MADD string		= "madd"
	CMD_MDEL string		= "mdel"
	CMD_MTYPES string	= "mt"
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
	CMD_CODE,
	CMD_MLS,
	CMD_MADD,
	CMD_MDEL,
	CMD_MTYPES,
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
	pcall	func(string, []string, [8]string)
	call	func([]string, [8]string)
}

var cmdMap = map[string]*cmdDesc {
	CMD_LOGIN:	&cmdDesc{			  opts: flag.NewFlagSet(CMD_LOGIN, flag.ExitOnError) },
	CMD_PS:		&cmdDesc{  call: list_projects,	  opts: flag.NewFlagSet(CMD_PS, flag.ExitOnError) },
	CMD_LS:		&cmdDesc{ pcall: list_functions,  opts: flag.NewFlagSet(CMD_LS, flag.ExitOnError) },
	CMD_INF:	&cmdDesc{ pcall: info_function,	  opts: flag.NewFlagSet(CMD_INF, flag.ExitOnError) },
	CMD_ADD:	&cmdDesc{ pcall: add_function,	  opts: flag.NewFlagSet(CMD_ADD, flag.ExitOnError) },
	CMD_RUN:	&cmdDesc{ pcall: run_function,	  opts: flag.NewFlagSet(CMD_RUN, flag.ExitOnError) },
	CMD_UPD:	&cmdDesc{ pcall: update_function, opts: flag.NewFlagSet(CMD_UPD, flag.ExitOnError) },
	CMD_DEL:	&cmdDesc{ pcall: del_function,	  opts: flag.NewFlagSet(CMD_DEL, flag.ExitOnError) },
	CMD_LOGS:	&cmdDesc{ pcall: show_logs,	  opts: flag.NewFlagSet(CMD_LOGS, flag.ExitOnError) },
	CMD_CODE:	&cmdDesc{ pcall: show_code,	  opts: flag.NewFlagSet(CMD_CODE, flag.ExitOnError) },
	CMD_MLS:	&cmdDesc{ pcall: list_mware,	  opts: flag.NewFlagSet(CMD_MLS, flag.ExitOnError) },
	CMD_MADD:	&cmdDesc{ pcall: add_mware,	  opts: flag.NewFlagSet(CMD_MADD, flag.ExitOnError) },
	CMD_MDEL:	&cmdDesc{ pcall: del_mware,	  opts: flag.NewFlagSet(CMD_MDEL, flag.ExitOnError) },
	CMD_MTYPES:	&cmdDesc{  call: mware_types,	  opts: flag.NewFlagSet(CMD_MTYPES, flag.ExitOnError) },
	CMD_LUSR:	&cmdDesc{  call: list_users,	  opts: flag.NewFlagSet(CMD_LUSR, flag.ExitOnError) },
	CMD_UADD:	&cmdDesc{  call: add_user,	  opts: flag.NewFlagSet(CMD_UADD, flag.ExitOnError) },
	CMD_UDEL:	&cmdDesc{  call: del_user,	  opts: flag.NewFlagSet(CMD_UDEL, flag.ExitOnError) },
	CMD_PASS:	&cmdDesc{  call: set_password,	  opts: flag.NewFlagSet(CMD_PASS, flag.ExitOnError) },
	CMD_UINF:	&cmdDesc{  call: show_user_info,  opts: flag.NewFlagSet(CMD_UINF, flag.ExitOnError) },
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
	var opts [8]string

	bindCmdUsage(CMD_LOGIN,	[]string{"USER:PASS@HOST:PORT"}, "Login into the system", false)

	bindCmdUsage(CMD_PS,	[]string{}, "List projects", false)

	bindCmdUsage(CMD_LS,	[]string{}, "List functions", true)
	bindCmdUsage(CMD_INF,	[]string{"NAME"}, "Function info", true)
	cmdMap[CMD_ADD].opts.StringVar(&opts[0], "lang", "auto", "Language")
	cmdMap[CMD_ADD].opts.StringVar(&opts[1], "src", ".", "Source file")
	cmdMap[CMD_ADD].opts.StringVar(&opts[2], "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_ADD].opts.StringVar(&opts[3], "event", "", "Event this fn is to start")
	cmdMap[CMD_ADD].opts.StringVar(&opts[4], "tmo", "", "Timeout")
	cmdMap[CMD_ADD].opts.StringVar(&opts[5], "rl", "", "Rate (rate[:burst])")
	bindCmdUsage(CMD_ADD,	[]string{"NAME"}, "Add a function", true)
	bindCmdUsage(CMD_RUN,	[]string{"NAME", "ARGUMENTS..."}, "Run a function", true)
	cmdMap[CMD_UPD].opts.StringVar(&opts[0], "src", "", "Source file")
	cmdMap[CMD_UPD].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	cmdMap[CMD_UPD].opts.StringVar(&opts[2], "rl", "", "Rate (rate[:burst])")
	bindCmdUsage(CMD_UPD,	[]string{"NAME"}, "Update a function", true)
	bindCmdUsage(CMD_DEL,	[]string{"NAME"}, "Delete a function", true)
	bindCmdUsage(CMD_LOGS,	[]string{"NAME"}, "Show function logs", true)
	cmdMap[CMD_CODE].opts.StringVar(&opts[0], "version", "", "Version")
	bindCmdUsage(CMD_CODE,  []string{"NAME"}, "Show function code", true)

	bindCmdUsage(CMD_MLS,	[]string{}, "List middleware", true)
	bindCmdUsage(CMD_MADD,	[]string{"ID", "TYPE"}, "Add middleware", true)
	bindCmdUsage(CMD_MDEL,	[]string{"ID"}, "Delete middleware", true)
	bindCmdUsage(CMD_MTYPES, []string{}, "List middleware types", false)

	bindCmdUsage(CMD_LUSR,	[]string{}, "List users", false)
	cmdMap[CMD_UADD].opts.StringVar(&opts[0], "name", "", "User name")
	cmdMap[CMD_UADD].opts.StringVar(&opts[1], "pass", "", "User password")
	bindCmdUsage(CMD_UADD,	[]string{"UID"}, "Add user", false)
	bindCmdUsage(CMD_UDEL,	[]string{"UID"}, "Del user", false)
	cmdMap[CMD_PASS].opts.StringVar(&opts[0], "pass", "", "New password")
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

	if os.Args[1] == CMD_LOGIN {
		make_login(os.Args[2])
		return
	}

	npa := len(cd.pargs) + 2
	cd.opts.Parse(os.Args[npa:])
	login()

	if cd.pcall != nil {
		cd.pcall(cd.project, os.Args[2:], opts)
	} else if cd.call != nil {
		cd.call(os.Args[2:], opts)
	} else {
		fatal(fmt.Errorf("Bad cmd"))
	}
}
