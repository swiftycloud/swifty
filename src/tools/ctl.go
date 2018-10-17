package main

import (
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"strings"
	"strconv"
	"regexp"
	"time"
	"flag"
	"sort"
	"fmt"
	"os"

	"swifty/common"
	"swifty/apis"
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

func user_list(args []string, opts [16]string) {
	var uss []swyapi.UserInfo
	swyclient.List("users", http.StatusOK, &uss)

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
	swyclient.Add("users", http.StatusCreated, &swyapi.AddUser{UId: args[0], Pass: opts[1], Name: opts[0]}, &ui)
	fmt.Printf("%s user created\n", ui.ID)
}

func user_del(args []string, opts [16]string) {
	swyclient.Del("users/" + args[0], http.StatusNoContent)
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

	swyclient.Mod("users/" + args[0], http.StatusOK, &swyapi.ModUser{Enabled: &enabled})
}

func user_pass(args []string, opts [16]string) {
	rq := &swyapi.ChangePass{}
	rq.Password = opts[0]
	if opts[1] != "" {
		rq.CPassword = opts[1]
	}

	swyclient.Mod("users/" + args[0] + "/pass", http.StatusCreated, rq)
}

func user_info(args []string, opts [16]string) {
	var ui swyapi.UserInfo
	swyclient.Get("users/" + args[0], http.StatusOK, &ui)
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
	swyclient.List("plans", http.StatusOK, &plans)
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
	swyclient.Add("plans", http.StatusCreated, &l, &l)
	fmt.Printf("%s plan created\n", l.Id)
}

func tplan_info(args []string, opts[16]string) {
	var p swyapi.PlanLimits
	swyclient.Get("plans/" + args[0], http.StatusOK, &p)
	fmt.Printf("%s/%s:\n", p.Id, p.Name)
	show_fn_limits(p.Fn)
}

func tplan_del(args []string, opts[16]string) {
	swyclient.Del("plans/" + args[0], http.StatusNoContent)
}

func user_limits(args []string, opts [16]string) {
	var l swyapi.UserLimits

	if opts[0] != "" {
		l.PlanId = opts[0]
	}

	l.Fn = parse_limits(opts[1:])

	if l.Fn != nil || l.PlanId != "" {
		l.UId = args[0]
		swyclient.Mod("users/" + args[0] + "/limits", http.StatusOK, &l)
	} else {
		swyclient.Get("users/" + args[0] + "/limits", http.StatusOK, &l)
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

	swyclient.Get(url("stats", ua), http.StatusOK, &st)

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

	if st.S3 != nil {
		fmt.Printf("*********** S3 **************\n")
		fmt.Printf("  Objects:        %d\n", st.S3.CntObjects)
		fmt.Printf("    Space:        %s\n", formatBytes(uint64(st.S3.CntBytes)))
	}
}

func list_projects(args []string, opts [16]string) {
	var ps []swyapi.ProjectItem
	swyclient.Req1("POST", "project/list", http.StatusOK, swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
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

type FName struct {
	Name	string
	Path	string
	Kids	[]*FName
}

func (f *FName)show(pfx string) {
	fmt.Printf("%s%s (%s)\n", pfx, f.Path, f.Name)
	for _, k := range f.Kids {
		k.show(pfx + "  ")
	}
}

func function_tree(args []string, opts [16]string) {
	var root FName
	ua := []string{}
	if curProj != "" {
		ua = append(ua, "project=" + curProj)
	}
	if opts[0] != "" {
		ua = append(ua, "leafs=" + opts[0])
	}
	swyclient.Req1("GET", url("functions/tree", ua), http.StatusOK, nil, &root)
	root.show("")
}

func function_list(args []string, opts [16]string) {
	ua := []string{}
	if curProj != "" {
		ua = append(ua, "project=" + curProj)
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
	swyclient.Functions().List(ua, &fns)

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
	args[0], r = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Get(args[0], &ifo)
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
		if !isURL(ifo.URL) {
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
	swyclient.Functions().Prop(args[0], "sources", &src)
	if src.Sync {
		fmt.Printf("Sync with:   %s\n", src.Repo)
	}

	var minf []*swyapi.MwareInfo
	swyclient.Functions().Prop(args[0], "middleware", &minf)
	if len(minf) != 0 {
		fmt.Printf("Mware:\n")
		for _, mi := range minf {
			fmt.Printf("\t%20s %-10s(id:%s)\n", mi.Name, mi.Type, mi.Id)
		}
	}

	var bkts []string
	swyclient.Functions().Prop(args[0], "s3buckets", &bkts)
	if len(bkts) != 0 {
		fmt.Printf("Buckets:\n")
		for _, bkt := range bkts {
			fmt.Printf("\t%20s\n", bkt)
		}
	}

	var acs []map[string]string
	swyclient.Functions().Prop(args[0], "accounts", &acs)
	if len(acs) != 0 {
		fmt.Printf("Accounts:\n")
		for _, ac := range acs {
			fmt.Printf("\t%s:%s\n", ac["id"], ac["type"])
		}
	}

	var env []string
	swyclient.Functions().Prop(args[0], "env", &env)
	if len(env) != 0 {
		fmt.Printf("Environment:\n")
		for _, ev := range env {
			fmt.Printf("\t%s\n", ev)
		}
	}
}

func function_minfo(args []string, opts [16]string) {
	var ifo swyapi.FunctionMdat
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Prop(args[0], "mdat", &ifo)
	fmt.Printf("Cookie: %s\n", ifo.Cookie)
	if len(ifo.RL) != 0 {
		fmt.Printf("RL: %d/%d (%d left)\n", ifo.RL[1], ifo.RL[2], ifo.RL[0])
	}
	if len(ifo.BR) != 0 {
		fmt.Printf("BR: %d:%d -> %d\n", ifo.BR[0], ifo.BR[1], ifo.BR[2])
	}
	if len(ifo.Hosts) != 0 {
		fmt.Printf("PODs at %s\n", strings.Join(ifo.Hosts, " "))
	}
	if len(ifo.Hosts) != 0 {
		fmt.Printf("PODs IPs %s\n", strings.Join(ifo.IPs, " "))
	}
	if ifo.Dep != "" {
		fmt.Printf("Deployments: %s\n", ifo.Dep)
	}
}

func check_lang(args []string, opts [16]string) {
	l := detect_language(opts[0])
	fmt.Printf("%s\n", l)
}

func sysctl(args []string, opts [16]string) {
	if len(args) == 0 {
		var ctls []map[string]string
		swyclient.List("sysctl", http.StatusOK, &ctls)
		sort.Slice(ctls, func(i, j int) bool { return ctls[i]["name"] < ctls[j]["name"] })
		for _, ctl := range(ctls) {
			fmt.Printf("%-32s = %s\n", ctl["name"], ctl["value"])
		}

		return
	}

	if len(args) == 1 {
		var ctl map[string]string
		swyclient.Get("sysctl/" + args[0], http.StatusOK, &ctl)
		fmt.Printf("%-32s = %s\n", ctl["name"], ctl["value"])
		return
	}

	if len(args) == 2 {
		swyclient.Mod("sysctl/" + args[0], http.StatusOK, &args[1])
		return
	}
}

func check_ext(path, ext, typ string) string {
	if strings.HasSuffix(path, ext) {
		return typ
	}

	fatal(fmt.Errorf("%s lang detected, but extention is not %s", typ, ext))
	return ""
}

func detect_language(path string) string {
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

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
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
		src.Repo = repo
		src.Sync = sync
	} else if isURL(opt) {
		src.URL = opt
	} else {
		st, err := os.Stat(opt)
		if err != nil {
			fatal(fmt.Errorf("Can't stat sources path " + opt))
		}

		if st.IsDir() {
			fatal(fmt.Errorf("Can't add dir as source"))
		}

		fmt.Printf("Will add file %s\n", opt)
		src.Code = encodeFile(opt)
	}
}

func function_add(args []string, opts [16]string) {
	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

	getSrc(opts[1], &sources)

	if opts[0] == "" {
		opts[0] = detect_language(opts[1])
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
		Project: curProj,
		Sources: sources,
		Code: code,
		Mware: mw,
	}

	if opts[4] != "" {
		x, err := strconv.ParseUint(opts[4], 10, 32)
		if err != nil {
			fatal(fmt.Errorf("Bad tmo value %s: %s", opts[4], err.Error()))
		}
		req.Size.Timeout = uint(x)
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
	swyclient.Functions().Add(req, &fi)
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
	var rres swyapi.WdogFunctionRunResult

	rq := &swyapi.FunctionRun{}

	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	rq.Args = split_args_string(args[1])

	if opts[0] != "" {
		src := &swyapi.FunctionSources{}
		src.Code = encodeFile(opts[0])
		rq.Src = src
	}

	if opts[1] != "" {
		if opts[1] == "-" {
			opts[1] = ""
		}
		rq.Method = &opts[1]
	}

	swyclient.Req1("POST", "functions/" + args[0] + "/run", http.StatusOK, rq, &rres)

	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func function_update(args []string, opts [16]string) {
	fid, _ := swyclient.Functions().Resolve(curProj, args[0])

	if opts[0] != "" {
		var src swyapi.FunctionSources

		getSrc(opts[0], &src)
		swyclient.Functions().Set(fid, "sources", &src)
	}

	if opts[3] != "" {
		mid, _ := swyclient.Mwares().Resolve(curProj, opts[3][1:])
		if opts[3][0] == '+' {
			swyclient.Add("functions/" + fid + "/middleware", http.StatusOK, mid, nil)
		} else if opts[3][0] == '-' {
			swyclient.Del("functions/" + fid + "/middleware/" + mid, http.StatusOK)
		} else {
			fatal(fmt.Errorf("+/- mware name"))
		}
	}

	if opts[8] != "" {
		if opts[8][0] == '+' {
			swyclient.Add("functions/" + fid + "/s3buckets", http.StatusOK, opts[8][1:], nil)
		} else if opts[8][0] == '-' {
			swyclient.Del("functions/" + fid + "/s3buckets/" + opts[8][1:], http.StatusOK)
		} else {
			fatal(fmt.Errorf("+/- bucket name"))
		}
	}

	if opts[9] != "" {
		if opts[9][0] == '+' {
			swyclient.Add("functions/" + fid + "/accounts", http.StatusOK, opts[9][1:], nil)
		} else if opts[9][0] == '-' {
			swyclient.Del("functions/" + fid + "/accounts/" + opts[9][1:], http.StatusOK)
		} else {
			fatal(fmt.Errorf("+/- bucket name"))
		}
	}

	if opts[4] != "" {
		swyclient.Functions().Set(fid, "", &swyapi.FunctionUpdate{UserData: &opts[4]})
	}

	if opts[7] != "" {
		var ac string
		if opts[7] != "-" {
			ac = opts[7]
		}
		swyclient.Functions().Set(fid, "authctx", ac)
	}

	if opts[1] != "" || opts[2] != "" {
		sz := swyapi.FunctionSize{}

		if opts[1] != "" {
			x, err := strconv.ParseUint(opts[1], 10, 32)
			if err != nil {
				fatal(fmt.Errorf("Bad tmo value %s: %s", opts[4], err.Error()))
			}
			sz.Timeout = uint(x)
		}
		if opts[2] != "" {
			sz.Rate, sz.Burst = parse_rate(opts[2])
		}

		swyclient.Functions().Set(fid, "size", &sz)
	}

	if opts[10] != "" {
		envs := strings.Split(opts[10], ":")
		swyclient.Functions().Set(fid, "env", envs)
	}

}

func function_del(args []string, opts [16]string) {
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Del(args[0])
}

func function_on(args []string, opts [16]string) {
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Set(args[0], "", &swyapi.FunctionUpdate{State: "ready"})
}

func function_off(args []string, opts [16]string) {
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Set(args[0], "", &swyapi.FunctionUpdate{State: "deactivated"})
}

func event_list(args []string, opts [16]string) {
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	var eds []swyapi.FunctionEvent
	swyclient.Triggers(args[0]).List([]string{}, &eds)
	for _, e := range eds {
		fmt.Printf("%16s%20s%8s\n", e.Id, e.Name, e.Source)
	}
}

func event_add(args []string, opts [16]string) {
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
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
	if e.Source == "websocket" {
		e.WS = &swyapi.FunctionEventWebsock {
			MwName: opts[0],
		}
	}
	var ei swyapi.FunctionEvent
	swyclient.Triggers(args[0]).Add(&e, &ei)
	fmt.Printf("Event %s created\n", ei.Id)
}

func event_info(args []string, opts [16]string) {
	var r bool
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	args[1], r = swyclient.Triggers(args[0]).Resolve(curProj, args[1])
	var e swyapi.FunctionEvent
	swyclient.Triggers(args[0]).Get(args[1], &e)
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
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	args[1], _ = swyclient.Triggers(args[0]).Resolve(curProj, args[1])
	swyclient.Triggers(args[0]).Del(args[1])
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

	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Req2("POST", "functions/" + args[0] + "/wait", &wo, http.StatusOK, 300)
}

func function_code(args []string, opts [16]string) {
	var res swyapi.FunctionSources
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])
	swyclient.Functions().Prop(args[0], "sources", &res)
	data, err := base64.StdEncoding.DecodeString(res.Code)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s", data)
}

func function_logs(args []string, opts [16]string) {
	var res []swyapi.FunctionLogEntry
	args[0], _ = swyclient.Functions().Resolve(curProj, args[0])

	fa := []string{}
	if opts[0] != "" {
		fa = append(fa, "last=" + opts[0])
	}

	swyclient.Get(url("functions/" + args[0] + "/logs", fa), http.StatusOK, &res)

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
	if curProj != "" {
		ua = append(ua, "project=" + curProj)
	}
	if opts[1] != "" {
		ua = append(ua, "type=" + opts[1])
	}
	if opts[2] != "" {
		for _, l := range strings.Split(opts[2], ",") {
			ua = append(ua, "label=" + l)
		}
	}

	swyclient.Mwares().List(ua, &mws)
	fmt.Printf("%-32s%-20s%-10s\n", "ID", "NAME", "TYPE")
	for _, mw := range mws {
		fmt.Printf("%-32s%-20s%-10s%s\n", mw.Id, mw.Name, mw.Type, strings.Join(mw.Labels, ","))
	}
}

func mware_info(args []string, opts [16]string) {
	var resp swyapi.MwareInfo
	var r bool

	args[0], r = swyclient.Mwares().Resolve(curProj, args[0])
	swyclient.Mwares().Get(args[0], &resp)
	if !r {
		fmt.Printf("Name:         %s\n", resp.Name)
	}
	fmt.Printf("Type:         %s\n", resp.Type)
	if resp.DU != nil {
		fmt.Printf("Disk usage:   %s\n", formatBytes(*resp.DU << 10))
	}
	if resp.URL != nil {
		fmt.Printf("URL:          %s\n", *resp.URL)
	}
	if resp.UserData != "" {
		fmt.Printf("Data:         %s\n", resp.UserData)
	}
}

func mware_add(args []string, opts [16]string) {
	req := swyapi.MwareAdd {
		Name: args[0],
		Project: curProj,
		Type: args[1],
		UserData: opts[0],
	}

	var mi swyapi.MwareInfo
	swyclient.Mwares().Add(&req, &mi)
	fmt.Printf("Mware %s created\n", mi.Id)
}

func mware_del(args []string, opts [16]string) {
	args[0], _ = swyclient.Mwares().Resolve(curProj, args[0])
	swyclient.Mwares().Del(args[0])
}

func auth_cfg(args []string, opts [16]string) {
	switch args[0] {
	case "get", "inf":
		var auths []*swyapi.AuthInfo
		swyclient.List("auths", http.StatusOK, &auths)
		for _, a := range auths {
			fmt.Printf("%s (%s)\n", a.Name, a.Id)
		}

	case "on":
		var di swyapi.DeployInfo
		name := opts[0]
		if name == "" {
			name = "simple_auth"
		}
		swyclient.Add("auths", http.StatusOK, &swyapi.AuthAdd { Name: name }, &di)
		fmt.Printf("Created %s auth\n", di.Id)

	case "off":
		var auths []*swyapi.AuthInfo
		swyclient.List("auths", http.StatusOK, &auths)
		for _, a := range auths {
			if opts[0] != "" && a.Name != opts[0] {
				continue
			}

			fmt.Printf("Shutting down aut %s\n", a.Name)
			swyclient.Del("auths/" + a.Id, http.StatusOK)
		}
	}
}

func deploy_del(args []string, opts [16]string) {
	args[0], _ = swyclient.Deployments().Resolve(curProj, args[0])
	swyclient.Deployments().Del(args[0])
}

func deploy_info(args []string, opts [16]string) {
	var di swyapi.DeployInfo
	args[0], _ = swyclient.Deployments().Resolve(curProj, args[0])
	swyclient.Deployments().Get(args[0], &di)
	fmt.Printf("State:        %s\n", di.State)
	fmt.Printf("Items:\n")
	for _, i := range di.Items {
		fmt.Printf("\t%-32s%-12s%s\n", i.Name + ":", i.Type + ",", i.State)
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
	swyclient.Deployments().List(ua, &dis)
	fmt.Printf("%-32s%-20s\n", "ID", "NAME")
	for _, di := range dis {
		fmt.Printf("%-32s%-20s (%d items) %s\n", di.Id, di.Name, len(di.Items), strings.Join(di.Labels, ","))
	}
}

func deploy_add(args []string, opts [16]string) {
	fmt.Printf("Starting deploy %s\n", args[0])

	da := swyapi.DeployStart{
		Name: args[0],
		Project: curProj,
	}

	if opts[1] != "" {
		da.Params = split_args_string(opts[1])
	}

	if strings.HasPrefix(opts[0], "repo:") {
		da.From = swyapi.DeploySource {
			Repo: opts[0][5:],
		}
	} else if isURL(opts[0]) {
		da.From = swyapi.DeploySource {
			URL: opts[0],
		}
	} else {
		fmt.Printf("Adding deploy from %s\n", opts[0])
		da.From = swyapi.DeploySource {
			Descr: encodeFile(opts[0]),
		}
	}

	var di swyapi.DeployInfo
	swyclient.Deployments().Add(&da, &di)
	fmt.Printf("%s deployment started\n", di.Id)
}

func router_list(args []string, opts [16]string) {
	var rts []swyapi.RouterInfo
	swyclient.Routers().List([]string{}, &rts)
	for _, rt := range rts {
		fmt.Printf("%s %12s %s (%s)\n", rt.Id, rt.Name, rt.URL, strings.Join(rt.Labels, ","))
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
		Project: curProj,
	}
	if opts[0] != "" {
		ra.Table = parse_route_table(opts[0])
	}
	var ri swyapi.RouterInfo
	swyclient.Routers().Add(&ra, &ri)
	fmt.Printf("Router %s created\n", ri.Id)
}

func router_info(args []string, opts [16]string) {
	args[0], _ = swyclient.Routers().Resolve(curProj, args[0])
	var ri swyapi.RouterInfo
	swyclient.Routers().Get(args[0], &ri)
	fmt.Printf("URL:      %s\n", ri.URL)
	fmt.Printf("Table:    (%d ents)\n", ri.TLen)
	var res []*swyapi.RouterEntry
	swyclient.Routers().Prop(args[0], "table", &res)
	for _, re := range res {
		fmt.Printf("   %8s /%-32s -> %s\n", re.Method, re.Path, re.Call)
	}
}

func router_upd(args []string, opts [16]string) {
	args[0], _ = swyclient.Routers().Resolve(curProj, args[0])
	if opts[0] != "" {
		rt := parse_route_table
		swyclient.Routers().Set(args[0], "table", rt)
	}
}

func router_del(args []string, opts [16]string) {
	args[0], _ = swyclient.Routers().Resolve(curProj, args[0])
	swyclient.Routers().Del(args[0])
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
	swyclient.Repos().List(ua, &ris)
	fmt.Printf("%-32s%-8s%-12s%s\n", "ID", "TYPE", "STATE", "URL")
	for _, ri := range ris {
		t := ri.Type
		if ri.AccID != "" {
			t += "*"
		}

		url := ri.URL
		if ri.Id == "" && ri.AccID != "" {
			url += "(" + ri.AccID + ")"
		}

		fmt.Printf("%-32s%-8s%-12s%s\n", ri.Id, t, ri.State, url)
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
	swyclient.Repos().Prop(args[0], "desc", &d)
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
	swyclient.Repos().Prop(args[0], "files", &fl)
	show_files("", fl, opts[0])
}

func repo_cat_file(args []string, opts [16]string) {
	p := strings.SplitN(args[0], "/", 2)
	resp, _ := swyclient.Req2("GET", "repos/" + p[0] + "/files/" + p[1], nil, http.StatusOK, 0)
	dat, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fatal(fmt.Errorf("Can't read file: %s", err.Error()))
	}
	fmt.Printf(string(dat))
}

func repo_pull(args []string, opts [16]string) {
	swyclient.Req1("POST", "repos/" + args[0] + "/pull", http.StatusOK, nil, nil)
}

func repo_info(args []string, opts [16]string) {
	var ri swyapi.RepoInfo
	swyclient.Repos().Get(args[0], &ri)
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
	swyclient.Repos().Add(&ra, &ri)
	fmt.Printf("%s repo attached\n", ri.Id)
}

func repo_upd(args []string, opts [16]string) {
	ra := swyapi.RepoUpdate {}
	if opts[0] != "" {
		if opts[0] == "-" {
			opts[0] = ""
		}
		ra.Pull = &opts[0]
	}

	swyclient.Repos().Set(args[0], "", &ra)
}

func repo_del(args []string, opts [16]string) {
	swyclient.Repos().Del(args[0])
}

func acc_list(args []string, opts [16]string) {
	var ais []map[string]string
	ua := []string{}
	if opts[0] != "" {
		ua = append(ua, "type=" + opts[0])
	}
	swyclient.Accounts().List(ua, &ais)
	fmt.Printf("%-32s%-12s\n", "ID", "TYPE")
	for _, ai := range ais {
		fmt.Printf("%-32s%-12s\n", ai["id"], ai["type"])
	}
}

func acc_info(args []string, opts [16]string) {
	var ai map[string]string
	swyclient.Accounts().Get(args[0], &ai)
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
	swyclient.Accounts().Add(&aa, &ai)
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

	swyclient.Accounts().Set(args[0], "", &au)
}

func acc_del(args []string, opts [16]string) {
	swyclient.Accounts().Del(args[0])
}

func s3_access(args []string, opts [16]string) {
	acc := swyapi.S3Access {
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

	swyclient.Req1("POST", "s3/access", http.StatusOK, acc, &creds)

	fmt.Printf("Endpoint %s\n", creds.Endpoint)
	fmt.Printf("Key:     %s\n", creds.Key)
	fmt.Printf("Secret:  %s\n", creds.Secret)
	fmt.Printf("Expires: in %d seconds\n", creds.Expires)
	fmt.Printf("AccID:   %s\n", creds.AccID)
}

func languages(args []string, opts [16]string) {
	var ls []string
	swyclient.Req1("GET", "info/langs", http.StatusOK, nil, &ls)
	for _, l := range(ls) {
		var li swyapi.LangInfo
		fmt.Printf("%s\n", l)
		swyclient.Req1("GET", "info/langs/" + l, http.StatusOK, nil , &li)
		fmt.Printf("\tversion: %s\n", li.Version)
		fmt.Printf("\tpackages:\n")
		for _, p := range(li.Packages) {
			fmt.Printf("\t\t%s\n", p)
		}
	}
}

func mware_types(args []string, opts [16]string) {
	var r []string

	swyclient.Req1("GET", "info/mwares", http.StatusOK, nil, &r)
	for _, v := range r {
		fmt.Printf("%s\n", v)
	}
}

func login() {
	home, found := os.LookupEnv("HOME")
	if !found {
		fatal(fmt.Errorf("No HOME dir set"))
	}

	err := xh.ReadYamlConfig(home + "/.swifty.conf", &conf)
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
	if curRelay != "" {
		swyclient.Relay(curRelay)
	} else if conf.Login.Relay != "" {
		swyclient.Relay(conf.Login.Relay)
	}
	if verbose {
		swyclient.Verbose()
	}
	if !conf.TLS {
		swyclient.NoTLS()
	}
	if conf.Direct {
		swyclient.Direct()
	}

	swyclient.OnError(func(err error) { fatal(err) })
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

	c := xh.ParseXCreds(creds)
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

	err := xh.WriteYamlConfig(home + "/.swifty.conf", &conf)
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
	CMD_FT string		= "ft"
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
	CMD_SYSCTL string	= "sc"
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
	CMD_FT,

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
	CMD_SYSCTL,
}

type cmdDesc struct {
	opts	*flag.FlagSet
	npa	int
	adm	bool
	wp	bool
	call	func([]string, [16]string)
	help	string
}

var curCmd *cmdDesc
var curProj string
var curRelay string
var verbose bool

var cmdMap = map[string]*cmdDesc {
	CMD_LOGIN:	&cmdDesc{ help: "Login to gate/admd" },
	CMD_ME:		&cmdDesc{ help: "Manage login",		call: manage_login,	  },
	CMD_STATS:	&cmdDesc{ help: "Show user stats",	call: show_stats,	  },
	CMD_PS:		&cmdDesc{ help: "List projects",	call: list_projects,	  },

	CMD_FL:		&cmdDesc{ help: "List functions",	call: function_list,	wp: true },
	CMD_FT:		&cmdDesc{ help: "Show fns tree",	call: function_tree,	wp: true },
	CMD_FI:		&cmdDesc{ help: "Show fn info",		call: function_info,	wp: true },
	CMD_FIM:	&cmdDesc{ help: "Show fn run state",	call: function_minfo,	wp: true },
	CMD_FA:		&cmdDesc{ help: "Add function",		call: function_add,	wp: true },
	CMD_FD:		&cmdDesc{ help: "Del function",		call: function_del,	wp: true },
	CMD_FU:		&cmdDesc{ help: "Update function",	call: function_update,	wp: true },
	CMD_RUN:	&cmdDesc{ help: "Run function code",	call: run_function,	wp: true },
	CMD_FLOG:	&cmdDesc{ help: "Show fn logs",		call: function_logs,	wp: true },
	CMD_FCOD:	&cmdDesc{ help: "Show fn code",		call: function_code,	wp: true },
	CMD_FON:	&cmdDesc{ help: "Activate fn",		call: function_on,	wp: true },
	CMD_FOFF:	&cmdDesc{ help: "Deactivate fn",	call: function_off,	wp: true },
	CMD_FW:		&cmdDesc{ help: "Wait something on fn",	call: function_wait,	wp: true },

	CMD_EL:		&cmdDesc{ help: "List fn triggers",	call: event_list,	wp: true },
	CMD_EA:		&cmdDesc{ help: "Add fn trigger",	call: event_add,	wp: true },
	CMD_EI:		&cmdDesc{ help: "Show fn trigger info",	call: event_info,	wp: true },
	CMD_ED:		&cmdDesc{ help: "Del fn trigger",	call: event_del,	wp: true },

	CMD_ML:		&cmdDesc{ help: "List middleware",	call: mware_list,	wp: true },
	CMD_MI:		&cmdDesc{ help: "Show mware info",	call: mware_info,	wp: true },
	CMD_MA:		&cmdDesc{ help: "Add mware",		call: mware_add,	wp: true },
	CMD_MD:		&cmdDesc{ help: "Del mware",		call: mware_del,	wp: true },

	CMD_DL:		&cmdDesc{ help: "List deployments",	call: deploy_list,	wp: true },
	CMD_DI:		&cmdDesc{ help: "Show deploy info",	call: deploy_info,	wp: true },
	CMD_DA:		&cmdDesc{ help: "Add deploy",		call: deploy_add,	wp: true },
	CMD_DD:		&cmdDesc{ help: "Del deploy",		call: deploy_del,	wp: true },
	CMD_AUTH:	&cmdDesc{ help: "Configure AaaS",	call: auth_cfg,		wp: true },

	CMD_RTL:	&cmdDesc{ help: "List routers",		call: router_list,	wp: true },
	CMD_RTI:	&cmdDesc{ help: "Show router info",	call: router_info,	wp: true },
	CMD_RTA:	&cmdDesc{ help: "Add router",		call: router_add,	wp: true },
	CMD_RTU:	&cmdDesc{ help: "Update router",	call: router_upd,	wp: true },
	CMD_RTD:	&cmdDesc{ help: "Del router",		call: router_del,	wp: true },

	CMD_RL:		&cmdDesc{ help: "List repositories",	call: repo_list		},
	CMD_RI:		&cmdDesc{ help: "Show repo info",	call: repo_info		},
	CMD_RA:		&cmdDesc{ help: "Add repo",		call: repo_add		},
	CMD_RU:		&cmdDesc{ help: "Update repo",		call: repo_upd		},
	CMD_RD:		&cmdDesc{ help: "Del repo",		call: repo_del		},
	CMD_RLS:	&cmdDesc{ help: "List files in repo",	call: repo_list_files	},
	CMD_RCAT:	&cmdDesc{ help: "Show file contents",	call: repo_cat_file	},
	CMD_RP:		&cmdDesc{ help: "Pull repo",		call: repo_pull		},

	CMD_AL:		&cmdDesc{ help: "List accounts",	call: acc_list		},
	CMD_AI:		&cmdDesc{ help: "Show accont info",	call: acc_info		},
	CMD_AA:		&cmdDesc{ help: "Add account",		call: acc_add		},
	CMD_AD:		&cmdDesc{ help: "Del account",		call: acc_del		},
	CMD_AU:		&cmdDesc{ help: "Update account",	call: acc_upd		},

	CMD_UL:		&cmdDesc{ help: "List users",		call: user_list,	adm: true },
	CMD_UI:		&cmdDesc{ help: "Show user info",	call: user_info,	adm: true },
	CMD_UA:		&cmdDesc{ help: "Add user",		call: user_add,		adm: true },
	CMD_UD:		&cmdDesc{ help: "Del user",		call: user_del,		adm: true },
	CMD_UPASS:	&cmdDesc{ help: "Change password",	call: user_pass,	adm: true },
	CMD_UEN:	&cmdDesc{ help: "Enable/disable user",	call: user_enabled,	adm: true },
	CMD_ULIM:	&cmdDesc{ help: "Configure user limits",call: user_limits,	adm: true },

	CMD_TL:		&cmdDesc{ help: "List plans",		call: tplan_list,	adm: true },
	CMD_TA:		&cmdDesc{ help: "Add plan",		call: tplan_add,	adm: true },
	CMD_TI:		&cmdDesc{ help: "Show plan info",	call: tplan_info,	adm: true },
	CMD_TD:		&cmdDesc{ help: "Del plan",		call: tplan_del,	adm: true },

	CMD_S3ACC:	&cmdDesc{ help: "Get S3 access",	call: s3_access		},

	CMD_LANGS:	&cmdDesc{ help: "Show supported languages",	call: languages		},
	CMD_MTYPES:	&cmdDesc{ help: "Show supported mwares",	call: mware_types	},
	CMD_LANG:	&cmdDesc{ help: "Detect file language",		call: check_lang	},
	CMD_SYSCTL:	&cmdDesc{ help: "Work with gate variables",	call: sysctl		},
}

func setupCommonCmd(cmd string, args ...string) {
	cd := cmdMap[cmd]
	cd.opts = flag.NewFlagSet(cmd, flag.ExitOnError)
	if cd.wp {
		cd.opts.StringVar(&curProj, "proj", "", "Project to work on")
	}
	cd.opts.BoolVar(&verbose, "V", false, "Verbose: show the request sent and responce got")
	cd.opts.StringVar(&curRelay, "for", "", "Act as another user (admin-only")

	cd.npa = len(args)
	cd.opts.Usage = func() {
		astr := cmd
		if len(args) != 0 {
			astr += " <" + strings.Join(args, "> <") + ">"
		}
		fmt.Fprintf(os.Stderr, "%-32s%s\n", astr, cd.help)
	}
}

func main() {
	var opts [16]string

	setupCommonCmd(CMD_LOGIN, "USER:PASS@HOST:PORT")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[0], "tls", "no", "TLS mode")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[1], "cert", "", "x509 cert file")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[2], "admd", "", "Admd address:port")
	cmdMap[CMD_LOGIN].opts.StringVar(&opts[3], "proxy", "", "Proxy mode")

	setupCommonCmd(CMD_ME, "ACTION")

	setupCommonCmd(CMD_STATS)
	cmdMap[CMD_STATS].opts.StringVar(&opts[0], "p", "0", "Periods to report")

	setupCommonCmd(CMD_PS)

	setupCommonCmd(CMD_FL)
	cmdMap[CMD_FL].opts.StringVar(&opts[0], "pretty", "", "Format of output")
	cmdMap[CMD_FL].opts.StringVar(&opts[1], "label", "", "Labels, comma-separated")
	cmdMap[CMD_FL].opts.StringVar(&opts[2], "pref", "", "Prefix")
	setupCommonCmd(CMD_FT)
	cmdMap[CMD_FT].opts.StringVar(&opts[0], "leafs", "", "Show leafs of the tree")
	setupCommonCmd(CMD_FI, "NAME")
	setupCommonCmd(CMD_FIM, "NAME")
	setupCommonCmd(CMD_FA, "NAME")
	cmdMap[CMD_FA].opts.StringVar(&opts[0], "lang", "auto", "Language")
	cmdMap[CMD_FA].opts.StringVar(&opts[1], "src", ".", "Source file")
	cmdMap[CMD_FA].opts.StringVar(&opts[2], "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_FA].opts.StringVar(&opts[4], "tmo", "", "Timeout")
	cmdMap[CMD_FA].opts.StringVar(&opts[5], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_FA].opts.StringVar(&opts[6], "data", "", "Any text associated with fn")
	cmdMap[CMD_FA].opts.StringVar(&opts[7], "env", "", "Colon-separated list of env vars")
	cmdMap[CMD_FA].opts.StringVar(&opts[8], "auth", "", "ID of auth mware to verify the call")
	setupCommonCmd(CMD_RUN, "NAME", "ARG=VAL,...")
	cmdMap[CMD_RUN].opts.StringVar(&opts[0], "src", "", "Run a custom source in it")
	cmdMap[CMD_RUN].opts.StringVar(&opts[1], "method", "", "Run method")
	setupCommonCmd(CMD_FU, "NAME")
	cmdMap[CMD_FU].opts.StringVar(&opts[0], "src", "", "Source file")
	cmdMap[CMD_FU].opts.StringVar(&opts[1], "tmo", "", "Timeout")
	cmdMap[CMD_FU].opts.StringVar(&opts[2], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_FU].opts.StringVar(&opts[3], "mw", "", "Mware to use, +/- to add/remove")
	cmdMap[CMD_FU].opts.StringVar(&opts[4], "data", "", "Associated text")
	cmdMap[CMD_FU].opts.StringVar(&opts[7], "auth", "", "Auth context (- for off)")
	cmdMap[CMD_FU].opts.StringVar(&opts[8], "s3b", "", "Bucket to use, +/- to add/remove")
	cmdMap[CMD_FU].opts.StringVar(&opts[9], "acc", "", "Accounts to use, +/- to add/remove")
	cmdMap[CMD_FU].opts.StringVar(&opts[10], "env", "", "Colon-separated list of env vars")
	setupCommonCmd(CMD_FD, "NAME")
	setupCommonCmd(CMD_FLOG, "NAME")
	cmdMap[CMD_FLOG].opts.StringVar(&opts[0], "last", "", "Last N 'duration' period")
	setupCommonCmd(CMD_FCOD, "NAME")
	setupCommonCmd(CMD_FON, "NAME")
	setupCommonCmd(CMD_FOFF, "NAME")

	setupCommonCmd(CMD_FW, "NAME")
	cmdMap[CMD_FW].opts.StringVar(&opts[0], "version", "", "Version")
	cmdMap[CMD_FW].opts.StringVar(&opts[1], "tmo", "", "Timeout")

	setupCommonCmd(CMD_EL, "NAME")
	setupCommonCmd(CMD_EA, "NAME", "ENAME", "SRC")
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "tab", "", "Cron tab")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "args", "", "Cron args")
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "buck", "", "S3 bucket")
	cmdMap[CMD_EA].opts.StringVar(&opts[1], "ops", "", "S3 ops")
	cmdMap[CMD_EA].opts.StringVar(&opts[0], "wsid", "", "Websock mware id")
	setupCommonCmd(CMD_EI, "NAME", "ENAME")
	setupCommonCmd(CMD_ED, "NAME", "ENAME")

	setupCommonCmd(CMD_ML)
	cmdMap[CMD_ML].opts.StringVar(&opts[1], "type", "", "Filter mware by type")
	cmdMap[CMD_ML].opts.StringVar(&opts[2], "label", "", "Labels, comma-separated")
	setupCommonCmd(CMD_MI, "NAME")
	setupCommonCmd(CMD_MA, "NAME", "TYPE")
	cmdMap[CMD_MA].opts.StringVar(&opts[0], "data", "", "Associated text")
	setupCommonCmd(CMD_MD, "NAME")

	setupCommonCmd(CMD_S3ACC, "BUCKET")
	cmdMap[CMD_S3ACC].opts.StringVar(&opts[0], "life", "60", "Lifetime (default 1 min)")
	setupCommonCmd(CMD_AUTH, "ACTION")
	cmdMap[CMD_AUTH].opts.StringVar(&opts[0], "name", "", "Name for auth")

	setupCommonCmd(CMD_DL)
	cmdMap[CMD_DL].opts.StringVar(&opts[0], "label", "", "Labels, comma-separated")
	setupCommonCmd(CMD_DI, "NAME")
	setupCommonCmd(CMD_DA, "NAME")
	cmdMap[CMD_DA].opts.StringVar(&opts[0], "from", "", "File from which to get info")
	cmdMap[CMD_DA].opts.StringVar(&opts[1], "params", "", "Parameters, ,-separated")
	setupCommonCmd(CMD_DD, "NAME")

	setupCommonCmd(CMD_RTL)
	setupCommonCmd(CMD_RTI, "NAME")
	setupCommonCmd(CMD_RTA, "NAME")
	cmdMap[CMD_RTA].opts.StringVar(&opts[0], "table", "", "Table entries [M:path:function:key];")
	setupCommonCmd(CMD_RTU, "NAME")
	cmdMap[CMD_RTU].opts.StringVar(&opts[0], "table", "", "New table to set")
	setupCommonCmd(CMD_RTD, "NAME")

	setupCommonCmd(CMD_RL)
	cmdMap[CMD_RL].opts.StringVar(&opts[0], "acc", "", "Account ID")
	cmdMap[CMD_RL].opts.StringVar(&opts[1], "at", "", "Attach status")
	setupCommonCmd(CMD_RI, "ID")
	setupCommonCmd(CMD_RA, "URL")
	cmdMap[CMD_RA].opts.StringVar(&opts[0], "acc", "", "Acc ID from which to pull")
	cmdMap[CMD_RA].opts.StringVar(&opts[1], "pull", "", "Pull policy")
	setupCommonCmd(CMD_RU, "ID")
	cmdMap[CMD_RU].opts.StringVar(&opts[0], "pull", "", "Pull policy")
	setupCommonCmd(CMD_RD, "ID")
	setupCommonCmd(CMD_RLS, "ID")
	cmdMap[CMD_RLS].opts.StringVar(&opts[0], "pretty", "", "Prettiness of the output")
	setupCommonCmd(CMD_RCAT, "ID/NAME")
	setupCommonCmd(CMD_RP, "ID")

	setupCommonCmd(CMD_AL)
	cmdMap[CMD_AL].opts.StringVar(&opts[0], "type", "", "Type of account to list")
	setupCommonCmd(CMD_AI, "ID")
	setupCommonCmd(CMD_AA, "TYPE", "NAME")
	cmdMap[CMD_AA].opts.StringVar(&opts[0], "param", "", "List of key=value pairs, :-separated")
	setupCommonCmd(CMD_AD, "ID")
	setupCommonCmd(CMD_AU, "ID")
	cmdMap[CMD_AU].opts.StringVar(&opts[0], "param", "", "List of key=value pairs, :-separated")

	setupCommonCmd(CMD_UL)
	setupCommonCmd(CMD_UA, "UID")
	cmdMap[CMD_UA].opts.StringVar(&opts[0], "name", "", "User name")
	cmdMap[CMD_UA].opts.StringVar(&opts[1], "pass", "", "User password")
	setupCommonCmd(CMD_UD, "UID")
	setupCommonCmd(CMD_UPASS, "UID")
	cmdMap[CMD_UPASS].opts.StringVar(&opts[0], "pass", "", "New password")
	cmdMap[CMD_UPASS].opts.StringVar(&opts[1], "cur", "", "Current password")
	setupCommonCmd(CMD_UEN, "UID", "ST")
	setupCommonCmd(CMD_UI, "UID")
	setupCommonCmd(CMD_ULIM, "UID")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[0], "plan", "", "Taroff plan ID")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[1], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[2], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[3], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_ULIM].opts.StringVar(&opts[4], "bo", "", "Maximum outgoing network bytes")

	setupCommonCmd(CMD_TL)
	setupCommonCmd(CMD_TA, "NAME")
	cmdMap[CMD_TA].opts.StringVar(&opts[0], "rl", "", "Rate (rate[:burst])")
	cmdMap[CMD_TA].opts.StringVar(&opts[1], "fnr", "", "Number of functions (in a project)")
	cmdMap[CMD_TA].opts.StringVar(&opts[2], "gbs", "", "Maximum number of GBS to consume")
	cmdMap[CMD_TA].opts.StringVar(&opts[3], "bo", "", "Maximum outgoing network bytes")
	setupCommonCmd(CMD_TI, "ID")
	setupCommonCmd(CMD_TD, "ID")

	setupCommonCmd(CMD_MTYPES)
	setupCommonCmd(CMD_LANGS)

	setupCommonCmd(CMD_LANG)
	cmdMap[CMD_LANG].opts.StringVar(&opts[0], "src", "", "File")

	setupCommonCmd(CMD_SYSCTL)

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

	npa := cd.npa + 2
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
