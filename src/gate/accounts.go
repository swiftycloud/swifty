package main

import (
	"errors"
	"net/url"
	"net/http"
	"context"
	"strings"
	"gopkg.in/mgo.v2/bson"
	"../common/http"
	"../common/crypto"
	"../common/xrest"
	"../apis"
)

type Secret string

func (ct Secret)value() (string, error) {
	var err error
	t := string(ct)
	if t != "" {
		t, err = xcrypt.DecryptString(gateSecPas, t)
	}
	return t, err
}

func mkSecret(k, v string) (Secret, error) {
	if v != "" {
		if len(v) < 10 {
			return "", errors.New("Invalid secret value for " + k)
		}

		var err error

		v, err = xcrypt.EncryptString(gateSecPas, v)
		if err != nil {
			return "", err
		}
	}

	return Secret(v), nil
}

type AccDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Cookie		string			`bson:"cookie"`
	Type		string			`bson:"type"`
	Values		map[string]string	`bson:"values"`
	Secrets		map[string]Secret	`bson:"secrets"`
}

type Accounts struct {}

func mkAccEnvName(typ, name, env string) string {
	return "ACC_" + strings.ToUpper(typ) + strings.ToUpper(name) + "_" + strings.ToUpper(env)
}

type acHandler struct {
	setup func (*AccDesc) *xrest.ReqErr
}

var accHandlers = map[string]acHandler {
	"github":	{
		setup:	setupGithubAcc,
	},
}

func githubResolveName(token string) (string, error) {
	rsp, err := xhttp.MarshalAndPost(&xhttp.RestReq{
			Method: "GET",
			Address: "https://api.github.com/user?access_token=" + token,
		}, nil)
	if err != nil {
		return "", err
	}

	var u GitHubUser
	err = xhttp.ReadAndUnmarshalResp(rsp, &u)
	if err != nil {
		return "", err
	}

	return u.Login, nil
}

func setupGithubAcc(ad *AccDesc) *xrest.ReqErr {
	/* If there's no name -- resolve it */
	if ad.SwoId.Name == "" {
		var err error

		tok, ok := ad.Secrets["token"]
		if !ok || tok == "" {
			return GateErrM(swyapi.GateBadRequest, "Need either name or token")
		}

		v, err := tok.value()
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}

		ad.SwoId.Name, err = githubResolveName(v)
		if err != nil {
			return GateErrE(swyapi.GateGenErr, err)
		}
	}

	return nil
}

func (ad *AccDesc)fill(values map[string]string) *xrest.ReqErr {
	var err error

	for k, v := range(values) {
		switch k {
		case "id", "name", "type":
			continue
		case "token", "secret", "password", "key":
			ad.Secrets[k], err = mkSecret(k, v)
			if err != nil {
				return GateErrE(swyapi.GateGenErr, err)
			}
		default:
			ad.Values[k] = v
		}
	}

	return nil
}

func getAccDesc(id *SwoId, params map[string]string) (*AccDesc, *xrest.ReqErr) {
	ad := &AccDesc {
		SwoId:		*id,
		Type:		params["type"],
		Values:		make(map[string]string),
		Secrets:	make(map[string]Secret),
	}

	cerr := ad.fill(params)
	if cerr != nil {
		return nil, cerr
	}

	ah, ok := accHandlers[ad.Type]
	if ok {
		cerr := ah.setup(ad)
		if cerr != nil {
			return nil, cerr
		}
	}

	return ad, nil
}

func (_ Accounts)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var ac AccDesc

	cerr := objFindForReq(ctx, r, "aid", &ac)
	if cerr != nil {
		return nil, cerr
	}

	return &ac, nil
}

func (_ Accounts)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	var acs []*AccDesc

	rq := listReq(ctx, NoProject, []string{})
	if atype := q.Get("type"); atype != "" {
		rq = append(rq, bson.DocElem{"type", atype})
	}

	err := dbFindAll(ctx, rq, &acs)
	if err != nil {
		return GateErrD(err)
	}

	for _, ac := range acs {
		cerr := cb(ctx, ac)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (_ Accounts)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := *p.(*map[string]string)
	if _, ok := params["type"]; !ok {
		return nil, GateErrM(swyapi.GateBadRequest, "No type")
	}

	id := ctxSwoId(ctx, NoProject, params["name"])
	return getAccDesc(id, params)
}

type FnAccounts struct {
	Fn	*FunctionDesc
}

func (fa FnAccounts)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var acc AccDesc

	cerr := objFindForReq(ctx, r, "aid", &acc)
	if cerr != nil {
		return nil, cerr
	}

	return &FnAccount{Fn:fa.Fn, Acc:&acc}, nil
}

func (fa FnAccounts)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	var acc AccDesc

	cerr := objFindId(ctx, *p.(*string), &acc, nil)
	if cerr != nil {
		return nil, cerr
	}

	return &FnAccount{Fn:fa.Fn, Acc:&acc}, nil
}

func (fa FnAccounts)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	for _, aid := range fa.Fn.Accounts {
		fa := FnAccount{Fn: fa.Fn}
		ac, err := accFindByID(ctx, fa.Fn.SwoId, aid)
		if err == nil {
			fa.Acc = ac
		} else {
			fa.Id = aid
		}

		cerr := cb(ctx, &fa)
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

type FnAccount struct {
	Fn	*FunctionDesc
	Acc	*AccDesc
	Id	string
}

func (fa *FnAccount)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return fa.Fn.addAccount(ctx, fa.Acc)
}

func (fa *FnAccount)Del(ctx context.Context) *xrest.ReqErr {
	return fa.Fn.delAccount(ctx, fa.Acc)
}

func (fa *FnAccount)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	if fa.Acc != nil {
		return fa.Acc.toInfo(ctx, details), nil
	} else {
		return map[string]string{"id": fa.Id }, nil
	}
}

func (fa *FnAccount)Upd(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return GateErrM(swyapi.GateGenErr, "Not updatable")
}

func (ad *AccDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return ad.toInfo(ctx, details), nil
}

func (ad *AccDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return ad.Update(ctx, *upd.(*map[string]string))
}

func (ad *AccDesc)toInfo(ctx context.Context, details bool) map[string]string {
	ai := map[string]string {
		"id":		ad.ObjID.Hex(),
		"type":		ad.Type,
		"name":		ad.SwoId.Name,
	}

	for k, v := range(ad.Values) {
		ai[k] = v
	}

	for k, sv := range(ad.Secrets) {
		v, err := sv.value()
		if err != nil {
			v = "<BROKEN>"
		} else if len(v) > 6 {
			v = v[:6] + "..."
		} else {
			v = "..."
		}
		ai[k] = v
	}

	return ai
}

func (ad *AccDesc)getEnv() map[string]string {
	envs := make(map[string]string)

	for k, v := range(ad.Values) {
		en := mkAccEnvName(ad.Type, ad.SwoId.Name, k)
		envs[en] = v
	}

	for k, sv := range(ad.Secrets) {
		v, err := sv.value()
		if err == nil  {
			en := mkAccEnvName(ad.Type, ad.SwoId.Name, k)
			envs[en] = v
		}
	}

	return envs
}

func (ad *AccDesc)ID() string {
	return ad.Type + ":" + ad.Name
}

func accFindByID(ctx context.Context, id SwoId, aid string) (*AccDesc, error) {
	var ac AccDesc

	ps := strings.SplitN(aid, ":", 2)
	if len(ps) != 2 {
		return nil, errors.New("Bad AID")
	}

	id.Project = NoProject
	id.Name = ps[1]
	err := dbFind(ctx, bson.M{"cookie": id.Cookie2(ps[0])}, &ac)
	return &ac, err
}

func (ad *AccDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	ad.ObjID = bson.NewObjectId()
	ad.Cookie = ad.SwoId.Cookie2(ad.Type)

	err := dbInsert(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (ad *AccDesc)Update(ctx context.Context, upd map[string]string) *xrest.ReqErr {
	cerr := ad.fill(upd)
	if cerr != nil {
		return cerr
	}

	err := dbUpdateAll(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}

func (ad *AccDesc)Del(ctx context.Context) *xrest.ReqErr {
	err := dbRemove(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}
