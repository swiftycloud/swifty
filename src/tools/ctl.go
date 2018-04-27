package main

import (
	"encoding/json"
	"encoding/base64"
	"path/filepath"
	"io/ioutil"
	"net/http"
	"strings"
	"strconv"
	"regexp"
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
	TLS		bool		`yaml:"tls"`
	Certs		string		`yaml:"x509crtfile"`
}

var conf YAMLConf

func fatal(err error) {
	fmt.Printf("ERROR: %s\n", err.Error())
	os.Exit(1)
}

func gateProto() string {
	if conf.TLS {
		return "https"
	} else {
		return "http"
	}
}

func make_faas_req3(method, url string, in interface{}, succ_code int, tmo uint) (*http.Response, error) {
	address := gateProto() + "://" + conf.Login.Host + ":" + conf.Login.Port + "/v1/" + url

	h := make(map[string]string)
	if conf.Login.Token != "" {
		h["X-Auth-Token"] = conf.Login.Token
	}

	var crt []byte
	if conf.TLS && conf.Certs != "" {
		var err error

		crt, err = ioutil.ReadFile(conf.Certs)
		if err != nil {
			return nil, fmt.Errorf("Error reading cert file: %s", err.Error())
		}
	}

	return swyhttp.MarshalAndPost(
			&swyhttp.RestReq{
				Method:		method,
				Address:	address,
				Headers:	h,
				Success:	succ_code,
				Timeout:	tmo,
				Certs:		crt,
			}, in)
}

func faas_login() string {
	resp, err := make_faas_req3("POST", "login", swyapi.UserLogin {
			UserName: conf.Login.User, Password: conf.Login.Pass,
		}, http.StatusOK, 0)
	if err != nil {
		fatal(err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		fatal(fmt.Errorf("No auth token from server"))
	}

	var td swyapi.UserToken
	err = swyhttp.ReadAndUnmarshalResp(resp, &td)
	if err != nil {
		fatal(fmt.Errorf("Can't unmarshal login resp: %s", err.Error()))
	}

	fmt.Printf("Logged in, token till %s\n", td.Expires)

	return token
}

func make_faas_req2(method, url string, in interface{}, succ_code int, tmo uint) *http.Response {
	first_attempt := true
again:
	resp, err := make_faas_req3(method, url, in, succ_code, tmo)
	if err != nil {
		if resp == nil {
			fatal(err)
		}

		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			resp.Body.Close()
			first_attempt = false
			refresh_token("")
			goto again
		}

		if resp.StatusCode == http.StatusBadRequest {
			var gerr swyapi.GateErr

			err = swyhttp.ReadAndUnmarshalResp(resp, &gerr)
			resp.Body.Close()

			if err == nil {
				err = fmt.Errorf("Operation failed (%d): %s", gerr.Code, gerr.Message)
			} else {
				err = fmt.Errorf("Operation failed with no details")
			}
		} else {
			err = fmt.Errorf("Bad responce: %s", string(resp.Status))
		}

		fatal(err)
	}

	return resp
}

func make_faas_req1(method, url string, succ int, in interface{}, out interface{}) {
	resp := make_faas_req2(method, url, in, succ, 30)
	/* Here we have http.StatusOK */
	defer resp.Body.Close()

	if out != nil {
		err := swyhttp.ReadAndUnmarshalResp(resp, out)
		if err != nil {
			fatal(err)
		}
	}
}

func make_faas_req(url string, in interface{}, out interface{}) {
	make_faas_req1("POST", url, http.StatusOK, in, out)
}

func list_users(cd *cmdDesc, args []string, opts [16]string) {
	var uss []swyapi.UserInfo
	make_faas_req("users", swyapi.ListUsers{}, &uss)

	for _, u := range uss {
		fmt.Printf("%s (%s)\n", u.Id, u.Name)
	}
}

func add_user(cd *cmdDesc, args []string, opts [16]string) {
	make_faas_req2("POST", "adduser", swyapi.AddUser{Id: args[0], Pass: opts[1], Name: opts[0]},
		http.StatusCreated, 0)
}

func del_user(cd *cmdDesc, args []string, opts [16]string) {
	make_faas_req2("POST", "deluser", swyapi.UserInfo{Id: args[0]},
		http.StatusNoContent, 0)
}

func set_password(cd *cmdDesc, args []string, opts [16]string) {
	make_faas_req2("POST", "setpass", swyapi.UserLogin{UserName: args[0], Password: opts[0]},
		http.StatusCreated, 0)
}

func show_user_info(cd *cmdDesc, args []string, opts [16]string) {
	var ui swyapi.UserInfo
	make_faas_req("userinfo", swyapi.UserInfo{Id: args[0]}, &ui)
	fmt.Printf("Name: %s\n", ui.Name)
}

func do_user_limits(cd *cmdDesc, args []string, opts [16]string) {
	var l swyapi.UserLimits
	chg := false

	if opts[0] != "" {
		l.PlanId = opts[0]
		chg = true
	}

	if opts[1] != "" {
		if l.Fn == nil {
			l.Fn = &swyapi.FunctionLimits{}
		}
		l.Fn.Rate, l.Fn.Burst = parse_rate(opts[1])
		chg = true
	}

	if opts[2] != "" {
		if l.Fn == nil {
			l.Fn = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseUint(opts[2], 10, 32)
		if err != nil {
			fatal(fmt.Errorf("Bad max-fn value %s: %s", opts[2], err.Error()))
		}
		l.Fn.MaxInProj = uint(v)
		chg = true
	}

	if opts[3] != "" {
		if l.Fn == nil {
			l.Fn = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseFloat(opts[3], 64)
		if err != nil {
			fatal(fmt.Errorf("Bad GBS value %s: %s", opts[3], err.Error()))
		}
		l.Fn.GBS = v
		chg = true
	}

	if opts[4] != "" {
		if l.Fn == nil {
			l.Fn = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseUint(opts[4], 10, 64)
		if err != nil {
			fatal(fmt.Errorf("Bad bytes-out value %s: %s", opts[4], err.Error()))
		}
		l.Fn.BytesOut = v
		chg = true
	}

	if chg {
		l.Id = args[0]
		make_faas_req("limits/set", &l, nil)
	} else {
		make_faas_req("limits/get", swyapi.UserInfo{Id: args[0]}, &l)
		if l.PlanId != "" {
			fmt.Printf("Plan ID: %s\n", l.PlanId)
		}
		if l.Fn != nil {
			fmt.Printf("Functions:\n")
			if l.Fn.Rate != 0 {
				fmt.Printf("    Rate:              %d:%d\n", l.Fn.Rate, l.Fn.Burst)
			}
			if l.Fn.MaxInProj != 0 {
				fmt.Printf("    Max in project:    %d\n", l.Fn.MaxInProj)
			}
			if l.Fn.GBS != 0 {
				fmt.Printf("    Max GBS:           %f\n", l.Fn.GBS)
			}
			if l.Fn.BytesOut != 0 {
				fmt.Printf("    Max bytes out:     %d\n", formatBytes(l.Fn.BytesOut))
			}
		}
	}
}

func dateOnly(tm string) string {
	if tm == "" {
		return ""
	}

	t, _ := time.Parse(time.RFC1123Z, tm)
	return t.Format("02 Jan 2006")
}

func show_stats(cd *cmdDesc, args []string, opts [16]string) {
	var rq swyapi.TenantStatsReq
	var st swyapi.TenantStatsResp
	var err error

	if opts[0] != "" {
		rq.Periods, err = strconv.Atoi(opts[0])
		if err != nil {
			fatal(fmt.Errorf("Bad period value %s: %s", opts[0],  err.Error()))
		}
	}

	make_faas_req("stats", rq, &st)

	for _, s := range(st.Stats) {
		fmt.Printf("---\n%s ... %s\n", dateOnly(s.From), dateOnly(s.Till))
		fmt.Printf("Called:           %d\n", s.Called)
		fmt.Printf("GBS:              %f\n", s.GBS)
		fmt.Printf("Bytes sent:       %s\n", formatBytes(s.BytesOut))
	}
}

func list_projects(cd *cmdDesc, args []string, opts [16]string) {
	var ps []swyapi.ProjectItem
	make_faas_req("project/list", swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func resolve_fn(project, fname string) string {
	var ifo []swyapi.FunctionInfo
	ua := []string{}
	if project != "" {
		ua = append(ua, "project=" + project)
	}
	make_faas_req1("GET", url("functions", ua), http.StatusOK, nil, &ifo)
	for _, i := range ifo {
		if i.Name == fname {
			return i.Id
		}
	}

	fatal(fmt.Errorf("\tCanname %s not resolved", fname))
	return ""
}

func list_functions(cd *cmdDesc, args []string, opts [16]string) {
	ua := []string{}
	if cd.project != "" {
		ua = append(ua, "project=" + cd.project)
	}

	if opts[0] == "" {
		var fns []swyapi.FunctionInfo
		make_faas_req1("GET", url("functions", ua), http.StatusOK, nil, &fns)

		fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "STATE")
		for _, fn := range fns {
			fmt.Printf("%-32s%-20s%-12s\n", fn.Id, fn.Name, fn.State)
		}
	} else if opts[0] == "json" {
		ua = append(ua, "details=1")
		resp := make_faas_req2("GET", url("functions", ua), nil, http.StatusOK, 30)
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fatal(fmt.Errorf("\tCan't parse responce: %s", err.Error()))
		}
		fmt.Printf("%s\n", string(body))
	} else {
		fatal(fmt.Errorf("Bad -o value %s", opts[0]))
	}
}

func sb2s(b uint64, o uint64, s string) string {
	if b >= 1 << o {
		i := b >> o
		r := ((b - (i<<o)) >> (o-10))
		if r >= 1000 {
			/* Values 1000 through 1023 are valid, but should
			 * not result in .10 fraction below. Maybe we should
			 * rather split tenths by 102? Maybe 103? Dunno.
			 */
			r = 999
		}

		if r >= 100 {
			return fmt.Sprintf("%d.%d %s", i, r/100, s)
		} else {
			return fmt.Sprintf("%d %s", i, s)
		}
	}
	return ""
}

func formatBytes(b uint64) string {
	var bo string

	bo = sb2s(b, 30, "Gb")
	if bo == "" {
		bo = sb2s(b, 20, "Mb")
		if bo == "" {
			bo = sb2s(b, 10, "Kb")
			if bo == "" {
				bo = fmt.Sprintf("%d bytes", b)
			}
		}
	}

	return bo
}

func info_function(cd *cmdDesc, args []string, opts [16]string) {
	var ifo swyapi.FunctionInfo
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("GET", "functions/" + args[0], http.StatusOK, nil, &ifo)
	ver := ifo.Version
	if len(ver) > 8 {
		ver = ver[:8]
	}

	fmt.Printf("Lang:        %s\n", ifo.Code.Lang)

	rv := ""
	if len(ifo.RdyVersions) != 0 {
		rv = " (" + strings.Join(ifo.RdyVersions, ",") + ")"
	}
	fmt.Printf("Version:     %s%s\n", ver, rv)
	fmt.Printf("State:       %s\n", ifo.State)
	if len(ifo.Mware) > 0 {
		fmt.Printf("Mware:       %s\n", strings.Join(ifo.Mware, ", "))
	}
	if ifo.URL != "" {
		fmt.Printf("URL:         %s://%s\n", gateProto(), ifo.URL)
	}
	fmt.Printf("Timeout:     %dms\n", ifo.Size.Timeout)
	if ifo.Size.Rate != 0 {
		fmt.Printf("Rate:        %d:%d\n", ifo.Size.Rate, ifo.Size.Burst)
	}
	fmt.Printf("Memory:      %dMi\n", ifo.Size.Memory)
	fmt.Printf("Called:      %d\n", ifo.Stats[0].Called)
	if ifo.Stats[0].Called != 0 {
		lc, _ := time.Parse(time.RFC1123Z, ifo.Stats[0].LastCall)
		since := time.Since(lc)
		since -= since % time.Second
		fmt.Printf("Last run:    %s ago\n", since.String())
		fmt.Printf("Time:        %d (avg %d) usec\n", ifo.Stats[0].Time, ifo.Stats[0].Time / ifo.Stats[0].Called)
		fmt.Printf("GBS:         %f\n", ifo.Stats[0].GBS)
	}

	if b := ifo.Stats[0].BytesOut; b != 0 {
		fmt.Printf("Bytes sent:  %s\n", formatBytes(b))
	}

	if ifo.AuthCtx != "" {
		fmt.Printf("Auth by:     %s\n", ifo.AuthCtx)
	}

	if ifo.UserData != "" {
		fmt.Printf("Data:        %s\n", ifo.UserData)
	}
}

func check_lang(cd *cmdDesc, args []string, opts [16]string) {
	l := detect_language(opts[0], "code")
	fmt.Printf("%s\n", l)
}

func detect_language(path string, typ string) string {
	if typ != "code" {
		fatal(fmt.Errorf("can't detect repo language"))
		return ""
	}

	cont, err := ioutil.ReadFile(path)
	if err != nil {
		fatal(fmt.Errorf("Can't read sources: %s", err.Error()))
		return ""
	}

	pyr := regexp.MustCompile("^def\\s+main\\s*\\(")
	gor := regexp.MustCompile("^func\\s+Main\\s*\\(.*interface\\s*{\\s*}")
	swr := regexp.MustCompile("^func\\s+Main\\s*\\(.*->\\s*Encodable")
	jsr := regexp.MustCompile("^exports.Main\\s*=\\s*function")

	lines := strings.Split(string(cont), "\n")
	for _, ln := range(lines) {
		if pyr.MatchString(ln) {
			return "python"
		}

		if gor.MatchString(ln) {
			return "golang"
		}

		if swr.MatchString(ln) {
			return "swift"
		}

		if jsr.MatchString(ln) {
			return "nodejs"
		}
	}

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

func add_function(cd *cmdDesc, args []string, opts [16]string) {
	ua := []string{}
	if cd.project != "" {
		ua = append(ua, "project=" + cd.project)
	}

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

	if opts[0] == "" {
		opts[0] = detect_language(opts[1], sources.Type)
		fmt.Printf("Detected lang to %s\n", opts[0])
	}

	code.Lang = opts[0]
	if opts[7] != "" {
		code.Env = strings.Split(opts[7], ":")
	}

	var mw []string
	if opts[2] != "" {
		mw = strings.Split(opts[2], ",")
	}

	req := swyapi.FunctionAdd{
		Name: args[0],
		Sources: sources,
		Code: code,
		Mware: mw,
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

	if opts[6] != "" {
		req.UserData = opts[6]
	}

	if opts[8] != "" {
		req.AuthCtx = opts[8]
	}

	if !cd.req {
		var fid string
		make_faas_req1("POST", url("functions", ua), http.StatusOK, req, &fid)
		fmt.Printf("Function %s created\n", fid)
	} else {
		d, err := json.Marshal(req)
		if err == nil {
			fmt.Printf("%s\n", string(d))
		}
	}
}

/* Splits a=v,a=v,... string into map */
func split_args_string(as string) map[string]string {
	ret := make(map[string]string)
	for _, arg := range strings.Split(as, ",") {
		a := strings.SplitN(arg, "=", 2)
		ret[a[0]] = a[1]
	}
	return ret
}

func make_args_string(args map[string]string) string {
	var ass []string
	for a, v := range args {
		ass = append(ass, a + "=" + v)
	}
	return strings.Join(ass, ",")
}

func run_function(cd *cmdDesc, args []string, opts [16]string) {
	var rres swyapi.FunctionRunResult

	argmap := split_args_string(args[1])
	make_faas_req("function/run",
		swyapi.FunctionRun{ Project: cd.project, FuncName: args[0], Args: argmap, }, &rres)

	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func update_function(cd *cmdDesc, args []string, opts [16]string) {
	req := swyapi.FunctionUpdate {
		Project: cd.project,
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

	if opts[3] != "" {
		if opts[3] == "-" {
			req.Mware = &[]string{}
		} else {
			mw := strings.Split(opts[3], ",")
			req.Mware = &mw
		}
	}

	if opts[4] != "" {
		req.UserData = opts[4]
	}

	if opts[7] != "" {
		var ac string
		if opts[7] != "-" {
			ac = opts[7]
		}

		req.AuthCtx = &ac
	}

	make_faas_req("function/update", req, nil)

	if opts[5] != "" {
		fmt.Printf("Wait FN %s\n", opts[5])
		wait_fn(cd, []string{args[0]}, [16]string{opts[5], "15000"})
	}

	if opts[6] != "" {
		fmt.Printf("Run FN %s\n", opts[6])
		run_function(cd, []string{args[0], opts[6]}, [16]string{})
	}
}

func del_function(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("DELETE", "functions/" + args[0], http.StatusOK, nil, nil)
}

func on_fn(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("PUT", "function/" + args[0] + "/state?v=ready", http.StatusOK, nil, nil)
}

func off_fn(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("PUT", "function/" + args[0] + "/state?v=deactivated", http.StatusOK, nil, nil)
}

func list_events(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	var eds []swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/events", http.StatusOK,  nil, &eds)
	for _, e := range eds {
		fmt.Printf("%16s%20s%8s\n", e.Id, e.Name, e.Source)
	}
}

func add_event(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	e := swyapi.FunctionEvent {
		Name: args[1],
		Source: args[2],
	}
	if e.Source == "cron" {
		e.Cron = &swyapi.FunctionEventCron {
			Tab: opts[0],
			Args: split_args_string(opts[1]),
		}
	}
	if e.Source == "s3" {
		e.S3 = &swyapi.FunctionEventS3 {
			Bucket: opts[0],
			Ops: opts[1],
		}
	}
	var res string
	make_faas_req1("POST", "functions/" + args[0] + "/events", http.StatusOK, &e, &res)
	fmt.Printf("Event %s created\n", res)
}

func info_event(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	var e swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/events/" + args[1], http.StatusOK,  nil, &e)
	fmt.Printf("Name:          %s\n", e.Name)
	fmt.Printf("Source:        %s\n", e.Source)
	if e.Cron != nil {
		fmt.Printf("Tab:        %s\n", e.Cron.Tab)
		fmt.Printf("Args:       %s\n", make_args_string(e.Cron.Args))
	}
	if e.S3 != nil {
		fmt.Printf("Bucket:     %s\n", e.S3.Bucket)
		fmt.Printf("Ops:        %s\n", e.S3.Ops)
	}
}

func del_event(cd *cmdDesc, args []string, opts [16]string) {
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("DELETE", "functions/" + args[0] + "/events/" + args[1], http.StatusOK, nil, nil)
}

func wait_fn(cd *cmdDesc, args []string, opts [16]string) {
	req := swyapi.FunctionWait{
		Project: cd.project,
		FuncName: args[0],
	}

	if opts[0] != "" {
		req.Version = opts[0]
	}

	if opts[1] != "" {
		tmo, err := strconv.ParseUint(opts[1], 10, 32)
		if err != nil {
			fatal(fmt.Errorf("Bad timeout value"))
		}

		req.Timeout = uint32(tmo)
	}

	var httpTmo uint /* seconds */
	httpTmo = uint((time.Duration(req.Timeout) * time.Millisecond) / time.Second)
	if httpTmo <= 1 {
		httpTmo = 1
	}

	make_faas_req2("POST", "function/wait", req, http.StatusOK, httpTmo + 10)
}

func show_code(cd *cmdDesc, args []string, opts [16]string) {
	var res swyapi.FunctionSources
	make_faas_req("function/code",
		swyapi.FunctionXID{ Project: cd.project, FuncName: args[0], Version: opts[0], }, &res)

	data, err := base64.StdEncoding.DecodeString(res.Code)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s", data)
}

func show_logs(cd *cmdDesc, args []string, opts [16]string) {
	var res []swyapi.FunctionLogEntry
	args[0] = resolve_fn(cd.project, args[0])
	make_faas_req1("GET", "functions/" + args[0] + "/logs", http.StatusOK, nil, &res)

	for _, le := range res {
		fmt.Printf("%36s%8s: %s\n", le.Ts, le.Event, le.Text)
	}
}

func url(url string, args []string) string {
	if len(args) != 0 {
		url += "?" + strings.Join(args, "&")
	}
	return url
}

func list_mware(cd *cmdDesc, args []string, opts [16]string) {
	var mws []swyapi.MwareInfo
	ua := []string{}
	if cd.project != "" {
		ua = append(ua, "project=" + cd.project)
	}
	if opts[1] != "" {
		ua = append(ua, "type=" + opts[1])
	}

	if opts[0] == "" {
		make_faas_req1("GET", url("middleware", ua), http.StatusOK, nil, &mws)
		fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "TYPE")
		for _, mw := range mws {
			fmt.Printf("%-32s%-20s%-10s\n", mw.ID, mw.Name, mw.Type)
		}
	} else if opts[0] == "json" {
		ua = append(ua, "details=1")
		resp := make_faas_req2("GET", url("middleware", ua), nil, http.StatusOK, 30)
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fatal(fmt.Errorf("\tCan't parse responce: %s", err.Error()))
		}
		fmt.Printf("%s\n", string(body))
	}
}

func info_mware(cd *cmdDesc, args []string, opts [16]string) {
	var resp swyapi.MwareInfo

	make_faas_req1("GET", "middleware/" + args[0], http.StatusOK, nil, &resp)
	fmt.Printf("Name:         %s\n", resp.Name)
	fmt.Printf("Type:         %s\n", resp.Type)
	if resp.DU != nil {
		fmt.Printf("Disk usage:   %s\n", formatBytes(*resp.DU << 10))
	}
	if resp.UserData != "" {
		fmt.Printf("Data:         %s\n", resp.UserData)
	}
}

func add_mware(cd *cmdDesc, args []string, opts [16]string) {
	p := ""
	if cd.project != "" {
		p = "?project=" + cd.project
	}

	req := swyapi.MwareAdd {
		Name: args[0],
		Type: args[1],
		UserData: opts[0],
	}

	if !cd.req {
		var id string
		make_faas_req1("POST", "middleware" + p, http.StatusOK, &req, &id)
		fmt.Printf("Mware %s created\n", id)
	} else {
		d, err := json.Marshal(req)
		if err == nil {
			fmt.Printf("%s\n", string(d))
		}
	}
}

func del_mware(cd *cmdDesc, args []string, opts [16]string) {
	make_faas_req1("DELETE", "middleware/" + args[0], http.StatusOK, nil, nil)
}

func deploy_stop(cd *cmdDesc, args []string, opts [16]string) {
	make_faas_req("deploy/stop",
		swyapi.DeployId{Project: cd.project, Name: args[0]}, nil)
}

func deploy_info(cd *cmdDesc, args []string, opts [16]string) {
	var di swyapi.DeployInfo
	make_faas_req("deploy/info",
		swyapi.DeployId{Project: cd.project, Name: args[0]}, &di)
	fmt.Printf("State:        %s\n", di.State)
	fmt.Printf("Items:\n")
	for _, i := range di.Items {
		fmt.Printf("\t%s: %s, %s\n", i.Name, i.Type, i.State)
	}
}

func deploy_start(cd *cmdDesc, args []string, opts [16]string) {
	cont, err := ioutil.ReadFile(args[1])
	if err != nil {
		fatal(fmt.Errorf("Can't read desc flie: %s", err.Error()))
	}

	var items []swyapi.DeployItem
	err = json.Unmarshal(cont, &items)
	if err != nil {
		fatal(fmt.Errorf("Can't parse items: %s", err.Error()))
	}

	make_faas_req("deploy/start",
		swyapi.DeployStart{Project: cd.project, Name: args[0], Items: items}, nil)
}

func s3_access(cd *cmdDesc, args []string, opts [16]string) {
	lt, err := strconv.Atoi(opts[0])
	if err != nil {
		fatal(fmt.Errorf("Bad lifetie value: %s", err.Error()))
	}

	var creds swyapi.S3Creds

	p := ""
	if cd.project != "" {
		p = "?project=" + cd.project
	}

	make_faas_req("s3/access" + p,
		swyapi.S3Access{ Bucket: args[0], Lifetime: uint32(lt)}, &creds)
	fmt.Printf("Endpoint %s\n", creds.Endpoint)
	fmt.Printf("Key:     %s\n", creds.Key)
	fmt.Printf("Secret:  %s\n", creds.Secret)
	fmt.Printf("Expires: in %d seconds\n", creds.Expires)
}

func req_list(url string) {
	var r []string

	make_faas_req(url, nil, &r)
	for _, v := range r {
		fmt.Printf("%s\n", v)
	}
}

func languages(cd *cmdDesc, args []string, opts [16]string) {
	req_list("info/langs")
}

func mware_types(cd *cmdDesc, args []string, opts [16]string) {
	req_list("info/mwares")
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

func show_login() {
	fmt.Printf("%s@%s:%s (%s)\n", conf.Login.User, conf.Login.Host, conf.Login.Port, gateProto())
}

func manage_login(cd *cmdDesc, args []string, opts [16]string) {
	action := "show"
	if len(args) >= 1 {
		action = args[0]
	}

	switch action {
	case "show":
		show_login()
	}
}

func make_login(creds string, opts [16]string) {
	//
	// Login string is user:pass@host:port
	//
	// swifty.user:swifty@10.94.96.216:8686
	//
	home, found := os.LookupEnv("HOME")
	if !found {
		fatal(fmt.Errorf("No HOME dir set"))
	}

	c := swy.ParseXCreds(creds)
	conf.Login.User = c.User
	conf.Login.Pass = c.Pass
	conf.Login.Host = c.Host
	conf.Login.Port = c.Port

	if opts[0] == "yes" {
		conf.TLS = true
		if opts[1] != "" {
			conf.Certs = opts[1]
		}
	}

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
	CMD_ME string		= "me"
	CMD_STATS string	= "st"
	CMD_PS string		= "ps"
	CMD_LS string		= "ls"
	CMD_INF string		= "inf"
	CMD_ADD string		= "add"
	CMD_RUN string		= "run"
	CMD_UPD string		= "upd"
	CMD_DEL string		= "del"
	CMD_LOGS string		= "logs"
	CMD_CODE string		= "code"
	CMD_ON string		= "on"
	CMD_OFF string		= "off"
	CMD_WAIT string		= "wait"
	CMD_EL string		= "el"
	CMD_EA string		= "ea"
	CMD_EI string		= "ei"
	CMD_ED string		= "ed"
	CMD_MLS string		= "mls"
	CMD_MINF string		= "minf"
	CMD_MADD string		= "madd"
	CMD_MDEL string		= "mdel"
	CMD_S3ACC string	= "s3acc"
	CMD_DSTART string	= "ds"
	CMD_DINF string		= "di"
	CMD_DSTOP string	= "dx"
	CMD_LUSR string		= "uls"
	CMD_UADD string		= "uadd"
	CMD_UDEL string		= "udel"
	CMD_PASS string		= "pass"
	CMD_UINF string		= "uinf"
	CMD_LIMITS string	= "limits"
	CMD_MTYPES string	= "mt"
	CMD_LANGS string	= "lng"
	CMD_LANG string		= "ld"
)

var cmdOrder = []string {
	CMD_LOGIN,
	CMD_ME,
	CMD_STATS,
	CMD_PS,
	CMD_LS,
	CMD_INF,
	CMD_ADD,
	CMD_RUN,
	CMD_UPD,
	CMD_DEL,
	CMD_LOGS,
	CMD_CODE,
	CMD_ON,
	CMD_OFF,
	CMD_WAIT,
	CMD_EL,
	CMD_EA,
	CMD_EI,
	CMD_ED,
	CMD_MLS,
	CMD_MINF,
	CMD_MADD,
	CMD_MDEL,
	CMD_S3ACC,
	CMD_DSTART,
	CMD_DINF,
	CMD_DSTOP,
	CMD_LUSR,
	CMD_UADD,
	CMD_UDEL,
	CMD_PASS,
	CMD_UINF,
	CMD_LIMITS,
	CMD_LANGS,
	CMD_MTYPES,
	CMD_LANG,
}

type cmdDesc struct {
	opts	*flag.FlagSet
	pargs	[]string
	project	string
	req	bool
	call	func(*cmdDesc, []string, [16]string)
}

var cmdMap = map[string]*cmdDesc {
	CMD_LOGIN:	&cmdDesc{			  opts: flag.NewFlagSet(CMD_LOGIN, flag.ExitOnError) },
	CMD_ME:		&cmdDesc{ call: manage_login,	  opts: flag.NewFlagSet(CMD_ME, flag.ExitOnError) },
	CMD_STATS:	&cmdDesc{ call: show_stats,	  opts: flag.NewFlagSet(CMD_STATS, flag.ExitOnError) },
	CMD_PS:		&cmdDesc{ call: list_projects,	  opts: flag.NewFlagSet(CMD_PS, flag.ExitOnError) },
	CMD_LS:		&cmdDesc{ call: list_functions,  opts: flag.NewFlagSet(CMD_LS, flag.ExitOnError) },
	CMD_INF:	&cmdDesc{ call: info_function,	  opts: flag.NewFlagSet(CMD_INF, flag.ExitOnError) },
	CMD_ADD:	&cmdDesc{ call: add_function,	  opts: flag.NewFlagSet(CMD_ADD, flag.ExitOnError) },
	CMD_RUN:	&cmdDesc{ call: run_function,	  opts: flag.NewFlagSet(CMD_RUN, flag.ExitOnError) },
	CMD_UPD:	&cmdDesc{ call: update_function, opts: flag.NewFlagSet(CMD_UPD, flag.ExitOnError) },
	CMD_DEL:	&cmdDesc{ call: del_function,	  opts: flag.NewFlagSet(CMD_DEL, flag.ExitOnError) },
	CMD_LOGS:	&cmdDesc{ call: show_logs,	  opts: flag.NewFlagSet(CMD_LOGS, flag.ExitOnError) },
	CMD_CODE:	&cmdDesc{ call: show_code,	  opts: flag.NewFlagSet(CMD_CODE, flag.ExitOnError) },
	CMD_ON:		&cmdDesc{ call: on_fn,		  opts: flag.NewFlagSet(CMD_ON, flag.ExitOnError) },
	CMD_OFF:	&cmdDesc{ call: off_fn,	  opts: flag.NewFlagSet(CMD_OFF, flag.ExitOnError) },
	CMD_WAIT:	&cmdDesc{ call: wait_fn,	  opts: flag.NewFlagSet(CMD_WAIT, flag.ExitOnError) },
	CMD_EL:		&cmdDesc{ call: list_events,	  opts: flag.NewFlagSet(CMD_EL, flag.ExitOnError) },
	CMD_EA:		&cmdDesc{ call: add_event,	  opts: flag.NewFlagSet(CMD_EA, flag.ExitOnError) },
	CMD_EI:		&cmdDesc{ call: info_event,	  opts: flag.NewFlagSet(CMD_EI, flag.ExitOnError) },
	CMD_ED:		&cmdDesc{ call: del_event,	  opts: flag.NewFlagSet(CMD_ED, flag.ExitOnError) },
	CMD_MLS:	&cmdDesc{ call: list_mware,	  opts: flag.NewFlagSet(CMD_MLS, flag.ExitOnError) },
	CMD_MINF:	&cmdDesc{ call: info_mware,	  opts: flag.NewFlagSet(CMD_INF, flag.ExitOnError) },
	CMD_MADD:	&cmdDesc{ call: add_mware,	  opts: flag.NewFlagSet(CMD_MADD, flag.ExitOnError) },
	CMD_MDEL:	&cmdDesc{ call: del_mware,	  opts: flag.NewFlagSet(CMD_MDEL, flag.ExitOnError) },
	CMD_S3ACC:	&cmdDesc{ call: s3_access,	  opts: flag.NewFlagSet(CMD_S3ACC, flag.ExitOnError) },
	CMD_DSTART:	&cmdDesc{ call: deploy_start,	  opts: flag.NewFlagSet(CMD_DSTART, flag.ExitOnError) },
	CMD_DINF:	&cmdDesc{ call: deploy_info,	  opts: flag.NewFlagSet(CMD_DINF, flag.ExitOnError) },
	CMD_DSTOP:	&cmdDesc{ call: deploy_stop,	  opts: flag.NewFlagSet(CMD_DSTOP, flag.ExitOnError) },
	CMD_LUSR:	&cmdDesc{ call: list_users,	  opts: flag.NewFlagSet(CMD_LUSR, flag.ExitOnError) },
	CMD_UADD:	&cmdDesc{ call: add_user,	  opts: flag.NewFlagSet(CMD_UADD, flag.ExitOnError) },
	CMD_UDEL:	&cmdDesc{ call: del_user,	  opts: flag.NewFlagSet(CMD_UDEL, flag.ExitOnError) },
	CMD_PASS:	&cmdDesc{ call: set_password,	  opts: flag.NewFlagSet(CMD_PASS, flag.ExitOnError) },
	CMD_UINF:	&cmdDesc{ call: show_user_info,  opts: flag.NewFlagSet(CMD_UINF, flag.ExitOnError) },
	CMD_LIMITS:	&cmdDesc{ call: do_user_limits,  opts: flag.NewFlagSet(CMD_LIMITS, flag.ExitOnError) },
	CMD_LANGS:	&cmdDesc{ call: languages,	  opts: flag.NewFlagSet(CMD_LANGS, flag.ExitOnError) },
	CMD_MTYPES:	&cmdDesc{ call: mware_types,	  opts: flag.NewFlagSet(CMD_MTYPES, flag.ExitOnError) },
	CMD_LANG:	&cmdDesc{ call: check_lang,	  opts: flag.NewFlagSet(CMD_LANG, flag.ExitOnError) },
}

func bindCmdUsage(cmd string, args []string, help string, wp bool) {
	cd := cmdMap[cmd]
	if wp {
		cd.opts.StringVar(&cd.project, "proj", "", "Project to work on")
	}
	cd.opts.BoolVar(&cd.req, "req", false, "Only show the request to be sent")

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
	var opts [16]string

	cmdMap[CMD_LOGIN].opts.StringVar(&opts[0], "tls", "no", "TLS mode")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[1], "cert", "", "x509 cert file")
	bindCmdUsage(CMD_LOGIN,	[]string{"USER:PASS@HOST:PORT"}, "Login into the system", false)

	bindCmdUsage(CMD_ME, []string{"ACTION"}, "Manage login", false)

	cmdMap[CMD_STATS].opts.StringVar(&opts[0], "p", "0", "Periods to report")
	bindCmdUsage(CMD_STATS,	[]string{}, "Show stats", false)
	bindCmdUsage(CMD_PS,	[]string{}, "List projects", false)

	cmdMap[CMD_LS].opts.StringVar(&opts[0], "o", "", "Output format (NONE, json)")
	bindCmdUsage(CMD_LS,	[]string{}, "List functions", true)
	bindCmdUsage(CMD_INF,	[]string{"NAME"}, "Function info", true)
	cmdMap[CMD_ADD].opts.StringVar(&opts[0], "lang", "auto", "Language")
	cmdMap[CMD_ADD].opts.StringVar(&opts[1], "src", ".", "Source file")
	cmdMap[CMD_ADD].opts.StringVar(&opts[2], "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_ADD].opts.StringVar(&opts[4], "tmo", "", "Timeout")
	cmdMap[CMD_ADD].opts.StringVar(&opts[5], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_ADD].opts.StringVar(&opts[6], "data", "", "Any text associated with fn")
	cmdMap[CMD_ADD].opts.StringVar(&opts[7], "env", "", "Colon-separated list of env vars")
	cmdMap[CMD_ADD].opts.StringVar(&opts[8], "auth", "", "ID of auth mware to verify the call")
	bindCmdUsage(CMD_ADD,	[]string{"NAME"}, "Add a function", true)
	bindCmdUsage(CMD_RUN,	[]string{"NAME", "ARG=VAL,..."}, "Run a function", true)
	cmdMap[CMD_UPD].opts.StringVar(&opts[0], "src", "", "Source file")
	cmdMap[CMD_UPD].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	cmdMap[CMD_UPD].opts.StringVar(&opts[2], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_UPD].opts.StringVar(&opts[3], "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_UPD].opts.StringVar(&opts[4], "data", "", "Associated text")
	cmdMap[CMD_UPD].opts.StringVar(&opts[5], "ver", "", "Version")
	cmdMap[CMD_UPD].opts.StringVar(&opts[6], "arg", "", "Args")
	cmdMap[CMD_UPD].opts.StringVar(&opts[7], "auth", "", "Auth context (- for off)")
	bindCmdUsage(CMD_UPD,	[]string{"NAME"}, "Update a function", true)
	bindCmdUsage(CMD_DEL,	[]string{"NAME"}, "Delete a function", true)
	bindCmdUsage(CMD_LOGS,	[]string{"NAME"}, "Show function logs", true)
	cmdMap[CMD_CODE].opts.StringVar(&opts[0], "version", "", "Version")
	bindCmdUsage(CMD_CODE,  []string{"NAME"}, "Show function code", true)
	bindCmdUsage(CMD_ON,	[]string{"NAME"}, "Activate function", true)
	bindCmdUsage(CMD_OFF,	[]string{"NAME"}, "Deactivate function", true)

	cmdMap[CMD_WAIT].opts.StringVar(&opts[0], "version", "", "Version")
	cmdMap[CMD_WAIT].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	bindCmdUsage(CMD_WAIT,	[]string{"NAME"}, "Wait function event", true)

	bindCmdUsage(CMD_EL,	[]string{"NAME"}, "List events for a function", true)
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "tab", "", "Cron tab")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "args", "", "Cron args")
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "buck", "", "S3 bucket")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "ops", "", "S3 ops")
	bindCmdUsage(CMD_EA,	[]string{"NAME", "NAME", "SRC"}, "Add event", true)
	bindCmdUsage(CMD_EI,	[]string{"NAME", "EID"}, "Show event info", true)
	bindCmdUsage(CMD_ED,	[]string{"NAME", "EID"}, "Remove event", true)

	cmdMap[CMD_MLS].opts.StringVar(&opts[0], "o", "", "Output format (NONE, json)")
	cmdMap[CMD_MLS].opts.StringVar(&opts[1], "type", "", "Filter mware by type")
	bindCmdUsage(CMD_MLS,	[]string{}, "List middleware", true)
	bindCmdUsage(CMD_MINF,	[]string{"ID"}, "Middleware info", true)
	cmdMap[CMD_MADD].opts.StringVar(&opts[0], "data", "", "Associated text")
	bindCmdUsage(CMD_MADD,	[]string{"NAME", "TYPE"}, "Add middleware", true)
	bindCmdUsage(CMD_MDEL,	[]string{"ID"}, "Delete middleware", true)

	cmdMap[CMD_S3ACC].opts.StringVar(&opts[0], "life", "60", "Lifetime (default 1 min)")
	bindCmdUsage(CMD_S3ACC,	[]string{"BUCKET"}, "Get keys for S3", true)

	bindCmdUsage(CMD_DSTART, []string{"NAME", "DESC"}, "Start deployment", true)
	bindCmdUsage(CMD_DINF,	[]string{"NAME"}, "Show info about deployment", true)
	bindCmdUsage(CMD_DSTOP,	[]string{"NAME"}, "Stop deployment", true)

	bindCmdUsage(CMD_LUSR,	[]string{}, "List users", false)
	cmdMap[CMD_UADD].opts.StringVar(&opts[0], "name", "", "User name")
	cmdMap[CMD_UADD].opts.StringVar(&opts[1], "pass", "", "User password")
	bindCmdUsage(CMD_UADD,	[]string{"UID"}, "Add user", false)
	bindCmdUsage(CMD_UDEL,	[]string{"UID"}, "Del user", false)
	cmdMap[CMD_PASS].opts.StringVar(&opts[0], "pass", "", "New password")
	bindCmdUsage(CMD_PASS,	[]string{"UID"}, "Set password", false)
	bindCmdUsage(CMD_UINF,	[]string{"UID"}, "Get user info", false)
	cmdMap[CMD_LIMITS].opts.StringVar(&opts[0], "plan", "", "Taroff plan ID")
	cmdMap[CMD_LIMITS].opts.StringVar(&opts[1], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_LIMITS].opts.StringVar(&opts[2], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_LIMITS].opts.StringVar(&opts[3], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_LIMITS].opts.StringVar(&opts[4], "bo", "", "Maximum outgoing network bytes")
	bindCmdUsage(CMD_LIMITS, []string{"UID"}, "Get/Set limits for user", false)

	bindCmdUsage(CMD_MTYPES, []string{}, "List middleware types", false)
	bindCmdUsage(CMD_LANGS, []string{}, "List of supported languages", false)

	cmdMap[CMD_LANG].opts.StringVar(&opts[0], "src", "", "File")
	bindCmdUsage(CMD_LANG, []string{}, "Check source language", false)

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
	if len(os.Args) >= npa {
		cd.opts.Parse(os.Args[npa:])
	}

	if os.Args[1] == CMD_LOGIN {
		make_login(os.Args[2], opts)
		return
	}

	login()

	if cd.call != nil {
		cd.call(cd, os.Args[2:], opts)
	} else {
		fatal(fmt.Errorf("Bad cmd"))
	}
}
