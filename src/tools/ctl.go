package main

import (
	"encoding/json"
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"strings"
	"strconv"
	"reflect"
	"regexp"
	"time"
	"flag"
	"fmt"
	"os"

	"../common"
	"../apis"
)

var swyclient *swyapi.Client

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

type YAMLConf struct {
	Login		LoginInfo	`yaml:"login"`
	TLS		bool		`yaml:"tls"`
	Direct		bool		`yaml:"direct"`
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

func make_faas_req1(method, url string, succ int, in interface{}, out interface{}) {
	err := swyclient.Req1(method, url, succ, in, out)
	if err != nil {
		fatal(err)
	}
}

func make_faas_req2(method, url string, in interface{}, succ_code int, tmo uint) *http.Response {
	resp, err := swyclient.Req2(method, url, in, succ_code, tmo)
	if err != nil {
		fatal(err)
	}
	return resp
}

func user_list(args []string, opts [16]string) {
	var uss []swyapi.UserInfo
	make_faas_req1("GET", "users", http.StatusOK, nil, &uss)

	for _, u := range uss {
		en := ""
		if u.Created != "" {
			en += " since " + u.Created
		}
		if !u.Enabled {
			en += " [X]"
		}
		fmt.Printf("%s: %s (%s)%s\n", u.ID, u.UId, u.Name, en)
	}
}

func user_add(args []string, opts [16]string) {
	var ui swyapi.UserInfo
	make_faas_req1("POST", "users", http.StatusCreated, &swyapi.AddUser{UId: args[0], Pass: opts[1], Name: opts[0]}, &ui)
	fmt.Printf("%s user created\n", ui.ID)
}

func user_del(args []string, opts [16]string) {
	make_faas_req1("DELETE", "users/" + args[0], http.StatusNoContent, nil, nil)
}

func user_enabled(args []string, opts [16]string) {
	var enabled bool
	if args[1] == "0" {
		enabled = false
	} else if args[1] == "1" {
		enabled = true
	} else {
		fatal(fmt.Errorf("Bad enable status"))
	}

	make_faas_req1("PUT", "users/" + args[0], http.StatusOK, &swyapi.ModUser{Enabled: &enabled}, nil)
}

func user_pass(args []string, opts [16]string) {
	make_faas_req1("PUT", "users/" + args[0] + "/pass", http.StatusCreated,
			&swyapi.UserLogin{Password: opts[0]}, nil)
}

func user_info(args []string, opts [16]string) {
	var ui swyapi.UserInfo
	make_faas_req1("GET", "users/" + args[0], http.StatusOK, nil, &ui)
	fmt.Printf("ID:      %s\n", ui.ID)
	fmt.Printf("Name:    %s\n", ui.Name)
	fmt.Printf("Roles:   %s\n", strings.Join(ui.Roles, ", "))
	fmt.Printf("Created: %s\n", ui.Created)
	if !ui.Enabled {
		fmt.Printf("!!! disabled\n")
	}
}

func tplan_list(args []string, opts[16]string) {
	var plans []*swyapi.PlanLimits
	make_faas_req1("GET", "plans", http.StatusOK, nil, &plans)
	for _, p := range(plans) {
		fmt.Printf("%s/%s:\n", p.Id, p.Name)
		show_fn_limits(p.Fn)
	}
}

func tplan_add(args []string, opts[16]string) {
	var l swyapi.PlanLimits

	l.Name = args[0]
	l.Fn = parse_limits(opts[0:])
	if l.Fn == nil {
		fatal(fmt.Errorf("No limits"))
	}
	make_faas_req1("POST", "plans", http.StatusCreated, &l, &l)
	fmt.Printf("%s plan created\n", l.Id)
}

func tplan_info(args []string, opts[16]string) {
	var p swyapi.PlanLimits
	make_faas_req1("GET", "plans/" + args[0], http.StatusOK, nil, &p)
	fmt.Printf("%s/%s:\n", p.Id, p.Name)
	show_fn_limits(p.Fn)
}

func tplan_del(args []string, opts[16]string) {
	make_faas_req1("DELETE", "plans/" + args[0], http.StatusNoContent, nil, nil)
}

func user_limits(args []string, opts [16]string) {
	var l swyapi.UserLimits

	if opts[0] != "" {
		l.PlanId = opts[0]
	}

	l.Fn = parse_limits(opts[1:])

	if l.Fn != nil || l.PlanId != "" {
		l.UId = args[0]
		make_faas_req1("PUT", "users/" + args[0] + "/limits", http.StatusOK, &l, nil)
	} else {
		make_faas_req1("GET", "users/" + args[0] + "/limits", http.StatusOK, nil, &l)
		if l.PlanId != "" {
			fmt.Printf("Plan ID: %s\n", l.PlanId)
		}
		show_fn_limits(l.Fn)
		fmt.Printf(">>> %s\n", l.UId)
	}
}

func parse_limits(opts []string) *swyapi.FunctionLimits {
	var ret *swyapi.FunctionLimits

	if opts[1] != "" {
		if ret == nil {
			ret = &swyapi.FunctionLimits{}
		}
		ret.Rate, ret.Burst = parse_rate(opts[1])
	}

	if opts[2] != "" {
		if ret == nil {
			ret = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseUint(opts[2], 10, 32)
		if err != nil {
			fatal(fmt.Errorf("Bad max-fn value %s: %s", opts[2], err.Error()))
		}
		ret.MaxInProj = uint(v)
	}

	if opts[3] != "" {
		if ret == nil {
			ret = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseFloat(opts[3], 64)
		if err != nil {
			fatal(fmt.Errorf("Bad GBS value %s: %s", opts[3], err.Error()))
		}
		ret.GBS = v
	}

	if opts[4] != "" {
		if ret == nil {
			ret = &swyapi.FunctionLimits{}
		}
		v, err := strconv.ParseUint(opts[4], 10, 64)
		if err != nil {
			fatal(fmt.Errorf("Bad bytes-out value %s: %s", opts[4], err.Error()))
		}
		ret.BytesOut = v
	}

	return ret
}

func show_fn_limits(fl *swyapi.FunctionLimits) {
	if fl != nil {
		fmt.Printf("Functions:\n")
		if fl.Rate != 0 {
			fmt.Printf("    Rate:              %d:%d\n", fl.Rate, fl.Burst)
		}
		if fl.MaxInProj != 0 {
			fmt.Printf("    Max in project:    %d\n", fl.MaxInProj)
		}
		if fl.GBS != 0 {
			fmt.Printf("    Max GBS:           %f\n", fl.GBS)
		}
		if fl.BytesOut != 0 {
			fmt.Printf("    Max bytes out:     %d\n", formatBytes(fl.BytesOut))
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
	var st swyapi.TenantStatsResp

	ua := []string{}
	if opts[0] != "" {
		ua = append(ua, "periods=" + opts[0])
	}

	make_faas_req1("GET", url("stats", ua), http.StatusOK, nil, &st)

	fmt.Printf("*********** Calls ***********\n")
	for _, s := range(st.Stats) {
		fmt.Printf("---\n%s ... %s\n", dateOnly(s.From), dateOnly(s.Till))
		fmt.Printf("Called:           %d\n", s.Called)
		fmt.Printf("GBS:              %f\n", s.GBS)
		fmt.Printf("Bytes sent:       %s\n", formatBytes(s.BytesOut))
	}

	fmt.Printf("*********** Mware ***********\n")
	for mt, st := range(st.Mware) {
		fmt.Printf("* %s:\n", mt)
		fmt.Printf("  Count:        %d\n", st.Count)
		if st.DU != nil {
			fmt.Printf("  Disk usage:   %s\n", formatBytes(*st.DU << 10))
		}
	}
}

func list_projects(args []string, opts [16]string) {
	var ps []swyapi.ProjectItem
	make_faas_req1("POST", "project/list", http.StatusOK, swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func resolve_name(name string, path string, objs interface{}) (string, bool) {
	if strings.HasPrefix(name, ":") {
		return name[1:], false
	}

	ua := []string{}
	if curCmd.project != "" {
		ua = append(ua, "project=" + curCmd.project)
	}

	ua = append(ua, "name=" + name)
	make_faas_req1("GET", url(path, ua), http.StatusOK, nil, objs)

	items := reflect.ValueOf(objs).Elem()
	for i := 0; i < items.Len(); i++ {
		obj := reflect.Indirect(items.Index(i))
		n := obj.FieldByName("Name").Interface().(string)
		if n == name {
			id := obj.FieldByName("Id")
			if id.IsValid() {
				return id.Interface().(string), true
			}
			id = obj.FieldByName("ID")
			if id.IsValid() {
				return id.Interface().(string), true
			}
		}
	}


	fatal(fmt.Errorf("\tname %s not resolved", name))
	return "", false
}
func resolve_fn(fname string) (string, bool) {
	var ifo []swyapi.FunctionInfo
	return resolve_name(fname, "functions", &ifo)
}

func resolve_mw(mname string) (string, bool) {
	var ifo []swyapi.MwareInfo
	return resolve_name(mname, "middleware", &ifo)
}

func resolve_dep(dname string) (string, bool) {
	var ifo []swyapi.DeployInfo
	return resolve_name(dname, "deployments", &ifo)
}

func resolve_router(rname string) (string, bool) {
	var ifo []swyapi.RouterInfo
	return resolve_name(rname, "routers", &ifo)
}

func resolve_evt(fnid, name string) (string, bool) {
	var es []swyapi.FunctionEvent
	return resolve_name(name, "functions/" + fnid + "/triggers", &es)
}

type node struct {
	name	string
	id	string
	kids	[]*node
}

func show_names(pfx string, n *node) {
	if n.name != "/" {
		if n.id == "" {
			fmt.Printf("%24s", "")
		} else {
			fmt.Printf("%s", n.id)
		}
		fmt.Printf("  %s%s\n", pfx, n.name)
		pfx += "    "
	}
	for _, k := range n.kids {
		show_names(pfx, k)
	}
}

func show_fn_tree(fns []*swyapi.FunctionInfo) {
	root := node{name:"/", kids:[]*node{}}
	for _, fn := range fns {
		n := &root
		path := strings.Split(fn.Name, ".")
		for _, p := range path {
			var tn *node
			for _, c := range n.kids {
				if c.name == p {
					tn = c
					break
				}
			}
			if tn == nil {
				tn = &node{name: p, kids:[]*node{}}
				n.kids = append(n.kids, tn)
			}
			n = tn
		}
		n.id = fn.Id
	}

	show_names("", &root)
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
	if opts[2] != "" {
		ua = append(ua, "prefix=" + opts[2])
	}

	var fns []*swyapi.FunctionInfo
	make_faas_req1("GET", url("functions", ua), http.StatusOK, nil, &fns)

	switch opts[0] {
	case "tree":
		show_fn_tree(fns)
	default:
		fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "STATE")
		for _, fn := range fns {
			fmt.Printf("%-32s%-20s%-12s%s\n", fn.Id, fn.Name, fn.State, strings.Join(fn.Labels, ","))
		}
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
	var r bool
	args[0], r = resolve_fn(args[0])
	make_faas_req1("GET", "functions/" + args[0], http.StatusOK, nil, &ifo)
	ver := ifo.Version
	if len(ver) > 8 {
		ver = ver[:8]
	}

	if !r {
		fmt.Printf("Name:        %s\n", ifo.Name)
	}
	fmt.Printf("Lang:        %s\n", ifo.Code.Lang)

	rv := ""
	if len(ifo.RdyVersions) != 0 {
		rv = " (" + strings.Join(ifo.RdyVersions, ",") + ")"
	}
	fmt.Printf("Version:     %s%s\n", ver, rv)
	fmt.Printf("State:       %s\n", ifo.State)
	if ifo.URL != "" {
		pfx := ""
		if !(strings.HasPrefix(ifo.URL, "http://") || strings.HasPrefix(ifo.URL, "https://")) {
			pfx = gateProto() + "://"
		}
		fmt.Printf("URL:         %s%s\n", pfx, ifo.URL)
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

	var src swyapi.FunctionSources
	make_faas_req1("GET", "functions/" + args[0] + "/sources", http.StatusOK, nil, &src)
	if src.Sync {
		fmt.Printf("Sync with:   %s\n", src.Repo)
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

	var acs []map[string]string
	make_faas_req1("GET", "functions/" + args[0] + "/accounts", http.StatusOK, nil, &acs)
	if len(acs) != 0 {
		fmt.Printf("Accounts:\n")
		for _, ac := range acs {
			fmt.Printf("\t%s:%s\n", ac["id"], ac["type"])
		}
	}

	var env []string
	make_faas_req1("GET", "functions/" + args[0] + "/env", http.StatusOK, nil, &env)
	if len(env) != 0 {
		fmt.Printf("Environment:\n")
		for _, ev := range env {
			fmt.Printf("\t%s\n", ev)
		}
	}
}

func function_minfo(args []string, opts [16]string) {
	var ifo swyapi.FunctionMdat
	args[0], _ = resolve_fn(args[0])
	make_faas_req1("GET", "functions/" + args[0] + "/mdat", http.StatusOK, nil, &ifo)
	fmt.Printf("Cookie: %s\n", ifo.Cookie)
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

func getSrc(opt string, src *swyapi.FunctionSources) {
	if strings.HasPrefix(opt, "repo:") {
		sync := false
		repo := opt[5:]
		fmt.Printf("[%s]\n", repo)
		if repo[0] == '!' {
			fmt.Printf("SYNC\n")
			sync = true
			repo = repo[1:]
		}
		fmt.Printf("Will add file from repo %s (sync %v)\n", repo, sync)
		src.Type = "git"
		src.Repo = repo
		src.Sync = sync
	} else {
		st, err := os.Stat(opt)
		if err != nil {
			fatal(fmt.Errorf("Can't stat sources path " + opt))
		}

		if st.IsDir() {
			fatal(fmt.Errorf("Can't add dir as source"))
		}

		fmt.Printf("Will add file %s\n", opt)
		src.Type = "code"
		src.Code = encodeFile(opt)
	}
}

func function_add(args []string, opts [16]string) {
	var err error

	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

	getSrc(opts[1], &sources)

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

	var fi swyapi.FunctionInfo
	make_faas_req1("POST", "functions", http.StatusOK, req, &fi)
	fmt.Printf("Function %s created\n", fi.Id)
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
	var rres swyapi.SwdFunctionRunResult

	rq := &swyapi.SwdFunctionRun{}

	args[0], _ = resolve_fn(args[0])
	rq.Args = split_args_string(args[1])

	if opts[0] != "" {
		src := &swyapi.FunctionSources{}
		src.Type = "code"
		src.Code = encodeFile(opts[0])
		rq.Src = src
	}

	make_faas_req1("POST", "functions/" + args[0] + "/run", http.StatusOK, rq, &rres)

	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func function_update(args []string, opts [16]string) {
	fid, _ := resolve_fn(args[0])

	if opts[0] != "" {
		var src swyapi.FunctionSources

		getSrc(opts[0], &src)
		make_faas_req1("PUT", "functions/" + fid + "/sources", http.StatusOK, &src, nil)
	}

	if opts[3] != "" {
		mid, _ := resolve_mw(opts[3][1:])
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

	if opts[9] != "" {
		if opts[9][0] == '+' {
			make_faas_req1("POST", "functions/" + fid + "/accounts", http.StatusOK, opts[9][1:], nil)
		} else if opts[9][0] == '-' {
			make_faas_req1("DELETE", "functions/" + fid + "/accounts/" + opts[9][1:], http.StatusOK, nil, nil)
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

	if opts[10] != "" {
		envs := strings.Split(opts[10], ":")
		make_faas_req1("PUT", "functions/" + fid + "/env", http.StatusOK, envs, nil)
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
	args[0], _ = resolve_fn(args[0])
	make_faas_req1("DELETE", "functions/" + args[0], http.StatusOK, nil, nil)
}

func function_on(args []string, opts [16]string) {
	args[0], _ = resolve_fn(args[0])
	make_faas_req1("PUT", "functions/" + args[0], http.StatusOK,
			&swyapi.FunctionUpdate{State: "ready"}, nil)
}

func function_off(args []string, opts [16]string) {
	args[0], _ = resolve_fn(args[0])
	make_faas_req1("PUT", "functions/" + args[0], http.StatusOK,
			&swyapi.FunctionUpdate{State: "deactivated"}, nil)
}

func event_list(args []string, opts [16]string) {
	args[0], _ = resolve_fn(args[0])
	var eds []swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/triggers", http.StatusOK,  nil, &eds)
	for _, e := range eds {
		fmt.Printf("%16s%20s%8s\n", e.Id, e.Name, e.Source)
	}
}

func event_add(args []string, opts [16]string) {
	args[0], _ = resolve_fn(args[0])
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
	var ei swyapi.FunctionEvent
	make_faas_req1("POST", "functions/" + args[0] + "/triggers", http.StatusOK, &e, &ei)
	fmt.Printf("Event %s created\n", ei.Id)
}

func event_info(args []string, opts [16]string) {
	var r bool
	args[0], _ = resolve_fn(args[0])
	args[1], r = resolve_evt(args[0], args[1])
	var e swyapi.FunctionEvent
	make_faas_req1("GET", "functions/" + args[0] + "/triggers/" + args[1], http.StatusOK,  nil, &e)
	if !r {
		fmt.Printf("Name:          %s\n", e.Name)
	}
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
	args[0], _ = resolve_fn(args[0])
	args[1], _ = resolve_evt(args[0], args[1])
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

	args[0], _ = resolve_fn(args[0])
	make_faas_req2("POST", "functions/" + args[0] + "/wait", &wo, http.StatusOK, 300)
}

func function_code(args []string, opts [16]string) {
	var res swyapi.FunctionSources
	args[0], _ = resolve_fn(args[0])
	make_faas_req1("GET", "functions/" + args[0] + "/sources", http.StatusOK, nil, &res)
	data, err := base64.StdEncoding.DecodeString(res.Code)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s", data)
}

func function_logs(args []string, opts [16]string) {
	var res []swyapi.FunctionLogEntry
	args[0], _ = resolve_fn(args[0])

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

	make_faas_req1("GET", url("middleware", ua), http.StatusOK, nil, &mws)
	fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "TYPE")
	for _, mw := range mws {
		fmt.Printf("%-32s%-20s%-10s%s\n", mw.ID, mw.Name, mw.Type, strings.Join(mw.Labels, ","))
	}
}

func mware_info(args []string, opts [16]string) {
	var resp swyapi.MwareInfo
	var r bool

	args[0], r = resolve_mw(args[0])
	make_faas_req1("GET", "middleware/" + args[0], http.StatusOK, nil, &resp)
	if !r {
		fmt.Printf("Name:         %s\n", resp.Name)
	}
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

	var mi swyapi.MwareInfo
	make_faas_req1("POST", "middleware", http.StatusOK, &req, &mi)
	fmt.Printf("Mware %s created\n", mi.ID)
}

func mware_del(args []string, opts [16]string) {
	args[0], _ = resolve_mw(args[0])
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
		var di swyapi.DeployInfo
		name := opts[0]
		if name == "" {
			name = "simple_auth"
		}
		make_faas_req1("POST", "auths", http.StatusOK, &swyapi.AuthAdd { Name: name }, &di)
		fmt.Printf("Created %s auth\n", di.Id)

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
	args[0], _ = resolve_dep(args[0])
	make_faas_req1("DELETE", "deployments/" + args[0], http.StatusOK, nil, nil)
}

func deploy_info(args []string, opts [16]string) {
	var di swyapi.DeployInfo
	args[0], _ = resolve_dep(args[0])
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

	var dd swyapi.DeployStart
	err = json.Unmarshal(cont, &dd)
	if err != nil {
		fatal(fmt.Errorf("Can't parse items: %s", err.Error()))
	}

	dd.Name = args[0]
	dd.Project = curCmd.project

	var di swyapi.DeployInfo
	make_faas_req1("POST", "deployments", http.StatusOK, &dd, &di)
	fmt.Printf("%s deployment started\n", di.Id)
}

func router_list(args []string, opts [16]string) {
	var rts []swyapi.RouterInfo
	make_faas_req1("GET", "routers", http.StatusOK, nil, &rts)
	for _, rt := range rts {
		fmt.Printf("%s %12s %s\n", rt.Id, rt.Name, rt.URL)
	}
}

func parse_route_table(opt string) []*swyapi.RouterEntry {
	res := []*swyapi.RouterEntry{}
	ents := strings.Split(opt, ";")
	for _, e := range ents {
		ee := strings.SplitN(e, ":", 4)
		res = append(res, &swyapi.RouterEntry {
			Method:	ee[0],
			Path:	ee[1],
			Call:	ee[2],
			Key:	ee[3],
		})
	}
	return res
}

func router_add(args []string, opts [16]string) {
	ra := swyapi.RouterAdd {
		Name: args[0],
		Project: curCmd.project,
	}
	if opts[0] != "" {
		ra.Table = parse_route_table(opts[0])
	}
	var ri swyapi.RouterInfo
	make_faas_req1("POST", "routers", http.StatusOK, &ra, &ri)
	fmt.Printf("Router %s created\n", ri.Id)
}

func router_info(args []string, opts [16]string) {
	args[0], _ = resolve_router(args[0])
	var ri swyapi.RouterInfo
	make_faas_req1("GET", "routers/" + args[0], http.StatusOK, nil, &ri)
	fmt.Printf("URL:      %s\n", ri.URL)
	fmt.Printf("Table:    (%d ents)\n", ri.TLen)
	var res []*swyapi.RouterEntry
	make_faas_req1("GET", "routers/" + args[0] + "/table", http.StatusOK, nil, &res)
	for _, re := range res {
		fmt.Printf("   %8s /%-32s -> %s\n", re.Method, re.Path, re.Call)
	}
}

func router_upd(args []string, opts [16]string) {
	args[0], _ = resolve_router(args[0])
	if opts[0] != "" {
		rt := parse_route_table
		make_faas_req1("PUT", "routers/" + args[0] + "/table", http.StatusOK, rt, nil)
	}
}

func router_del(args []string, opts [16]string) {
	args[0], _ = resolve_router(args[0])
	make_faas_req1("DELETE", "routers/" + args[0], http.StatusOK, nil, nil)
}

func repo_list(args []string, opts [16]string) {
	var ris []*swyapi.RepoInfo
	ua := []string{}
	if opts[0] != "" {
		ua = append(ua, "aid=" + opts[0])
	}
	if opts[1] != "" {
		ua = append(ua, "attached=" + opts[1])
	}
	make_faas_req1("GET", url("repos", ua), http.StatusOK, nil, &ris)
	fmt.Printf("%-32s%-8s%-12s%s\n", "ID", "TYPE", "STATE", "URL")
	for _, ri := range ris {
		t := ri.Type
		if ri.AccID != "" {
			t += "*"
		}

		url := ri.URL
		if ri.ID == "" && ri.AccID != "" {
			url += "(" + ri.AccID + ")"
		}

		fmt.Printf("%-32s%-8s%-12s%s\n", ri.ID, t, ri.State, url)
	}
}

func show_files(pref string, fl []*swyapi.RepoFile, pty string) {
	for _, f := range fl {
		l := ""
		if f.Type == "file" && f.Lang != nil {
			l = " (" + *f.Lang + ")"
		}
		if pty == "tree" {
			fmt.Printf("%s%s\n", pref, f.Label + l)
		} else if f.Type == "file" {
			fmt.Printf("%s\n", f.Path + l)
		}
		if f.Type == "dir" {
			show_files(pref + "  ", *f.Children, pty)
		}
	}
}

func repo_desc(args []string, opts [16]string) {
	var d swyapi.RepoDesc
	make_faas_req1("GET", "repos/" + args[0] + "/desc", http.StatusOK, nil, &d)
	fmt.Printf("%s\n", d.Description)
	for _, e := range d.Entries {
		fmt.Printf("%s: %s\n", e.Name, e.Description)
		fmt.Printf("    %s (%s)\n", e.Path, e.Lang)
	}
}

func repo_list_files(args []string, opts [16]string) {
	if opts[0] == "desc" {
		repo_desc(args, opts)
		return
	}

	var fl []*swyapi.RepoFile
	make_faas_req1("GET", "repos/" + args[0] + "/files", http.StatusOK, nil, &fl)
	show_files("", fl, opts[0])
}

func repo_cat_file(args []string, opts [16]string) {
	p := strings.SplitN(args[0], "/", 2)
	resp := make_faas_req2("GET", "repos/" + p[0] + "/files/" + p[1], nil, http.StatusOK, 0)
	dat, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fatal(fmt.Errorf("Can't read file: %s", err.Error()))
	}
	fmt.Printf(string(dat))
}

func repo_pull(args []string, opts [16]string) {
	make_faas_req1("POST", "repos/" + args[0] + "/pull", http.StatusOK, nil, nil)
}

func repo_info(args []string, opts [16]string) {
	var ri swyapi.RepoInfo
	make_faas_req1("GET", "repos/" + args[0], http.StatusOK, nil, &ri)
	fmt.Printf("State:     %s\n", ri.State)
	fmt.Printf("Type:      %s\n", ri.Type)
	fmt.Printf("URL:       %s\n", ri.URL)
	fmt.Printf("Pull:      %s\n", ri.Pull)
	if ri.Commit != "" {
		fmt.Printf("Commit:    %s\n", ri.Commit)
	}
	if ri.AccID != "" {
		fmt.Printf("Account:   %s\n", ri.AccID)
	}

	if ri.Desc {
		fmt.Printf("With description\n")
	}
}

func repo_add(args []string, opts [16]string) {
	ra := swyapi.RepoAdd {
		URL:		args[0],
		Type:		"github",
	}
	if opts[0] != "" {
		ra.AccID = opts[0]
	}
	if opts[1] != "" {
		ra.Pull = opts[1]
	}

	var ri swyapi.RepoInfo
	make_faas_req1("POST", "repos", http.StatusOK, &ra, &ri)
	fmt.Printf("%s repo attached\n", ri.ID)
}

func repo_upd(args []string, opts [16]string) {
	ra := swyapi.RepoUpdate {}
	if opts[0] != "" {
		if opts[0] == "-" {
			opts[0] = ""
		}
		ra.Pull = &opts[0]
	}

	make_faas_req1("PUT", "repos/" + args[0], http.StatusOK, &ra, nil)
}

func repo_del(args []string, opts [16]string) {
	make_faas_req1("DELETE", "repos/" + args[0], http.StatusOK, nil, nil)
}

func acc_list(args []string, opts [16]string) {
	var ais []map[string]string
	ua := []string{}
	if opts[0] != "" {
		ua = append(ua, "type=" + opts[0])
	}
	make_faas_req1("GET", url("accounts", ua), http.StatusOK, nil, &ais)
	fmt.Printf("%-32s%-12s\n", "ID", "TYPE")
	for _, ai := range ais {
		fmt.Printf("%-32s%-12s\n", ai["id"], ai["type"])
	}
}

func acc_info(args []string, opts [16]string) {
	var ai map[string]string
	make_faas_req1("GET", "accounts/" + args[0], http.StatusOK, nil, &ai)
	fmt.Printf("Type:           %s\n", ai["type"])
	fmt.Printf("Name:           %s\n", ai["name"])
	for k, v := range(ai) {
		if k == "id" || k == "name" || k =="type" {
			continue
		}

		fmt.Printf("%-16s%s\n", strings.Title(k)+":", v)
	}
}

func acc_add(args []string, opts [16]string) {
	if args[1] == "-" {
		args[1] = ""
	}

	aa := map[string]string {
		"type": args[0],
		"name": args[1],
	}

	if opts[0] != "" {
		for _, kv := range(strings.Split(opts[0], ":")) {
			kvs := strings.SplitN(kv, "=", 2)
			aa[kvs[0]] = kvs[1]
		}
	}

	var ai map[string]string
	make_faas_req1("POST", "accounts", http.StatusOK, &aa, &ai)
	fmt.Printf("%s account created\n", ai["id"])
}

func acc_upd(args []string, opts [16]string) {
	au := map[string]string{}

	if opts[0] != "" {
		for _, kv := range(strings.Split(opts[1], ":")) {
			kvs := strings.SplitN(kv, "=", 2)
			au[kvs[0]] = kvs[1]
		}
	}

	make_faas_req1("PUT", "accounts/" + args[0], http.StatusOK, &au, nil)
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
	fmt.Printf("AccID:   %s\n", creds.AccID)
}

func languages(args []string, opts [16]string) {
	var ls []string
	make_faas_req1("GET", "info/langs", http.StatusOK, nil, &ls)
	for _, l := range(ls) {
		var li swyapi.LangInfo
		fmt.Printf("%s\n", l)
		make_faas_req1("GET", "info/langs/" + l, http.StatusOK, nil , &li)
		fmt.Printf("\tversion: %s\n", li.Version)
		fmt.Printf("\tpackages:\n")
		for _, p := range(li.Packages) {
			fmt.Printf("\t\t%s\n", p)
		}
	}
}

func mware_types(args []string, opts [16]string) {
	var r []string

	make_faas_req1("GET", "info/mwares", http.StatusOK, nil, &r)
	for _, v := range r {
		fmt.Printf("%s\n", v)
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

	mkClient()
}

func show_login() {
	fmt.Printf("%s@%s:%s (%s)\n", conf.Login.User, conf.Login.Host, conf.Login.Port, gateProto())
	if conf.Login.AdmHost != "" || conf.Login.AdmPort != "" {
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
	save_config()
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

func mkClient() {
	swyclient = swyapi.MakeClient(conf.Login.User, conf.Login.Pass, conf.Login.Host, conf.Login.Port)
	if curCmd.adm {
		swyclient.Admd(conf.Login.AdmHost, conf.Login.AdmPort)
		swyclient.ToAdmd(true)
	}
	if curCmd.relay != "" {
		swyclient.Relay(curCmd.relay)
	} else if conf.Login.Relay != "" {
		swyclient.Relay(conf.Login.Relay)
	}
	if curCmd.verb {
		swyclient.Verbose()
	}
	if !conf.TLS {
		swyclient.NoTLS()
	}
	if conf.Direct {
		swyclient.Direct()
	}
	swyclient.TokSaver(func(tok string) { conf.Login.Token = tok; save_config() })

	/* Guy can be cached in config */
	swyclient.Token(conf.Login.Token)
}

func make_login(creds string, opts [16]string) {
	//
	// Login string is user:pass@host:port
	//
	// swifty.user:swifty@10.94.96.216:8686
	//

	c := swy.ParseXCreds(creds)
	conf.Login.User = c.User
	conf.Login.Pass = c.Pass
	conf.Login.Host = c.Host
	conf.Login.Port = c.Port

	if opts[0] == "no" {
		conf.TLS = false
	} else {
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

	if opts[3] == "no" {
		conf.Direct = true
	} else {
		conf.Direct = false
	}

	mkClient()

	err := swyclient.Login()
	if err != nil {
		fatal(err)
	}
}

func save_config() {
	home, found := os.LookupEnv("HOME")
	if !found {
		fatal(fmt.Errorf("No HOME dir set"))
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

	CMD_RTL string		= "rtl"
	CMD_RTI string		= "rti"
	CMD_RTA string		= "rta"
	CMD_RTU string		= "rtu"
	CMD_RTD string		= "rtd"

	CMD_RL string		= "rl"
	CMD_RI string		= "ri"
	CMD_RA string		= "ra"
	CMD_RU string		= "ru"
	CMD_RD string		= "rd"
	CMD_RLS string		= "rls"
	CMD_RCAT string		= "rcat"
	CMD_RP string		= "rp"

	CMD_AL string		= "al"
	CMD_AI string		= "ai"
	CMD_AA string		= "aa"
	CMD_AD string		= "ad"
	CMD_AU string		= "au"

	CMD_UL string		= "ul"
	CMD_UI string		= "ui"
	CMD_UA string		= "ua"
	CMD_UD string		= "ud"
	CMD_UPASS string	= "upass"
	CMD_ULIM string		= "ulim"
	CMD_UEN string		= "uen"

	CMD_TL string		= "tl"
	CMD_TA string		= "ta"
	CMD_TI string		= "ti"
	CMD_TD string		= "td"

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

	CMD_RTL,
	CMD_RTI,
	CMD_RTA,
	CMD_RTU,
	CMD_RTD,

	CMD_RL,
	CMD_RI,
	CMD_RA,
	CMD_RU,
	CMD_RD,
	CMD_RLS,
	CMD_RCAT,
	CMD_RP,

	CMD_AL,
	CMD_AA,
	CMD_AI,
	CMD_AD,
	CMD_AU,

	CMD_UL,
	CMD_UI,
	CMD_UA,
	CMD_UD,
	CMD_UPASS,
	CMD_ULIM,
	CMD_UEN,

	CMD_TL,
	CMD_TA,
	CMD_TI,
	CMD_TD,

	CMD_LANGS,
	CMD_MTYPES,
	CMD_LANG,
}

type cmdDesc struct {
	opts	*flag.FlagSet
	pargs	[]string
	project	string
	relay	string
	verb	bool
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

	CMD_RTL:	&cmdDesc{ call: router_list,	  opts: flag.NewFlagSet(CMD_RTL, flag.ExitOnError) },
	CMD_RTI:	&cmdDesc{ call: router_info,	  opts: flag.NewFlagSet(CMD_RTI, flag.ExitOnError) },
	CMD_RTA:	&cmdDesc{ call: router_add,	  opts: flag.NewFlagSet(CMD_RTA, flag.ExitOnError) },
	CMD_RTU:	&cmdDesc{ call: router_upd,	  opts: flag.NewFlagSet(CMD_RTU, flag.ExitOnError) },
	CMD_RTD:	&cmdDesc{ call: router_del,	  opts: flag.NewFlagSet(CMD_RTD, flag.ExitOnError) },

	CMD_RL:		&cmdDesc{ call: repo_list,	  opts: flag.NewFlagSet(CMD_RL, flag.ExitOnError) },
	CMD_RI:		&cmdDesc{ call: repo_info,	  opts: flag.NewFlagSet(CMD_RI, flag.ExitOnError) },
	CMD_RA:		&cmdDesc{ call: repo_add,	  opts: flag.NewFlagSet(CMD_RA, flag.ExitOnError) },
	CMD_RU:		&cmdDesc{ call: repo_upd,	  opts: flag.NewFlagSet(CMD_RU, flag.ExitOnError) },
	CMD_RD:		&cmdDesc{ call: repo_del,	  opts: flag.NewFlagSet(CMD_RD, flag.ExitOnError) },
	CMD_RLS:	&cmdDesc{ call: repo_list_files,  opts: flag.NewFlagSet(CMD_RLS, flag.ExitOnError) },
	CMD_RCAT:	&cmdDesc{ call: repo_cat_file,	  opts: flag.NewFlagSet(CMD_RCAT, flag.ExitOnError) },
	CMD_RP:		&cmdDesc{ call: repo_pull,	  opts: flag.NewFlagSet(CMD_RP, flag.ExitOnError) },

	CMD_AL:		&cmdDesc{ call: acc_list,	  opts: flag.NewFlagSet(CMD_AL, flag.ExitOnError) },
	CMD_AI:		&cmdDesc{ call: acc_info,	  opts: flag.NewFlagSet(CMD_AI, flag.ExitOnError) },
	CMD_AA:		&cmdDesc{ call: acc_add,	  opts: flag.NewFlagSet(CMD_AA, flag.ExitOnError) },
	CMD_AD:		&cmdDesc{ call: acc_del,	  opts: flag.NewFlagSet(CMD_AD, flag.ExitOnError) },
	CMD_AU:		&cmdDesc{ call: acc_upd,	  opts: flag.NewFlagSet(CMD_AU, flag.ExitOnError) },

	CMD_UL:		&cmdDesc{ call: user_list,	  opts: flag.NewFlagSet(CMD_UL, flag.ExitOnError), adm: true },
	CMD_UI:		&cmdDesc{ call: user_info,	  opts: flag.NewFlagSet(CMD_UI, flag.ExitOnError), adm: true },
	CMD_UA:		&cmdDesc{ call: user_add,	  opts: flag.NewFlagSet(CMD_UA, flag.ExitOnError), adm: true },
	CMD_UD:		&cmdDesc{ call: user_del,	  opts: flag.NewFlagSet(CMD_UD, flag.ExitOnError), adm: true },
	CMD_UPASS:	&cmdDesc{ call: user_pass,	  opts: flag.NewFlagSet(CMD_UPASS, flag.ExitOnError), adm: true },
	CMD_UEN:	&cmdDesc{ call: user_enabled,	  opts: flag.NewFlagSet(CMD_UEN, flag.ExitOnError), adm: true },
	CMD_ULIM:	&cmdDesc{ call: user_limits,	  opts: flag.NewFlagSet(CMD_ULIM, flag.ExitOnError), adm: true },

	CMD_TL:		&cmdDesc{ call: tplan_list,	  opts: flag.NewFlagSet(CMD_TL, flag.ExitOnError), adm: true },
	CMD_TA:		&cmdDesc{ call: tplan_add,	  opts: flag.NewFlagSet(CMD_TA, flag.ExitOnError), adm: true },
	CMD_TI:		&cmdDesc{ call: tplan_info,	  opts: flag.NewFlagSet(CMD_TI, flag.ExitOnError), adm: true },
	CMD_TD:		&cmdDesc{ call: tplan_del,	  opts: flag.NewFlagSet(CMD_TD, flag.ExitOnError), adm: true },

	CMD_LANGS:	&cmdDesc{ call: languages,	  opts: flag.NewFlagSet(CMD_LANGS, flag.ExitOnError) },
	CMD_MTYPES:	&cmdDesc{ call: mware_types,	  opts: flag.NewFlagSet(CMD_MTYPES, flag.ExitOnError) },
	CMD_LANG:	&cmdDesc{ call: check_lang,	  opts: flag.NewFlagSet(CMD_LANG, flag.ExitOnError) },
}

func bindCmdUsage(cmd string, args []string, help string, wp bool) {
	cd := cmdMap[cmd]
	if wp {
		cd.opts.StringVar(&cd.project, "proj", "", "Project to work on")
	}
	cd.opts.BoolVar(&cd.verb, "V", false, "Verbose: show the request sent and responce got")
	cd.opts.StringVar(&cd.relay, "for", "", "Act as another user (admin-only")

	cd.pargs = args
	cd.opts.Usage = func() {
		astr := cmd
		if len(args) != 0 {
			astr += " <" + strings.Join(args, "> <") + ">"
		}
		fmt.Fprintf(os.Stderr, "%-32s%s\n", astr, help)
	}
}

func main() {
	var opts [16]string

	cmdMap[CMD_LOGIN].opts.StringVar(&opts[0], "tls", "no", "TLS mode")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[1], "cert", "", "x509 cert file")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[2], "admd", "", "Admd address:port")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[3], "proxy", "", "Proxy mode")
	bindCmdUsage(CMD_LOGIN,	[]string{"USER:PASS@HOST:PORT"}, "Login into the system", false)

	bindCmdUsage(CMD_ME, []string{"ACTION"}, "Manage login", false)

	cmdMap[CMD_STATS].opts.StringVar(&opts[0], "p", "0", "Periods to report")
	bindCmdUsage(CMD_STATS,	[]string{}, "Show stats", false)
	bindCmdUsage(CMD_PS,	[]string{}, "List projects", false)

	cmdMap[CMD_FL].opts.StringVar(&opts[0], "pretty", "", "Format of output")
	cmdMap[CMD_FL].opts.StringVar(&opts[1], "label", "", "Labels, comma-separated")
	cmdMap[CMD_FL].opts.StringVar(&opts[2], "pref", "", "Prefix")
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
	cmdMap[CMD_RUN].opts.StringVar(&opts[0], "src", "", "Run a custom source in it")
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
	cmdMap[CMD_FU].opts.StringVar(&opts[9], "acc", "", "Accounts to use, +/- to add/remove")
	cmdMap[CMD_FU].opts.StringVar(&opts[10], "env", "", "Colon-separated list of env vars")
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

	bindCmdUsage(CMD_RTL,	[]string{},	  "List routers", true)
	bindCmdUsage(CMD_RTI,	[]string{"NAME"}, "Show info about router", true)
	cmdMap[CMD_RTA].opts.StringVar(&opts[0], "table", "", "Table entries [M:path:function:key];")
	bindCmdUsage(CMD_RTA,	[]string{"NAME"}, "Create router", true)
	cmdMap[CMD_RTU].opts.StringVar(&opts[0], "table", "", "New table to set")
	bindCmdUsage(CMD_RTU,	[]string{"NAME"}, "Edit router", true)
	bindCmdUsage(CMD_RTD,	[]string{"NAME"}, "Detach repo", true)

	cmdMap[CMD_RL].opts.StringVar(&opts[0], "acc", "", "Account ID")
	cmdMap[CMD_RL].opts.StringVar(&opts[1], "at", "", "Attach status")
	bindCmdUsage(CMD_RL,	[]string{},	"List repos", false)
	bindCmdUsage(CMD_RI,	[]string{"ID"}, "Show info about repo", false)
	cmdMap[CMD_RA].opts.StringVar(&opts[0], "acc", "", "Acc ID from which to pull")
	cmdMap[CMD_RA].opts.StringVar(&opts[1], "pull", "", "Pull policy")
	bindCmdUsage(CMD_RA,	[]string{"URL"}, "Attach repo", false)
	cmdMap[CMD_RU].opts.StringVar(&opts[0], "pull", "", "Pull policy")
	bindCmdUsage(CMD_RU,	[]string{"ID"}, "Update repo", false)
	bindCmdUsage(CMD_RD,	[]string{"ID"}, "Detach repo", false)
	cmdMap[CMD_RLS].opts.StringVar(&opts[0], "pretty", "", "Prettiness of the output")
	bindCmdUsage(CMD_RLS,	[]string{"ID"}, "List files in repo", false)
	bindCmdUsage(CMD_RCAT,	[]string{"ID/NAME"}, "Show contents of a file", false)
	bindCmdUsage(CMD_RP,	[]string{"ID"}, "Pull repo", false)

	cmdMap[CMD_AL].opts.StringVar(&opts[0], "type", "", "Type of account to list")
	bindCmdUsage(CMD_AL,	[]string{},	"List accounts", false)
	bindCmdUsage(CMD_AI,	[]string{"ID"}, "Show info about account", false)
	cmdMap[CMD_AA].opts.StringVar(&opts[0], "param", "", "List of key=value pairs, :-separated")
	bindCmdUsage(CMD_AA,	[]string{"TYPE", "NAME"}, "Add account", false)
	bindCmdUsage(CMD_AD,	[]string{"ID"}, "Delete account", false)
	cmdMap[CMD_AU].opts.StringVar(&opts[0], "param", "", "List of key=value pairs, :-separated")
	bindCmdUsage(CMD_AU,	[]string{"ID"}, "Add account", false)

	bindCmdUsage(CMD_UL,	[]string{}, "List users", false)
	cmdMap[CMD_UA].opts.StringVar(&opts[0], "name", "", "User name")
	cmdMap[CMD_UA].opts.StringVar(&opts[1], "pass", "", "User password")
	bindCmdUsage(CMD_UA,	[]string{"UID"}, "Add user", false)
	bindCmdUsage(CMD_UD,	[]string{"UID"}, "Del user", false)
	cmdMap[CMD_UPASS].opts.StringVar(&opts[0], "pass", "", "New password")
	bindCmdUsage(CMD_UPASS,	[]string{"UID"}, "Set password", false)
	bindCmdUsage(CMD_UEN, []string{"UID", "ST"}, "Set enable status for user", false)
	bindCmdUsage(CMD_UI,	[]string{"UID"}, "Get user info", false)
	cmdMap[CMD_ULIM].opts.StringVar(&opts[0], "plan", "", "Taroff plan ID")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[1], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[2], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[3], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[4], "bo", "", "Maximum outgoing network bytes")
	bindCmdUsage(CMD_ULIM, []string{"UID"}, "Get/Set limits for user", false)

	bindCmdUsage(CMD_TL, []string{}, "List tarif plans", false)
	cmdMap[CMD_TA].opts.StringVar(&opts[0], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_TA].opts.StringVar(&opts[1], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_TA].opts.StringVar(&opts[2], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_TA].opts.StringVar(&opts[3], "bo", "", "Maximum outgoing network bytes")
	bindCmdUsage(CMD_TA, []string{"NAME"}, "Create tarif plan", false)
	bindCmdUsage(CMD_TI, []string{"ID"}, "Info about tarif plan", false)
	bindCmdUsage(CMD_TD, []string{"ID"}, "Info about tarif plan", false)

	bindCmdUsage(CMD_MTYPES, []string{}, "List middleware types", false)
	bindCmdUsage(CMD_LANGS, []string{}, "List of supported languages", false)

	cmdMap[CMD_LANG].opts.StringVar(&opts[0], "src", "", "File")
	bindCmdUsage(CMD_LANG, []string{}, "Check source language", false)

	flag.Usage = func() {
		for _, v := range cmdOrder {
			cmdMap[v].opts.Usage()
		}
	}

	if len(os.Args) < 2 || os.Args[1] == "-h" {
		flag.Usage()
		os.Exit(1)
	}

	cd, ok := cmdMap[os.Args[1]]
	if !ok {
		flag.Usage()
		os.Exit(1)
	}

	if len(os.Args) > 2 && os.Args[2] == "-h" {
		cd.opts.Usage()
		cd.opts.PrintDefaults()
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

	if cd.call == nil {
		fatal(fmt.Errorf("Bad cmd"))
	}

	curCmd = cd
	login()
	cd.call(os.Args[2:], opts)
}
