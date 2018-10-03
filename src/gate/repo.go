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
	"context"
	"time"
	"io/ioutil"
	"../common/http"
	"../common/xrest"
	"../common"
	"../apis"
)

const (
	DBRepoStateCln	int = 1
	DBRepoStateRem	int = 2
	DBRepoStateStl	int = 3
	DBRepoStateRdy	int = 4
)

const (
	RepoDescFile	= ".swifty.yml"
)

var repStates = map[int]string {
	DBRepoStateCln:	"cloning",
	DBRepoStateStl:	"stalled",
	DBRepoStateRem:	"removing",
	DBRepoStateRdy:	"ready",
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

type GitHubUser struct {
	Login		string		`json:"login"`
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
	return getRepoDesc(id, params), nil
}

func (rd *RepoDesc)Add(ctx context.Context, p interface{}) *xrest.ReqErr {
	var acc *AccDesc
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
	r := &swyapi.RepoInfo {
		ID:		rd.ObjID.Hex(),
		Type:		rd.Type,
		URL:		rd.URL(),
		State:		repStates[rd.State],
		Commit:		rd.Commit,
		AccID:		rd.AccID.Hex(),
	}

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
		return "", err
	}

	return gitCommit(clone_to)
}

func bgClone(rd *RepoDesc, ac *AccDesc, rh *repoHandler) {
	ctx, done := mkContext("::gitclone")
	defer done(ctx)

	commit, err := rh.clone(ctx, rd, ac)
	if err != nil {
		/* FIXME -- keep logs and show them user */
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

func (rd *RepoDesc)pull(ctx context.Context) *xrest.ReqErr {
	if rd.LastPull != nil && time.Now().Before( rd.LastPull.Add(time.Duration(conf.RepoSyncRate) * time.Minute)) {
		return GateErrM(swyapi.GateNotAvail, "To frequent sync")
	}

	go func() {
		pctx, done := mkContext("::repo-pull")
		rd.pullSync(pctx)
		done(pctx)
	}()

	return nil
}

func (rd *RepoDesc)changedFiles(ctx context.Context, till string) ([]string, error) {
	if rd.Commit == "" {
		/* FIXME -- pre-configured repos might have this unset */
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

func tryToUpdateFunctions(ctx context.Context, rd *RepoDesc, to string) {
	if rd.Commit == to {
		/* Common "already up-to-date" case */
		return
	}

	ctxlog(ctx).Debugf("Updated repo %s [%s -> %s]\n", rd.ObjID.Hex(), rd.Commit, to)

	files, err := rd.changedFiles(ctx, to)
	if err != nil || len(files) == 0 {
		return
	}

	var fns []*FunctionDesc
	err = dbFindAll(ctx, bson.M{"src.repo": rd.ObjID.Hex()}, &fns)
	if err != nil {
		ctxlog(ctx).Errorf("Error listing functions to update: %s", err.Error())
		return
	}

	for _, fn := range(fns) {
		if !filesMatch(fn.Src.File, files) {
			continue
		}

		ctxlog(ctx).Debugf("Update function %s from %s", fn.SwoId.Str(), fn.Src.File)
		t := gctx(ctx).tpush(fn.SwoId.Tennant)
		cerr := fn.updateSources(ctx, &swyapi.FunctionSources {
			Type: "git",
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

func (rd *RepoDesc)pullSync(ctx context.Context) *xrest.ReqErr {
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
		return GateErrE(swyapi.GateGenErr, err)
	}

	cmt, err := gitCommit(clone_to)
	if err == nil {
		t := time.Now()
		dbUpdatePart(ctx, rd, bson.M{"commit": cmt, "last_pull": &t})
		tryToUpdateFunctions(ctx, rd, cmt)
	}

	return nil
}

func pullRepos(ctx context.Context, ts time.Time) (int, error) {
	var rds []*RepoDesc

	err := dbFindAll(ctx, bson.M{
			"pulling": "periodic",
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
		if rd.pullSync(ctx) == nil {
			synced++
		}
	}

	return synced, nil
}

func periodicPullRepos(period time.Duration) {
	for {
		ctx, done := mkContext("::reposync")

		t := time.Now()
		nxt := period

		synced, err := pullRepos(ctx, t.Add(-period))
		if err != nil {
			nxt = 5 * time.Minute /* Will try in 5 minutes */
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

func ReposInit(ctx context.Context, conf *YAMLConf) error {
	go periodicPullRepos(time.Duration(conf.RepoSyncPeriod) * time.Minute)

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

	rsp, err := xhttp.MarshalAndPost(rq, nil)
	if err != nil {
		return nil, err
	}

	var grs []*GitHubRepo
	err = xhttp.ReadAndUnmarshalResp(rsp, &grs)
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

func (_ Repos)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	accid := q.Get("aid")
	if accid != "" && !bson.IsObjectIdHex(accid) {
		return GateErrM(swyapi.GateBadRequest, "Bad account ID value")
	}

	att := q.Get("attached")

	urls := make(map[string]bool)

	if att == "" || att == "true" {
		var reps []*RepoDesc

		q := bson.D{
			{"tennant", bson.M {
				"$in": []string{gctx(ctx).Tenant, "*" },
			}},
			{"project", NoProject},
		}
		err := dbFindAll(ctx, q, &reps)
		if err != nil {
			return GateErrD(err)
		}

		for _, rp := range reps {
			if accid != "" && accid != rp.AccID.Hex() {
				continue
			}

			cerr := cb(ctx, rp)
			if cerr != nil {
				return cerr
			}

			urls[rp.URL()] = true
		}
	}

	if att == "" || att == "false" {
		/* FIXME -- maybe cache repos in a DB? */
		var acs []*AccDesc

		q := bson.M{"type": "github", "tennant":  gctx(ctx).Tenant}
		if accid != "" {
			q["_id"] = bson.ObjectIdHex(accid)
		}
		err := dbFindAll(ctx, q, &acs)
		if err != nil && !dbNF(err) {
			return GateErrD(err)
		}

		for _, ac := range acs {
			grs, err := listReposGH(ac)
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
	var rd RepoDesc
	var fname string

	ids := strings.SplitN(rf, "/", 2)
	if len(ids) == 2 && bson.IsObjectIdHex(ids[0]) {
		err := dbFind(ctx, ctxRepoId(ctx, ids[0]), &rd)
		if err != nil {
			return nil, err
		}

		fname = ids[1]
		goto got_rd

	}

	ids = strings.SplitN(rf, "//", 3)
	if len(ids) == 3 {
		err := dbFind(ctx, ctxRepoName(ctx, ids[0] + "//" + ids[1]), &rd)
		if err != nil {
			return nil, err
		}

		fname = ids[2]
		goto got_rd
	}

	return nil, errors.New("Bad repo file ID")

got_rd:
	return ioutil.ReadFile(rd.clonePath() + "/" + fname)
}
