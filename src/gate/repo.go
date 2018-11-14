/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/yaml.v2"
	"gopkg.in/mgo.v2/bson"
	"fmt"
	"strings"
	"bytes"
	"os/exec"
	"errors"
	"net/url"
	"net/http"
	"bufio"
	"os"
	"io"
	"context"
	"time"
	"io/ioutil"
	"swifty/common/http"
	"swifty/common/xrest"
	"swifty/common"
	"swifty/apis"
)

const (
	DBRepoStateCln	int = 1
	DBRepoStateRem	int = 2
	DBRepoStateStl	int = 3
	DBRepoStateRdy	int = 4
)

const (
	RepoDescFile	= ".swifty.yml"

	PullEvent	= "event"
	PullPeriodic	= "periodic"
)

var repStates = map[int]string {
	DBRepoStateCln:	"cloning",
	DBRepoStateStl:	"stalled",
	DBRepoStateRem:	"removing",
	DBRepoStateRdy:	"ready",
}

func cloneDir() string {
	return conf.Home + "/" + CloneDir
}

type RepoDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Type		string		`bson:"type"`
	State		int		`bson:"state"`
	Commit		string		`bson:"commit,omitempty"`
	UserData	string		`bson:"userdata,omitempty"`
	Pull		string		`bson:"pulling"`
	DU		uint64		`bson:"du"`
	Path		string		`bson:"path"`
	LastPull	*time.Time	`bson:"last_pull,omitempty"`

	AccID		bson.ObjectId	`bson:"accid,omitempty"`
}

type Repos struct {}

type GitHubRepo struct {
	Name		string		`json:"name"`
	URL		string		`json:"clone_url"`
	Private		bool		`json:"private"`
}

type GitHubPushEvent struct {
	Repo		*GitHubRepo	`json:"repository"`
}

type GitHubUser struct {
	Login		string		`json:"login"`
}

func githubRepoUpdated(ctx context.Context, r *http.Request) {
	var params GitHubPushEvent

	err := xhttp.RReq(r, &params)
	if err != nil {
		ctxlog(ctx).Errorf("Error decoding GH event: %s", err.Error())
		return
	}
	if params.Repo == nil || params.Repo.URL == "" {
		ctxlog(ctx).Errorf("Bad GH event: %v", params.Repo)
		return
	}

	ctxlog(ctx).Debugf("Repo %s updated", params.Repo.URL)
	var rds []*RepoDesc

	err = dbFindAll(ctx, bson.M{
		"name": params.Repo.URL,
		"pulling": PullEvent,
	}, &rds)
	if err != nil {
		ctxlog(ctx).Errorf("Cannot get repos: %s", err.Error())
		return
	}

	synced := 0

	for _, rd := range rds {
		if rd.pullSync() == nil {
			repoPulls.WithLabelValues("hook").Inc()
			synced++
		}
	}

	ctxlog(ctx).Debugf("Synced %d repos", synced)
}

func (rd *RepoDesc)path() string {
	if rd.Path != "" {
		return rd.Path
	}

	return rd.SwoId.Tennant + "/" + rd.ObjID.Hex()
}

func (rd *RepoDesc)clonePath() string {
	return cloneDir() + "/" + rd.path()
}

func (rd *RepoDesc)URL() string { return rd.SwoId.Name }

func getRepoDesc(id *SwoId, params *swyapi.RepoAdd) *RepoDesc {
	rd := &RepoDesc {
		SwoId:		*id,
		Type:		params.Type,
		UserData:	params.UserData,
		Pull:		params.Pull,
	}

	return rd
}

func (_ Repos)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.RepoAdd)
	id := ctxSwoId(ctx, NoProject, params.URL)
	switch params.Pull {
	case PullPeriodic, PullEvent, "":
		;
	default:
		return nil, GateErrM(swyapi.GateBadRequest, "Bad pull mode")
	}
	return getRepoDesc(id, params), nil
}

func (rd *RepoDesc)Add(ctx context.Context, p interface{}) *xrest.ReqErr {
	var acc *AccDesc

	td, err := tendatGet(ctx)
	if err != nil {
		return GateErrC(swyapi.GateGenErr)
	}

	if td.repl != nil && td.repl.Number != 0 {
		cnr, err := dbRepoCountTen(ctx)
		if err != nil {
			return GateErrD(err)
		}

		if uint32(cnr + 1) > td.repl.Number {
			return GateErrC(swyapi.GateLimitHit)
		}
	}

	params := p.(*swyapi.RepoAdd)
	if params.AccID != "" {
		var ac AccDesc

		cerr := objFindId(ctx, params.AccID, &ac, nil)
		if cerr != nil {
			return cerr
		}

		if ac.Type != params.Type {
			return GateErrM(swyapi.GateBadRequest, "Bad account type")
		}

		acc = &ac
	}

	return rd.Attach(ctx, acc)
}

func (rd *RepoDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return rd.toInfo(ctx, details)
}

func (rd *RepoDesc)toInfo(ctx context.Context, details bool) (*swyapi.RepoInfo, *xrest.ReqErr) {
	if rd.DU == 0 {
		rq := &scrapeReq{r:rd, done:make(chan error)}
		scrapes <-rq
		err := <-rq.done
		if err != nil {
			return nil, GateErrC(swyapi.GateGenErr)
		}
	}

	r := &swyapi.RepoInfo {
		Id:		rd.ObjID.Hex(),
		Type:		rd.Type,
		URL:		rd.URL(),
		State:		repStates[rd.State],
		Commit:		rd.Commit,
		AccID:		rd.AccID.Hex(),
	}

	r.SetDU(rd.DU)

	if details {
		r.UserData = rd.UserData
		r.Pull = rd.Pull

		dfile := rd.clonePath() + "/" + RepoDescFile
		if _, err := os.Stat(dfile); err == nil {
			r.Desc = true
		} else {
			r.Desc = false
		}
	}

	return r, nil
}

type repoHandler struct {
	clone func (context.Context, *RepoDesc, *AccDesc) (string, error)
}

var repoHandlers = map[string]repoHandler {
	"github":	{
		clone:	cloneGit,
	},
	"gitlab":	{
		clone:	cloneGit,
	},
}

func cloneGit(ctx context.Context, rd *RepoDesc, ac *AccDesc) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		ctxlog(ctx).Errorf("repo for %s is already there", rd.SwoId.Str())
		return "", fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		ctxlog(ctx).Errorf("can't create %s: %s", clone_to, err.Error())
		return "", err
	}

	curl := rd.URL()

	if ac != nil {
		if ac.Type != "github" {
			return "", errors.New("Corrupted acc type")
		}

		t, err := ac.Secrets["token"].value()
		if err != nil {
			return "", err
		}

		if t != "" && strings.HasPrefix(curl, "https://") {
			curl = "https://" + ac.SwoId.Name + ":" + t + "@" + curl[8:]
		}
	}

	cmd := exec.Command("git", "-C", clone_to, "clone", "--depth=1", curl, ".")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't clone %s -> %s: %s (%s:%s)",
				rd.URL(), clone_to, err.Error(),
				stdout.String(), stderr.String())
		logSaveResult(ctx, rd.SwoId.PCookie(), "repo_clone", stdout.String(), stderr.String())
		return "", err
	}

	rd.dirty()
	return gitCommit(clone_to)
}

func bgClone(rd *RepoDesc, ac *AccDesc, rh *repoHandler) {
	ctx, done := mkContext("::gitclone")
	defer done(ctx)

	commit, err := rh.clone(ctx, rd, ac)
	if err != nil {
		dbUpdatePart(ctx, rd, bson.M{ "state": DBRepoStateStl })
		return
	}

	t := time.Now()
	dbUpdatePart(ctx, rd, bson.M{
					"state": DBRepoStateRdy,
					"commit": commit,
					"last_pull": &t,
				})
}

func (rd *RepoDesc)Attach(ctx context.Context, ac *AccDesc) *xrest.ReqErr {
	rd.ObjID = bson.NewObjectId()
	rd.State = DBRepoStateCln
	if ac != nil {
		rd.AccID = ac.ObjID
	}

	rh, ok := repoHandlers[rd.Type]
	if !ok {
		return GateErrM(swyapi.GateBadRequest, "Unsupported repo type")
	}

	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	go bgClone(rd, ac, &rh)
	gateRepos.Inc()

	return nil
}

func (rd *RepoDesc)Upd(ctx context.Context, p interface{}) *xrest.ReqErr {
	ru := p.(*swyapi.RepoUpdate)
	if ru.Pull != nil {
		rd.Pull = *ru.Pull
		err := dbUpdatePart(ctx, rd, bson.M{"pulling": rd.Pull})
		if err != nil {
			return GateErrD(err)
		}
	}

	return nil
}

func (rd *RepoDesc)Del(ctx context.Context) *xrest.ReqErr {
	err := dbUpdatePart(ctx, rd, bson.M{"state": DBRepoStateRem})
	if err != nil {
		return GateErrD(err)
	}

	rd.State = DBRepoStateRem

	if rd.Path == "" {
		_, err = xh.DropDir(cloneDir(), rd.path())
		if err != nil {
			return GateErrE(swyapi.GateFsError, err)
		}
	}

	err = dbRemove(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	gateRepos.Dec()
	return nil
}

func (rd *RepoDesc)description(ctx context.Context) (*swyapi.RepoDesc, *xrest.ReqErr) {
	dfile := rd.clonePath() + "/" + RepoDescFile
	if _, err := os.Stat(dfile); os.IsNotExist(err) {
		return nil, GateErrM(swyapi.GateNotAvail, "No description for repo")
	}

	var out swyapi.RepoDesc

	desc, err := ioutil.ReadFile(dfile)
	if err != nil {
		return nil, GateErrE(swyapi.GateFsError, err)
	}

	err = yaml.Unmarshal(desc, &out)
	if err != nil {
		return nil, GateErrE(swyapi.GateGenErr, err)
	}

	return &out, nil
}

func (rd *RepoDesc)readFile(ctx context.Context, fname string) ([]byte, *xrest.ReqErr) {
	dfile := rd.clonePath() + "/" + fname
	if _, err := os.Stat(dfile); os.IsNotExist(err) {
		return nil, GateErrM(swyapi.GateNotAvail, "No such file in repo")
	}

	cont, err := ioutil.ReadFile(dfile)
	if err != nil {
		return nil, GateErrM(swyapi.GateFsError, "Error reading file")
	}

	return cont, nil
}

func (rd *RepoDesc)listFiles(ctx context.Context) ([]*swyapi.RepoFile, *xrest.ReqErr) {
	rp := rd.clonePath()
	root := swyapi.RepoFile { Path: "", Children: &[]*swyapi.RepoFile{} }
	dirs := []*swyapi.RepoFile{&root}

	for len(dirs) > 0 {
		dir := dirs[0]
		dirs = dirs[1:]

		ents, err := ioutil.ReadDir(rp + "/" + dir.Path)
		if err != nil {
			return nil, GateErrM(swyapi.GateFsError, "Cannot list files in repo")
		}

		for _, ent := range ents {
			e := &swyapi.RepoFile{
				Label:	ent.Name(),
			}
			if dir.Path == "" {
				e.Path = ent.Name()
			} else {
				e.Path = dir.Path + "/" + ent.Name()
			}

			if ent.IsDir() {
				if e.Label == ".git" {
					continue
				}

				e.Type = "dir"
				e.Children = &[]*swyapi.RepoFile{}
				dirs = append(dirs, e)
			} else {
				e.Type = "file"
				lng := rtLangDetect(e.Label)
				e.Lang = &lng
			}

			l := *dir.Children
			l = append(l, e)
			dir.Children = &l
		}
	}

	return *root.Children, nil
}

var repoSyncDelay time.Duration

func (rd *RepoDesc)pullManual(ctx context.Context) *xrest.ReqErr {
	if rd.LastPull != nil && time.Now().Before(rd.LastPull.Add(repoSyncDelay)) {
		return GateErrM(swyapi.GateNotAvail, "To frequent sync")
	}

	rd.pullAsync()
	repoPulls.WithLabelValues("manual").Inc()
	return nil
}

func (rd *RepoDesc)changedFiles(ctx context.Context, till string) ([]string, error) {
	if rd.Commit == "" {
		/* Pre-configured repos might have this unset */
		return []string{}, nil
	}

	cmd := exec.Command("git", "-C", rd.clonePath(), "diff", "--name-only", rd.Commit, till)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("Err get git out: " + err.Error())
	}

	sc := bufio.NewScanner(out)
	err = cmd.Start()
	if err != nil {
		return nil, errors.New("Err start git: " + err.Error())
	}
	var ret []string
	for sc.Scan() {
		ret = append(ret, sc.Text())
	}
	cmd.Wait()

	if err := sc.Err(); err != nil {
		return nil, errors.New("Err reading lines: " + err.Error())
	}

	return ret, nil
}

func filesMatch(file string, files []string) bool {
	for _, f := range files {
		if f == file {
			return true
		}
	}
	return false
}

func listFunctionsToUpdate(ctx context.Context, rd *RepoDesc, to string) []*FunctionDesc {
	var ret []*FunctionDesc

	files, err := rd.changedFiles(ctx, to)
	if err != nil || len(files) == 0 {
		if err != nil {
			ctxlog(ctx).Errorf("Can't list changed files: %s", err.Error())
		}
		return ret
	}

	var fn FunctionDesc

	iter := dbIterAll(ctx, bson.M{"src.repo": rd.ObjID.Hex()}, &fn)
	defer iter.Close()

	for iter.Next(&fn) {
		if filesMatch(fn.Src.File, files) {
			aux := fn /* Do copy fn, next iter.Next() would overwrite it */
			ret = append(ret, &aux)
		}
	}

	err = iter.Err()
	if err != nil {
		ctxlog(ctx).Errorf("Can't query functions to update: %s", err.Error())
	}

	return ret
}

func tryToUpdateFunctions(ctx context.Context, rd *RepoDesc, to string) {
	if rd.Commit == to {
		/* Common "already up-to-date" case */
		return
	}

	ctxlog(ctx).Debugf("Updated repo %s [%s -> %s]\n", rd.ObjID.Hex(), rd.Commit, to)

	fns := listFunctionsToUpdate(ctx, rd, to)
	for _, fn := range(fns) {
		ctxlog(ctx).Debugf("Update function %s from %s", fn.SwoId.Str(), fn.Src.File)
		t := gctx(ctx).tpush(fn.SwoId.Tennant)
		traceFnEvent(ctx, "update from repo", fn)
		cerr := fn.updateSources(ctx, &swyapi.FunctionSources {
			Repo: fn.Src.Repo + "/" + fn.Src.File,
			Sync: true,
		})
		gctx(ctx).tpop(t)
		if cerr != nil {
			ctxlog(ctx).Errorf("Error auto-updating sources: %s", cerr.Message)
			logSaveEvent(ctx, fn.Cookie, "FAIL repo auto-update")
		}
	}
}

type pullReq struct {
	r	*RepoDesc
	done	chan error
}

type scrapeReq struct {
	r	*RepoDesc
	done	chan error
}

var pulls chan *pullReq
var scrapes chan *scrapeReq

func init() {
	pulls = make(chan *pullReq)
	scrapes = make(chan *scrapeReq)
	go func() {
		for rq := range pulls {
			ctx, done := mkContext("::repo-pull")
			err := rq.r.pull(ctx)
			if rq.done != nil {
				rq.done <-err
			}
			if err != nil {
				repoPllErrs.Inc()
			}
			done(ctx)
		}
	}()
	go func() {
		for rq := range scrapes {
			ctx, done := mkContext("::repo-scrape")
			err := rq.r.scrape(ctx)
			if rq.done != nil {
				rq.done <-err
			}
			if err != nil {
				repoScrapeErrs.Inc()
			}
			done(ctx)
		}
	}()
}

func (rd *RepoDesc)pullSync() *xrest.ReqErr {
	rq := pullReq{rd, make(chan error)}
	pulls <-&rq

	err := <-rq.done
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	return nil
}

func (rd *RepoDesc)pullAsync() {
	rq := pullReq{rd, nil}
	pulls <-&rq
}

func (rd *RepoDesc)dirty() {
	scrapes <-&scrapeReq{r:rd, done:nil}
}

func (rd *RepoDesc)scrape(ctx context.Context) error {
	du, err := xh.GetDirDU(rd.clonePath())
	if err != nil {
		return err
	}

	dbUpdatePart(ctx, rd, bson.M{"du": du})
	return nil
}

func (rd *RepoDesc)pull(ctx context.Context) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()

	cmd := exec.Command("git", "-C", clone_to, "pull")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't pull %s -> %s: %s (%s:%s)",
			rd.URL(), clone_to, err.Error(),
			stdout.String(), stderr.String())
		logSaveResult(ctx, rd.SwoId.PCookie(), "repo_pull", stdout.String(), stderr.String())
		return err
	}

	cmt, err := gitCommit(clone_to)
	if err == nil {
		t := time.Now()
		dbUpdatePart(ctx, rd, bson.M{"commit": cmt, "last_pull": &t})
		rd.dirty()
		tryToUpdateFunctions(ctx, rd, cmt)
	}

	return nil
}

func pullRepos(ctx context.Context, ts time.Time) (int, error) {
	var rds []*RepoDesc

	err := dbFindAll(ctx, bson.M{
			"pulling": PullPeriodic,
			"last_pull": bson.M{"$lt": ts},
		}, &rds)
	if err != nil {
		if !dbNF(err) {
			ctxlog(ctx).Errorf("Can't get repos to sync: %s", err.Error())
		} else {
			err = nil
		}

		return 0, err
	}

	synced := 0

	for _, rd := range rds {
		if rd.pullSync() == nil {
			repoPulls.WithLabelValues("priodic").Inc()
			synced++
		}
	}

	return synced, nil
}

var repoSyncPeriod time.Duration
var repoResyncOnError time.Duration = 5 * time.Minute

func init() {
	addTimeSysctl("repo_resync_on_error", &repoResyncOnError)
}

func periodicPullRepos() {
	for {
		ctx, done := mkContext("::reposync")

		t := time.Now()
		nxt := repoSyncPeriod

		synced, err := pullRepos(ctx, t.Add(-nxt))
		if err != nil {
			nxt = repoResyncOnError
		}

		t = t.Add(nxt)
		if synced != 0 {
			ctxlog(ctx).Debugf("Synced %d repos, next at %s", synced, t.String())
		}

		done(ctx)
		<-time.After(t.Sub(time.Now()))
	}
}

var demoRep RepoDesc

func ReposInit(ctx context.Context) error {
	go periodicPullRepos()

	ctxlog(ctx).Debugf("Resolve %s repo", conf.DemoRepo.URL)
	q := bson.M{
		"type": "github",
		"name": conf.DemoRepo.URL,
		"tennant": "*",
		"project": NoProject,
		"state": DBRepoStateRdy,
	}
	err := dbFind(ctx, q, &demoRep)
	if err != nil && ! dbNF(err) {
		return err
	}

	ctxlog(ctx).Debugf("Resolved remo repo: %s", demoRep.ObjID.Hex())
	return nil
}

func listReposGH(ac *AccDesc) ([]*GitHubRepo, error) {
	var rq *xhttp.RestReq

	t, err := ac.Secrets["token"].value()
	if err != nil {
		return nil, err
	}

	if t == "" {
		rq = &xhttp.RestReq{
			Address: "https://api.github.com/users/" + ac.SwoId.Name + "/repos",
			Method: "GET",
		}
	} else {
		rq = &xhttp.RestReq{
			Address: "https://api.github.com/user/repos?access_token=" + t,
			Method: "GET",
		}
	}

	rsp, err := xhttp.Req(rq, nil)
	if err != nil {
		return nil, err
	}

	var grs []*GitHubRepo
	err = xhttp.RResp(rsp, &grs)
	if err != nil {
		return nil, err
	}

	return grs, nil
}

type DetachedRepo struct {
	typ	string
	URL	string
	accid	string
}

func (rd *DetachedRepo)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.RepoInfo {
		Type:	rd.typ,
		URL:	rd.URL,
		State:	"unattached",
		AccID:	rd.accid,
	}, nil
}

func (rd *DetachedRepo)Del(context.Context) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }
func (rd *DetachedRepo)Upd(context.Context, interface{}) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }
func (rd *DetachedRepo)Add(context.Context, interface{}) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }

func (_ Repos)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	return repoFindForReq(ctx, r, r.Method == "GET")
}

func iterAttached(ctx context.Context, accid string, cb func(context.Context, xrest.Obj) *xrest.ReqErr, urls map[string]bool) *xrest.ReqErr {
	var rp RepoDesc

	q := bson.D{
		{"tennant", bson.M {
			"$in": []string{gctx(ctx).Tenant, "*" },
		}},
		{"project", NoProject},
	}

	iter := dbIterAll(ctx, q, &rp)
	defer iter.Close()

	for iter.Next(&rp) {
		if accid != "" && accid != rp.AccID.Hex() {
			continue
		}

		cerr := cb(ctx, &rp)
		if cerr != nil {
			return cerr
		}

		urls[rp.URL()] = true
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func iterFromAccounts(ctx context.Context, accid string, cb func(context.Context, xrest.Obj) *xrest.ReqErr, urls map[string]bool) *xrest.ReqErr {
	/* XXX -- maybe cache repos in a DB? */
	var ac AccDesc

	q := bson.M{"type": "github", "tennant":  gctx(ctx).Tenant}
	if accid != "" {
		q["_id"] = bson.ObjectIdHex(accid)
	}

	iter := dbIterAll(ctx, q, &ac)
	defer iter.Close()

	for iter.Next(&ac) {
		grs, err := listReposGH(&ac)
		if err != nil {
			ctxlog(ctx).Errorf("Can't list GH repos: %s", err.Error())
			continue
		}

		for _, gr := range grs {
			if _, ok := urls[gr.URL]; ok {
				continue
			}

			rd := &DetachedRepo{}
			rd.typ = "github"
			rd.URL = gr.URL
			if gr.Private {
				rd.accid = ac.ObjID.Hex()
			}

			cerr := cb(ctx, rd)
			if cerr != nil {
				return cerr
			}

			urls[gr.URL] = true
		}
	}

	err := iter.Err()
	if err != nil && !dbNF(err) {
		return GateErrD(err)
	}

	return nil
}

func (_ Repos)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	accid := q.Get("aid")
	if accid != "" && !bson.IsObjectIdHex(accid) {
		return GateErrM(swyapi.GateBadRequest, "Bad account ID value")
	}

	att := q.Get("attached")

	urls := make(map[string]bool)

	if att == "" || att == "true" {
		cerr := iterAttached(ctx, accid, cb, urls)
		if cerr != nil {
			return cerr
		}
	}

	if att == "" || att == "false" {
		cerr := iterFromAccounts(ctx, accid, cb, urls)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func ctxRepoId(ctx context.Context, rid string) bson.M {
	return  bson.M{
		"tennant": bson.M { "$in": []string{gctx(ctx).Tenant, "*"}},
		"_id": bson.ObjectIdHex(rid),
	}
}

func ctxRepoName(ctx context.Context, name string) bson.M {
	return  bson.M{
		"tennant": bson.M { "$in": []string{gctx(ctx).Tenant, "*"}},
		"name": name,
	}
}

func repoReadFile(ctx context.Context, rf string) ([]byte, error) {
	fname, err := repoFilePath(ctx, rf)
	if err != nil {
		return nil, err
	}

	return ioutil.ReadFile(fname)
}

func repoOpenFile(ctx context.Context, rf string) (io.ReadCloser, error) {
	fname, err := repoFilePath(ctx, rf)
	if err != nil {
		return nil, err
	}

	return os.Open(fname)
}

func repoFilePath(ctx context.Context, rf string) (string, error) {
	var rd RepoDesc

	ids := strings.SplitN(rf, "/", 2)
	if len(ids) == 2 && bson.IsObjectIdHex(ids[0]) {
		err := dbFind(ctx, ctxRepoId(ctx, ids[0]), &rd)
		if err != nil {
			return "", err
		}

		return rd.clonePath() + "/" + ids[1], nil

	}

	ids = strings.SplitN(rf, "//", 3)
	if len(ids) == 3 {
		err := dbFind(ctx, ctxRepoName(ctx, ids[0] + "//" + ids[1]), &rd)
		if err != nil {
			return "", err
		}

		return rd.clonePath() + "/" + ids[2], nil
	}

	return "", errors.New("Bad repo file ID")
}
