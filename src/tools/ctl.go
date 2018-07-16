package main

import (
	"encoding/json"
	"encoding/base64"
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
	AdmHost		string		`yaml:"admhost,omitempty"`
	AdmPort		string		`yaml:"admport,omitempty"`
	Relay		string		`yaml:"relay,omitempty"`
}

func (li *LoginInfo)HostPort() string {
	if !curCmd.adm {
		return li.Host + ":" + li.Port
	}

	if li.AdmHost == "" {
		fatal(fmt.Errorf("Admd not set for this command"))
	}

	return li.AdmHost + ":" + li.AdmPort
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
	address := gateProto() + "://" + conf.Login.HostPort() + "/v1/" + url

	h := make(map[string]string)
	if conf.Login.Token != "" {
		h["X-Auth-Token"] = conf.Login.Token
	}
	if curCmd.relay != "" {
		h["X-Relay-Tennant"] = curCmd.relay
	} else if conf.Login.Relay != "" {
		h["X-Relay-Tennant"] = conf.Login.Relay
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

func user_list(args []string, opts [16]string) {
	var uss []swyapi.UserInfo
	make_faas_req("users", swyapi.ListUsers{}, &uss)

	for _, u := range uss {
		fmt.Printf("%s (%s)\n", u.Id, u.Name)
	}
}

func user_add(args []string, opts [16]string) {
	make_faas_req2("POST", "adduser", swyapi.AddUser{Id: args[0], Pass: opts[1], Name: opts[0]},
		http.StatusCreated, 0)
}

func user_del(args []string, opts [16]string) {
	make_faas_req2("POST", "deluser", swyapi.UserInfo{Id: args[0]},
		http.StatusNoContent, 0)
}

func user_pass(args []string, opts [16]string) {
	make_faas_req2("POST", "setpass", swyapi.UserLogin{UserName: args[0], Password: opts[0]},
		http.StatusCreated, 0)
}

func user_info(args []string, opts [16]string) {
	var ui swyapi.UserInfo
	make_faas_req("userinfo", swyapi.UserInfo{Id: args[0]}, &ui)
	fmt.Printf("Name: %s\n", ui.Name)
}

func user_limits(args []string, opts [16]string) {
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

func show_stats(args []string, opts [16]string) {
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

func list_projects(args []string, opts [16]string) {
	var ps []swyapi.ProjectItem
	make_faas_req("project/list", swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func resolve_fn(fname string) string {
	if strings.HasPrefix(fname, ":") {
		return fname[1:]
	}

	var ifo []swyapi.FunctionInfo
	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}
	ua = append(ua, "name=" + fname)
	make_faas_req1("GET", url("functions", ua), http.StatusOK, nil, &ifo)
	for _, i := range ifo {
		if i.Name == fname {
			return i.Id
		}
	}

	fatal(fmt.Errorf("\tname %s not resolved", fname))
	return ""
}

func resolve_mw(mname string) string {
	if strings.HasPrefix(mname, ":") {
		return mname[1:]
	}

	var ifo []swyapi.MwareInfo
	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}
	ua = append(ua, "name=" + mname)
	make_faas_req1("GET", url("middleware", ua), http.StatusOK, nil, &ifo)
	for _, i := range ifo {
		if i.Name == mname {
			return i.ID
		}
	}

	fatal(fmt.Errorf("\tname %s not resolved", mname))
	return ""
}

func resolve_dep(dname string) string {
	if strings.HasPrefix(dname, ":") {
		return dname[1:]
	}

	var ifo []swyapi.DeployInfo
	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}
	ua = append(ua, "name=" + dname)
	make_faas_req1("GET", url("deployments", ua), http.StatusOK, nil, &ifo)
	for _, i := range ifo {
		if i.Name == dname {
			return i.Id
		}
	}

	fatal(fmt.Errorf("\tname %s not resolved", dname))
	return ""
}

func resolve_evt(fnid, name string) string {
	if strings.HasPrefix(name, ":") {
		return name[1:]
	}

	var es []swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + fnid + "/triggers?name=" + name, http.StatusOK,  nil, &es)
	for _, e := range es {
		if e.Name == name {
			return e.Id
		}
	}

	fatal(fmt.Errorf("\tname %s not resolved", name))
	return ""
}

func function_list(args []string, opts [16]string) {
	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}

	if opts[1] != "" {
		for _, l := range strings.Split(opts[1], ",") {
			ua = append(ua, "label=" + l)
		}
	}

	if opts[0] == "" {
		var fns []swyapi.FunctionInfo
		make_faas_req1("GET", url("functions", ua), http.StatusOK, nil, &fns)

		fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "STATE")
		for _, fn := range fns {
			fmt.Printf("%-32s%-20s%-12s%s\n", fn.Id, fn.Name, fn.State, strings.Join(fn.Labels, ","))
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

func function_info(args []string, opts [16]string) {
	var ifo swyapi.FunctionInfo
	args[0] = resolve_fn(args[0])
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

	var minf []*swyapi.MwareInfo
	make_faas_req1("GET", "functions/" + args[0] + "/middleware", http.StatusOK, nil, &minf)
	if len(minf) != 0 {
		fmt.Printf("Mware:\n")
		for _, mi := range minf {
			fmt.Printf("\t%20s %-10s(id:%s)\n", mi.Name, mi.Type, mi.ID)
		}
	}

	var bkts []string
	make_faas_req1("GET", "functions/" + args[0] + "/s3buckets", http.StatusOK, nil, &bkts)
	if len(bkts) != 0 {
		fmt.Printf("Buckets:\n")
		for _, bkt := range bkts {
			fmt.Printf("\t%20s\n", bkt)
		}
	}
}

func function_minfo(args []string, opts [16]string) {
	var ifo swyapi.FunctionMdat
	args[0] = resolve_fn(args[0])
	make_faas_req1("GET", "functions/" + args[0] + "/mdat", http.StatusOK, nil, &ifo)
	if len(ifo.RL) != 0 {
		fmt.Printf("RL: %d/%d (%d left)\n", ifo.RL[1], ifo.RL[2], ifo.RL[0])
	}
	if len(ifo.BR) != 0 {
		fmt.Printf("BR: %d:%d -> %d\n", ifo.BR[0], ifo.BR[1], ifo.BR[2])
	}
}

func check_lang(args []string, opts [16]string) {
	l := detect_language(opts[0], "code")
	fmt.Printf("%s\n", l)
}

func check_ext(path, ext, typ string) string {
	if strings.HasSuffix(path, ext) {
		return typ
	}

	fatal(fmt.Errorf("%s lang detected, but extention is not %s", typ, ext))
	return ""
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

	rur := regexp.MustCompile("^def\\s+Main\\s*\\(.*\\)[^:]*$")
	pyr := regexp.MustCompile("^def\\s+Main\\s*\\(.*\\):")
	gor := regexp.MustCompile("^func\\s+Main\\s*\\(.*interface\\s*{\\s*}")
	swr := regexp.MustCompile("^func\\s+Main\\s*\\(.*->\\s*Encodable")
	jsr := regexp.MustCompile("^exports.Main\\s*=\\s*function")

	lines := strings.Split(string(cont), "\n")
	for _, ln := range(lines) {
		if rur.MatchString(ln) {
			return check_ext(path, ".rb", "ruby")
		}

		if pyr.MatchString(ln) {
			return check_ext(path, ".py", "python")
		}

		if gor.MatchString(ln) {
			return check_ext(path, ".go", "golang")
		}

		if swr.MatchString(ln) {
			return check_ext(path, ".swift", "swift")
		}

		if jsr.MatchString(ln) {
			return check_ext(path, ".js", "nodejs")
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

func function_add(args []string, opts [16]string) {
	var err error

	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

	if strings.HasPrefix(opts[1], "repo:") {
		fmt.Printf("Will add file from repo %s\n", opts[1][5:])
		sources.Type = "git"
		sources.Repo = opts[1][5:]
	} else if strings.HasPrefix(opts[1], "sw:") {
		s := strings.Split(opts[1], ":")
		var sw swyapi.FunctionSwage

		sw.Template = s[1]
		sw.Params = make(map[string]string)
		for _, p := range s[2:] {
			ps := strings.SplitN(p, "=", 2)
			sw.Params[ps[0]] = ps[1]
		}

		sources.Type = "swage"
		sources.Swage = &sw
	} else {
		st, err := os.Stat(opts[1])
		if err != nil {
			fatal(fmt.Errorf("Can't stat sources path"))
		}

		if st.IsDir() {
			fatal(fmt.Errorf("Can't add dir as source"))
		}

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
		Project: curCmd.project,
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

	if !curCmd.req {
		var fid string
		make_faas_req1("POST", "functions", http.StatusOK, req, &fid)
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

func run_function(args []string, opts [16]string) {
	var rres swyapi.FunctionRunResult

	args[0] = resolve_fn(args[0])
	argmap := split_args_string(args[1])
	make_faas_req1("POST", "functions/" + args[0] + "/run", http.StatusOK, &swyapi.FunctionRun{ Args: argmap, }, &rres)

	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func function_update(args []string, opts [16]string) {
	fid := resolve_fn(args[0])

	if opts[0] != "" {
		make_faas_req1("PUT", "functions/" + fid + "/sources", http.StatusOK,
			&swyapi.FunctionSources{Type: "code", Code: encodeFile(opts[0])}, nil)
	}

	if opts[3] != "" {
		mid := resolve_mw(opts[3][1:])
		if opts[3][0] == '+' {
			make_faas_req1("POST", "functions/" + fid + "/middleware", http.StatusOK, mid, nil)
		} else if opts[3][0] == '-' {
			make_faas_req1("DELETE", "functions/" + fid + "/middleware/" + mid, http.StatusOK, nil, nil)
		} else {
			fatal(fmt.Errorf("+/- mware name"))
		}
	}

	if opts[8] != "" {
		if opts[8][0] == '+' {
			make_faas_req1("POST", "functions/" + fid + "/s3buckets", http.StatusOK, opts[8][1:], nil)
		} else if opts[8][0] == '-' {
			make_faas_req1("DELETE", "functions/" + fid + "/s3buckets/" + opts[8][1:], http.StatusOK, nil, nil)
		} else {
			fatal(fmt.Errorf("+/- bucket name"))
		}
	}

	if opts[4] != "" {
		make_faas_req1("PUT", "functions/" + fid, http.StatusOK,
				&swyapi.FunctionUpdate{UserData: &opts[4]}, nil)
	}

	if opts[7] != "" {
		var ac string
		if opts[7] != "-" {
			ac = opts[7]
		}
		make_faas_req1("PUT", "functions/" + fid + "/authctx", http.StatusOK, ac, nil)
	}

	if opts[1] != "" || opts[2] != "" {
		sz := swyapi.FunctionSize{}

		if opts[1] != "" {
			var err error

			sz.Timeout, err = strconv.ParseUint(opts[1], 10, 64)
			if err != nil {
				fatal(fmt.Errorf("Bad tmo value %s: %s", opts[4], err.Error()))
			}
		}
		if opts[2] != "" {
			sz.Rate, sz.Burst = parse_rate(opts[2])
		}

		make_faas_req1("PUT", "functions/" + fid + "/size", http.StatusOK, &sz, nil)
	}

	if opts[5] != "" {
		fmt.Printf("Wait FN %s\n", opts[5])
		function_wait([]string{args[0]}, [16]string{opts[5], "15000"})
	}

	if opts[6] != "" {
		fmt.Printf("Run FN %s\n", opts[6])
		run_function([]string{args[0], opts[6]}, [16]string{})
	}
}

func function_del(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	make_faas_req1("DELETE", "functions/" + args[0], http.StatusOK, nil, nil)
}

func function_on(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	make_faas_req1("PUT", "functions/" + args[0], http.StatusOK,
			&swyapi.FunctionUpdate{State: "ready"}, nil)
}

func function_off(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	make_faas_req1("PUT", "functions/" + args[0], http.StatusOK,
			&swyapi.FunctionUpdate{State: "deactivated"}, nil)
}

func event_list(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	var eds []swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/triggers", http.StatusOK,  nil, &eds)
	for _, e := range eds {
		fmt.Printf("%16s%20s%8s\n", e.Id, e.Name, e.Source)
	}
}

func event_add(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
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
	make_faas_req1("POST", "functions/" + args[0] + "/triggers", http.StatusOK, &e, &res)
	fmt.Printf("Event %s created\n", res)
}

func event_info(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	args[1] = resolve_evt(args[0], args[1])
	var e swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/triggers/" + args[1], http.StatusOK,  nil, &e)
	fmt.Printf("Name:          %s\n", e.Name)
	fmt.Printf("Source:        %s\n", e.Source)
	if e.Cron != nil {
		fmt.Printf("Tab:           %s\n", e.Cron.Tab)
		fmt.Printf("Args:          %s\n", make_args_string(e.Cron.Args))
	}
	if e.S3 != nil {
		fmt.Printf("Bucket:        %s\n", e.S3.Bucket)
		fmt.Printf("Ops:           %s\n", e.S3.Ops)
	}
	if e.URL != "" {
		fmt.Printf("URL:           %s\n", e.URL)
	}
}

func event_del(args []string, opts [16]string) {
	args[0] = resolve_fn(args[0])
	args[1] = resolve_evt(args[0], args[1])
	make_faas_req1("DELETE", "functions/" + args[0] + "/triggers/" + args[1], http.StatusOK, nil, nil)
}

func function_wait(args []string, opts [16]string) {
	var wo swyapi.FunctionWait
	if opts[0] != "" {
		wo.Version = opts[0]
	}
	if opts[1] != "" {
		t, err := strconv.Atoi(opts[1])
		if err != nil || t < 0 {
			fatal(fmt.Errorf("Bad timeout value"))
		}
		wo.Timeout = uint(t)
	}

	args[0] = resolve_fn(args[0])
	make_faas_req2("POST", "functions/" + args[0] + "/wait", &wo, http.StatusOK, 300)
}

func function_code(args []string, opts [16]string) {
	var res swyapi.FunctionSources
	args[0] = resolve_fn(args[0])
	make_faas_req1("GET", "functions/" + args[0] + "/sources", http.StatusOK, nil, &res)
	data, err := base64.StdEncoding.DecodeString(res.Code)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s", data)
}

func function_logs(args []string, opts [16]string) {
	var res []swyapi.FunctionLogEntry
	args[0] = resolve_fn(args[0])

	fa := []string{}
	if opts[0] != "" {
		fa = append(fa, "last=" + opts[0])
	}

	make_faas_req1("GET", url("functions/" + args[0] + "/logs", fa), http.StatusOK, nil, &res)

	for _, le := range res {
		fmt.Printf("%36s%12s: %s\n", le.Ts, le.Event, le.Text)
	}
}

func url(url string, args []string) string {
	if len(args) != 0 {
		url += "?" + strings.Join(args, "&")
	}
	return url
}

func mware_list(args []string, opts [16]string) {
	var mws []swyapi.MwareInfo
	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}
	if opts[1] != "" {
		ua = append(ua, "type=" + opts[1])
	}
	if opts[2] != "" {
		for _, l := range strings.Split(opts[2], ",") {
			ua = append(ua, "label=" + l)
		}
	}

	if opts[0] == "" {
		make_faas_req1("GET", url("middleware", ua), http.StatusOK, nil, &mws)
		fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "TYPE")
		for _, mw := range mws {
			fmt.Printf("%-32s%-20s%-10s%s\n", mw.ID, mw.Name, mw.Type, strings.Join(mw.Labels, ","))
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

func mware_info(args []string, opts [16]string) {
	var resp swyapi.MwareInfo

	args[0] = resolve_mw(args[0])
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

func mware_add(args []string, opts [16]string) {
	req := swyapi.MwareAdd {
		Name: args[0],
		Project: curCmd.project,
		Type: args[1],
		UserData: opts[0],
	}

	if !curCmd.req {
		var id string
		make_faas_req1("POST", "middleware", http.StatusOK, &req, &id)
		fmt.Printf("Mware %s created\n", id)
	} else {
		d, err := json.Marshal(req)
		if err == nil {
			fmt.Printf("%s\n", string(d))
		}
	}
}

func mware_del(args []string, opts [16]string) {
	args[0] = resolve_mw(args[0])
	make_faas_req1("DELETE", "middleware/" + args[0], http.StatusOK, nil, nil)
}

func auth_cfg(args []string, opts [16]string) {
	switch args[0] {
	case "get", "inf":
		var auths []*swyapi.AuthInfo
		make_faas_req1("GET", "auths", http.StatusOK, nil, &auths)
		for _, a := range auths {
			fmt.Printf("%s (%s)\n", a.Name, a.Id)
		}

	case "on":
		var did string
		name := opts[0]
		if name == "" {
			name = "simple_auth"
		}
		make_faas_req1("POST", "auths", http.StatusOK, &swyapi.AuthAdd { Name: name }, &did)
		fmt.Printf("Created %s auth\n", did)

	case "off":
		var auths []*swyapi.AuthInfo
		make_faas_req1("GET", "auths", http.StatusOK, nil, &auths)
		for _, a := range auths {
			if opts[0] != "" && a.Name != opts[0] {
				continue
			}

			fmt.Printf("Shutting down aut %s\n", a.Name)
			make_faas_req1("DELETE", "auths/" + a.Id, http.StatusOK, nil, nil)
		}
	}
}

func deploy_del(args []string, opts [16]string) {
	args[0] = resolve_dep(args[0])
	make_faas_req1("DELETE", "deployments/" + args[0], http.StatusOK, nil, nil)
}

func deploy_info(args []string, opts [16]string) {
	var di swyapi.DeployInfo
	args[0] = resolve_dep(args[0])
	make_faas_req1("GET", "deployments/" + args[0], http.StatusOK, nil, &di)
	fmt.Printf("State:        %s\n", di.State)
	fmt.Printf("Items:\n")
	for _, i := range di.Items {
		fmt.Printf("\t%s: %s, %s\n", i.Name, i.Type, i.State)
	}
}

func deploy_list(args []string, opts [16]string) {
	var dis []*swyapi.DeployInfo
	ua := []string{}
	if opts[0] != "" {
		for _, l := range strings.Split(opts[0], ",") {
			ua = append(ua, "label=" + l)
		}
	}
	make_faas_req1("GET", url("deployments", ua), http.StatusOK, nil, &dis)
	fmt.Printf("%-32s%-20s\n", "ID", "NAME")
	for _, di := range dis {
		fmt.Printf("%-32s%-20s (%d items) %s\n", di.Id, di.Name, len(di.Items), strings.Join(di.Labels, ","))
	}
}

func deploy_add(args []string, opts [16]string) {
	cont, err := ioutil.ReadFile(args[1])
	if err != nil {
		fatal(fmt.Errorf("Can't read desc flie: %s", err.Error()))
	}

	var items []*swyapi.DeployItem
	err = json.Unmarshal(cont, &items)
	if err != nil {
		fatal(fmt.Errorf("Can't parse items: %s", err.Error()))
	}

	make_faas_req1("POST", "deployments", http.StatusOK,
		swyapi.DeployStart{ Name: args[0], Project: curCmd.project, Items: items}, nil)
}

func repo_list(args []string, opts [16]string) {
	var ris []*swyapi.RepoInfo
	make_faas_req1("GET", "repos", http.StatusOK, nil, &ris)
	fmt.Printf("%-32s%-12s%s\n", "ID", "STATE", "URL")
	for _, ri := range ris {
		fmt.Printf("%-32s%-12s%s\n", ri.ID, ri.State, ri.URL)
	}
}

func repo_list_files(args []string, opts [16]string) {
	var fl []string
	make_faas_req1("GET", "repos/" + args[0] + "/files", http.StatusOK, nil, &fl)
	for _, f := range fl {
		fmt.Printf("%s\n", f)
	}
}

func repo_info(args []string, opts [16]string) {
	var ri swyapi.RepoInfo
	make_faas_req1("GET", "repos/" + args[0], http.StatusOK, nil, &ri)
	fmt.Printf("State:     %s\n", ri.State)
	fmt.Printf("URL:       %s\n", ri.URL)
	if ri.Commit != "" {
		fmt.Printf("Commit:    %s\n", ri.Commit)
	}
}

func repo_add(args []string, opts [16]string) {
	ra := swyapi.RepoAdd {
		URL:		args[0],
		Type:		"github",
	}
	var id string
	make_faas_req1("POST", "repos", http.StatusOK, &ra, &id)
	fmt.Printf("%s repo attached\n", id)
}

func repo_del(args []string, opts [16]string) {
	make_faas_req1("DELETE", "repos/" + args[0], http.StatusOK, nil, nil)
}

func acc_list(args []string, opts [16]string) {
	var ais []*swyapi.AccInfo
	make_faas_req1("GET", "accounts", http.StatusOK, nil, &ais)
	fmt.Printf("%-32s%-12s\n", "ID", "TYPE")
	for _, ai := range ais {
		fmt.Printf("%-32s%-12s\n", ai.ID, ai.Type)
	}
}

func acc_info(args []string, opts [16]string) {
	var ai swyapi.AccInfo
	make_faas_req1("GET", "accounts/" + args[0], http.StatusOK, nil, &ai)
	fmt.Printf("Type:        %s\n", ai.Type)
	if ai.GHName != "" {
		fmt.Printf("GitHub name: %s\n", ai.GHName)
	}
}

func acc_add(args []string, opts [16]string) {
	aa := swyapi.AccAdd {
		Type:	args[0],
	}

	if opts[0] != "" {
		aa.GHName = opts[0]
	}

	var id string
	make_faas_req1("POST", "accounts", http.StatusOK, &aa, &id)
	fmt.Printf("%s account created\n", id)
}

func acc_del(args []string, opts [16]string) {
	make_faas_req1("DELETE", "accounts/" + args[0], http.StatusOK, nil, nil)
}

func s3_access(args []string, opts [16]string) {
	acc := swyapi.S3Access {
		Project: curCmd.project,
		Lifetime: uint32(60),
	}

	if opts[0] != "" {
		lt, err := strconv.Atoi(opts[0])
		if err != nil {
			fatal(fmt.Errorf("Bad lifetie value: %s", err.Error()))
		}
		acc.Lifetime = uint32(lt)
	}

	if args[0] != "/" {
		acc.Bucket = args[0]
	}

	var creds swyapi.S3Creds

	make_faas_req1("POST", "s3/access", http.StatusOK, acc, &creds)

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

func languages(args []string, opts [16]string) {
	req_list("info/langs")
}

func mware_types(args []string, opts [16]string) {
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
	if conf.Login.AdmHost != "" {
		fmt.Printf("\tadmd: %s:%s\n", conf.Login.AdmHost, conf.Login.AdmPort)
	}
	if conf.Login.Relay != "" {
		fmt.Printf("\tfor %s\n", conf.Login.Relay)
	}
}

func set_relay_tenant(rt string) {
	if rt == "-" {
		rt = ""
	}

	conf.Login.Relay = rt
	save_config("")
}

func manage_login(args []string, opts [16]string) {
	action := "show"
	if len(args) >= 1 {
		action = args[0]
	}

	switch action {
	case "show":
		show_login()
	case "for":
		set_relay_tenant(args[1])
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

	if opts[2] != "" {
		c := strings.Split(opts[2], ":")
		conf.Login.AdmHost = c[0]
		conf.Login.AdmPort = c[1]
	}

	refresh_token(home)
}

func refresh_token(home string) {
	conf.Login.Token = faas_login()
	save_config(home)
}

func save_config(home string) {
	if home == "" {
		var found bool
		home, found = os.LookupEnv("HOME")
		if !found {
			fatal(fmt.Errorf("No HOME dir set"))
		}
	}

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

	CMD_RUN string		= "run"

	CMD_FL string		= "fl"
	CMD_FI string		= "fi"
	CMD_FIM string		= "fim"
	CMD_FA string		= "fa"
	CMD_FD string		= "fd"
	CMD_FU string		= "fu"
	CMD_FLOG string		= "flog"
	CMD_FCOD string		= "fcod"
	CMD_FON string		= "fon"
	CMD_FOFF string		= "foff"
	CMD_FW string		= "fw"

	CMD_EL string		= "el"
	CMD_EI string		= "ei"
	CMD_EA string		= "ea"
	CMD_ED string		= "ed"

	CMD_ML string		= "ml"
	CMD_MI string		= "mi"
	CMD_MA string		= "ma"
	CMD_MD string		= "md"

	CMD_S3ACC string	= "s3acc"
	CMD_AUTH string		= "auth"

	CMD_DL string		= "dl"
	CMD_DI string		= "di"
	CMD_DA string		= "da"
	CMD_DD string		= "dd"

	CMD_RL string		= "rl"
	CMD_RI string		= "ri"
	CMD_RA string		= "ra"
	CMD_RD string		= "rd"
	CMD_RLS string		= "rls"

	CMD_AL string		= "al"
	CMD_AI string		= "ai"
	CMD_AA string		= "aa"
	CMD_AD string		= "ad"

	CMD_UL string		= "ul"
	CMD_UI string		= "ui"
	CMD_UA string		= "ua"
	CMD_UD string		= "ud"
	CMD_UPASS string	= "upass"
	CMD_ULIM string		= "ulim"

	CMD_MTYPES string	= "mt"
	CMD_LANGS string	= "lng"
	CMD_LANG string		= "ld"
)

var cmdOrder = []string {
	CMD_LOGIN,
	CMD_ME,
	CMD_STATS,
	CMD_PS,

	CMD_RUN,

	CMD_FL,
	CMD_FI,
	CMD_FIM,
	CMD_FA,
	CMD_FD,
	CMD_FU,
	CMD_FON,
	CMD_FOFF,
	CMD_FW,
	CMD_FLOG,
	CMD_FCOD,

	CMD_EL,
	CMD_EI,
	CMD_EA,
	CMD_ED,

	CMD_ML,
	CMD_MI,
	CMD_MA,
	CMD_MD,

	CMD_S3ACC,
	CMD_AUTH,

	CMD_DL,
	CMD_DI,
	CMD_DA,
	CMD_DD,

	CMD_RL,
	CMD_RI,
	CMD_RA,
	CMD_RD,
	CMD_RLS,

	CMD_AL,
	CMD_AI,
	CMD_AD,
	CMD_AA,

	CMD_UL,
	CMD_UI,
	CMD_UA,
	CMD_UD,
	CMD_UPASS,
	CMD_ULIM,
	CMD_LANGS,
	CMD_MTYPES,
	CMD_LANG,
}

type cmdDesc struct {
	opts	*flag.FlagSet
	pargs	[]string
	project	string
	relay	string
	req	bool
	adm	bool
	call	func([]string, [16]string)
}

var curCmd *cmdDesc

var cmdMap = map[string]*cmdDesc {
	CMD_LOGIN:	&cmdDesc{			  opts: flag.NewFlagSet(CMD_LOGIN, flag.ExitOnError) },
	CMD_ME:		&cmdDesc{ call: manage_login,	  opts: flag.NewFlagSet(CMD_ME, flag.ExitOnError) },
	CMD_STATS:	&cmdDesc{ call: show_stats,	  opts: flag.NewFlagSet(CMD_STATS, flag.ExitOnError) },
	CMD_PS:		&cmdDesc{ call: list_projects,	  opts: flag.NewFlagSet(CMD_PS, flag.ExitOnError) },
	CMD_FL:		&cmdDesc{ call: function_list,	  opts: flag.NewFlagSet(CMD_FL, flag.ExitOnError) },
	CMD_FI:		&cmdDesc{ call: function_info,	  opts: flag.NewFlagSet(CMD_FI, flag.ExitOnError) },
	CMD_FIM:	&cmdDesc{ call: function_minfo,	  opts: flag.NewFlagSet(CMD_FIM, flag.ExitOnError) },
	CMD_FA:		&cmdDesc{ call: function_add,	  opts: flag.NewFlagSet(CMD_FA, flag.ExitOnError) },
	CMD_FD:		&cmdDesc{ call: function_del,	  opts: flag.NewFlagSet(CMD_FD, flag.ExitOnError) },
	CMD_FU:		&cmdDesc{ call: function_update,  opts: flag.NewFlagSet(CMD_FU, flag.ExitOnError) },
	CMD_RUN:	&cmdDesc{ call: run_function,	  opts: flag.NewFlagSet(CMD_RUN, flag.ExitOnError) },
	CMD_FLOG:	&cmdDesc{ call: function_logs,	  opts: flag.NewFlagSet(CMD_FLOG, flag.ExitOnError) },
	CMD_FCOD:	&cmdDesc{ call: function_code,	  opts: flag.NewFlagSet(CMD_FCOD, flag.ExitOnError) },
	CMD_FON:	&cmdDesc{ call: function_on,	  opts: flag.NewFlagSet(CMD_FON, flag.ExitOnError) },
	CMD_FOFF:	&cmdDesc{ call: function_off,	  opts: flag.NewFlagSet(CMD_FOFF, flag.ExitOnError) },
	CMD_FW:		&cmdDesc{ call: function_wait,	  opts: flag.NewFlagSet(CMD_FW, flag.ExitOnError) },
	CMD_EL:		&cmdDesc{ call: event_list,	  opts: flag.NewFlagSet(CMD_EL, flag.ExitOnError) },
	CMD_EA:		&cmdDesc{ call: event_add,	  opts: flag.NewFlagSet(CMD_EA, flag.ExitOnError) },
	CMD_EI:		&cmdDesc{ call: event_info,	  opts: flag.NewFlagSet(CMD_EI, flag.ExitOnError) },
	CMD_ED:		&cmdDesc{ call: event_del,	  opts: flag.NewFlagSet(CMD_ED, flag.ExitOnError) },
	CMD_ML:		&cmdDesc{ call: mware_list,	  opts: flag.NewFlagSet(CMD_ML, flag.ExitOnError) },
	CMD_MI:		&cmdDesc{ call: mware_info,	  opts: flag.NewFlagSet(CMD_MI, flag.ExitOnError) },
	CMD_MA:		&cmdDesc{ call: mware_add,	  opts: flag.NewFlagSet(CMD_MA, flag.ExitOnError) },
	CMD_MD:		&cmdDesc{ call: mware_del,	  opts: flag.NewFlagSet(CMD_MD, flag.ExitOnError) },
	CMD_S3ACC:	&cmdDesc{ call: s3_access,	  opts: flag.NewFlagSet(CMD_S3ACC, flag.ExitOnError) },
	CMD_AUTH:	&cmdDesc{ call: auth_cfg,	  opts: flag.NewFlagSet(CMD_AUTH, flag.ExitOnError) },

	CMD_DL:		&cmdDesc{ call: deploy_list,	  opts: flag.NewFlagSet(CMD_DL, flag.ExitOnError) },
	CMD_DI:		&cmdDesc{ call: deploy_info,	  opts: flag.NewFlagSet(CMD_DI, flag.ExitOnError) },
	CMD_DA:		&cmdDesc{ call: deploy_add,	  opts: flag.NewFlagSet(CMD_DA, flag.ExitOnError) },
	CMD_DD:		&cmdDesc{ call: deploy_del,	  opts: flag.NewFlagSet(CMD_DD, flag.ExitOnError) },

	CMD_RL:		&cmdDesc{ call: repo_list,	  opts: flag.NewFlagSet(CMD_RL, flag.ExitOnError) },
	CMD_RI:		&cmdDesc{ call: repo_info,	  opts: flag.NewFlagSet(CMD_RI, flag.ExitOnError) },
	CMD_RA:		&cmdDesc{ call: repo_add,	  opts: flag.NewFlagSet(CMD_RA, flag.ExitOnError) },
	CMD_RD:		&cmdDesc{ call: repo_del,	  opts: flag.NewFlagSet(CMD_RD, flag.ExitOnError) },
	CMD_RLS:	&cmdDesc{ call: repo_list_files,  opts: flag.NewFlagSet(CMD_RLS, flag.ExitOnError) },

	CMD_AL:		&cmdDesc{ call: acc_list,	  opts: flag.NewFlagSet(CMD_AL, flag.ExitOnError) },
	CMD_AI:		&cmdDesc{ call: acc_info,	  opts: flag.NewFlagSet(CMD_AI, flag.ExitOnError) },
	CMD_AA:		&cmdDesc{ call: acc_add,	  opts: flag.NewFlagSet(CMD_AA, flag.ExitOnError) },
	CMD_AD:		&cmdDesc{ call: acc_del,	  opts: flag.NewFlagSet(CMD_AD, flag.ExitOnError) },

	CMD_UL:		&cmdDesc{ call: user_list,	  opts: flag.NewFlagSet(CMD_UL, flag.ExitOnError), adm: true },
	CMD_UI:		&cmdDesc{ call: user_info,	  opts: flag.NewFlagSet(CMD_UI, flag.ExitOnError), adm: true },
	CMD_UA:		&cmdDesc{ call: user_add,	  opts: flag.NewFlagSet(CMD_UA, flag.ExitOnError), adm: true },
	CMD_UD:		&cmdDesc{ call: user_del,	  opts: flag.NewFlagSet(CMD_UD, flag.ExitOnError), adm: true },
	CMD_UPASS:	&cmdDesc{ call: user_pass,	  opts: flag.NewFlagSet(CMD_UPASS, flag.ExitOnError), adm: true },
	CMD_ULIM:	&cmdDesc{ call: user_limits,	  opts: flag.NewFlagSet(CMD_ULIM, flag.ExitOnError), adm: true },

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
	cd.opts.StringVar(&cd.relay, "for", "", "Act as another user (admin-only")

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
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[2], "admd", "", "Admd address:port")
	bindCmdUsage(CMD_LOGIN,	[]string{"USER:PASS@HOST:PORT"}, "Login into the system", false)

	bindCmdUsage(CMD_ME, []string{"ACTION"}, "Manage login", false)

	cmdMap[CMD_STATS].opts.StringVar(&opts[0], "p", "0", "Periods to report")
	bindCmdUsage(CMD_STATS,	[]string{}, "Show stats", false)
	bindCmdUsage(CMD_PS,	[]string{}, "List projects", false)

	cmdMap[CMD_FL].opts.StringVar(&opts[0], "o", "", "Output format (NONE, json)")
	cmdMap[CMD_FL].opts.StringVar(&opts[1], "label", "", "Labels, comma-separated")
	bindCmdUsage(CMD_FL,	[]string{}, "List functions", true)
	bindCmdUsage(CMD_FI,	[]string{"NAME"}, "Function info", true)
	bindCmdUsage(CMD_FIM,	[]string{"NAME"}, "Function memdat info", true)
	cmdMap[CMD_FA].opts.StringVar(&opts[0], "lang", "auto", "Language")
	cmdMap[CMD_FA].opts.StringVar(&opts[1], "src", ".", "Source file")
	cmdMap[CMD_FA].opts.StringVar(&opts[2], "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_FA].opts.StringVar(&opts[4], "tmo", "", "Timeout")
	cmdMap[CMD_FA].opts.StringVar(&opts[5], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_FA].opts.StringVar(&opts[6], "data", "", "Any text associated with fn")
	cmdMap[CMD_FA].opts.StringVar(&opts[7], "env", "", "Colon-separated list of env vars")
	cmdMap[CMD_FA].opts.StringVar(&opts[8], "auth", "", "ID of auth mware to verify the call")
	bindCmdUsage(CMD_FA,	[]string{"NAME"}, "Add a function", true)
	bindCmdUsage(CMD_RUN,	[]string{"NAME", "ARG=VAL,..."}, "Run a function", true)
	cmdMap[CMD_FU].opts.StringVar(&opts[0], "src", "", "Source file")
	cmdMap[CMD_FU].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	cmdMap[CMD_FU].opts.StringVar(&opts[2], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_FU].opts.StringVar(&opts[3], "mw", "", "Mware to use, +/- to add/remove")
	cmdMap[CMD_FU].opts.StringVar(&opts[4], "data", "", "Associated text")
	cmdMap[CMD_FU].opts.StringVar(&opts[5], "ver", "", "Version")
	cmdMap[CMD_FU].opts.StringVar(&opts[6], "arg", "", "Args")
	cmdMap[CMD_FU].opts.StringVar(&opts[7], "auth", "", "Auth context (- for off)")
	cmdMap[CMD_FU].opts.StringVar(&opts[8], "s3b", "", "Bucket to use, +/- to add/remove")
	bindCmdUsage(CMD_FU,	[]string{"NAME"}, "Update a function", true)
	bindCmdUsage(CMD_FD,	[]string{"NAME"}, "Delete a function", true)
	cmdMap[CMD_FLOG].opts.StringVar(&opts[0], "last", "", "Last N 'duration' period")
	bindCmdUsage(CMD_FLOG,	[]string{"NAME"}, "Show function logs", true)
	bindCmdUsage(CMD_FCOD,  []string{"NAME"}, "Show function code", true)
	bindCmdUsage(CMD_FON,	[]string{"NAME"}, "Activate function", true)
	bindCmdUsage(CMD_FOFF,	[]string{"NAME"}, "Deactivate function", true)

	cmdMap[CMD_FW].opts.StringVar(&opts[0], "version", "", "Version")
	cmdMap[CMD_FW].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	bindCmdUsage(CMD_FW,	[]string{"NAME"}, "Wait function event", true)

	bindCmdUsage(CMD_EL,	[]string{"NAME"}, "List events for a function", true)
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "tab", "", "Cron tab")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "args", "", "Cron args")
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "buck", "", "S3 bucket")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "ops", "", "S3 ops")
	bindCmdUsage(CMD_EA,	[]string{"NAME", "ENAME", "SRC"}, "Add event", true)
	bindCmdUsage(CMD_EI,	[]string{"NAME", "ENAME"}, "Show event info", true)
	bindCmdUsage(CMD_ED,	[]string{"NAME", "ENAME"}, "Remove event", true)

	cmdMap[CMD_ML].opts.StringVar(&opts[0], "o", "", "Output format (NONE, json)")
	cmdMap[CMD_ML].opts.StringVar(&opts[1], "type", "", "Filter mware by type")
	cmdMap[CMD_ML].opts.StringVar(&opts[2], "label", "", "Labels, comma-separated")
	bindCmdUsage(CMD_ML,	[]string{}, "List middleware", true)
	bindCmdUsage(CMD_MI,	[]string{"NAME"}, "Middleware info", true)
	cmdMap[CMD_MA].opts.StringVar(&opts[0], "data", "", "Associated text")
	bindCmdUsage(CMD_MA,	[]string{"NAME", "TYPE"}, "Add middleware", true)
	bindCmdUsage(CMD_MD,	[]string{"NAME"}, "Delete middleware", true)

	cmdMap[CMD_S3ACC].opts.StringVar(&opts[0], "life", "60", "Lifetime (default 1 min)")
	bindCmdUsage(CMD_S3ACC,	[]string{"BUCKET"}, "Get keys for S3", true)
	cmdMap[CMD_AUTH].opts.StringVar(&opts[0], "name", "", "Name for auth")
	bindCmdUsage(CMD_AUTH,	[]string{"ACTION"}, "Manage project auth", true)

	cmdMap[CMD_DL].opts.StringVar(&opts[0], "label", "", "Labels, comma-separated")
	bindCmdUsage(CMD_DL,	[]string{},	"List deployments", true)
	bindCmdUsage(CMD_DI,	[]string{"NAME"}, "Show info about deployment", true)
	bindCmdUsage(CMD_DA,	[]string{"NAME", "DESC"}, "Add (start) deployment", true)
	bindCmdUsage(CMD_DD,	[]string{"NAME"}, "Del (stop) deployment", true)

	bindCmdUsage(CMD_RL,	[]string{},	"List repos", false)
	bindCmdUsage(CMD_RI,	[]string{"ID"}, "Show info about repo", false)
	bindCmdUsage(CMD_RA,	[]string{"URL"}, "Attach repo", false)
	bindCmdUsage(CMD_RD,	[]string{"ID"}, "Detach repo", false)
	bindCmdUsage(CMD_RLS,	[]string{"ID"}, "List files in repo", false)

	bindCmdUsage(CMD_AL,	[]string{},	"List accounts", false)
	bindCmdUsage(CMD_AI,	[]string{"ID"}, "Show info about account", false)
	cmdMap[CMD_AA].opts.StringVar(&opts[0], "name", "", "GitHub name")
	bindCmdUsage(CMD_AA,	[]string{"TYPE"}, "Add account", false)
	bindCmdUsage(CMD_AD,	[]string{"ID"}, "Delete account", false)

	bindCmdUsage(CMD_UL,	[]string{}, "List users", false)
	cmdMap[CMD_UA].opts.StringVar(&opts[0], "name", "", "User name")
	cmdMap[CMD_UA].opts.StringVar(&opts[1], "pass", "", "User password")
	bindCmdUsage(CMD_UA,	[]string{"UID"}, "Add user", false)
	bindCmdUsage(CMD_UD,	[]string{"UID"}, "Del user", false)
	cmdMap[CMD_UPASS].opts.StringVar(&opts[0], "pass", "", "New password")
	bindCmdUsage(CMD_UPASS,	[]string{"UID"}, "Set password", false)
	bindCmdUsage(CMD_UI,	[]string{"UID"}, "Get user info", false)
	cmdMap[CMD_ULIM].opts.StringVar(&opts[0], "plan", "", "Taroff plan ID")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[1], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[2], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[3], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[4], "bo", "", "Maximum outgoing network bytes")
	bindCmdUsage(CMD_ULIM, []string{"UID"}, "Get/Set limits for user", false)

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
		curCmd = cd
		make_login(os.Args[2], opts)
		return
	}

	login()

	if cd.call != nil {
		curCmd = cd
		cd.call(os.Args[2:], opts)
	} else {
		fatal(fmt.Errorf("Bad cmd"))
	}
}
