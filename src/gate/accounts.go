package main

import (
	"errors"
	"context"
	"strings"
	"gopkg.in/mgo.v2/bson"
	"../apis"
	"../common"
	"../common/http"
	"../common/crypto"
)

type CypToken string

type GenDesc struct {
	Name		string		`bson:"name,omitempty"`
	Tok		CypToken	`bson:"token,omitempty"`
}

func (ct CypToken)value() (string, error) {
	var err error
	t := string(ct)
	if t != "" {
		t, err = swycrypt.DecryptString(gateSecPas, t)
	}
	return t, err
}

func mkCypToken(v string) (CypToken, error) {
	if v != "" {
		if len(v) < 10 {
			return "", errors.New("Invalid token value")
		}

		var err error

		v, err = swycrypt.EncryptString(gateSecPas, v)
		if err != nil {
			return "", err
		}
	}

	return CypToken(v), nil
}

type AccDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`
	Type		string		`bson:"type"`
	Gen		*GenDesc	`bson:"gh,omitempty"`
}

func mkAccEnvName(typ, name, env string) string {
	return "ACC_" + strings.ToUpper(typ) + strings.ToUpper(name) + "_" + env
}

func infoGenAcc(ad *AccDesc, info *swyapi.AccInfo, detail bool) {
	t, err := ad.Gen.Tok.value()
	if err == nil {
		if len(t) > 6 {
			t = t[:6] + "..."
		} else {
			t = ""
		}
	} else {
		t = "<broken>"
	}

	info.Name = ad.Gen.Name
	info.Token = t
}

func setupGenAcc(ad *AccDesc, params *swyapi.AccAdd) *swyapi.GateErr {
	var err error

	ad.Gen = &GenDesc { Name: params.Name }
	ad.Gen.Tok, err = mkCypToken(params.Token)
	if err != nil {
		return GateErrE(swy.GateGenErr, err)
	}

	ad.Cookie = cookifyS(ad.SwoId.Tennant, ad.Type, ad.Gen.Name)

	return nil
}

func updateGenAcc(ad *AccDesc, params *swyapi.AccUpdate) *swyapi.GateErr {
	if params.Token != nil {
		var err error

		ad.Gen.Tok, err = mkCypToken(*params.Token)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
	}

	return nil
}

func getEnvGenAcc(ad *AccDesc) map[string]string {
	tok, _ := ad.Gen.Tok.value()
	return map[string]string {
		mkAccEnvName(ad.Type, ad.Gen.Name, "TOKEN"): tok,
	}
}

type acHandler struct {
	setup func (*AccDesc, *swyapi.AccAdd) *swyapi.GateErr
	info func (*AccDesc, *swyapi.AccInfo, bool)
	update func (*AccDesc, *swyapi.AccUpdate) *swyapi.GateErr
	getEnv func (*AccDesc) map[string]string
}

var accHandlers = map[string]acHandler {
	"github":	{
		setup:	setupGithubAcc,
		info:	infoGenAcc,
		update:	updateGenAcc,
		getEnv: getEnvGenAcc,
	},

	"telegram":	{
		setup:	setupGenAcc,
		info:	infoGenAcc,
		update:	updateGenAcc,
		getEnv: getEnvGenAcc,
	},
}

func (ad *AccDesc)handler() *acHandler {
	ah, ok := accHandlers[ad.Type]
	if !ok {
		ah = acHandler{
			setup:	setupGenAcc,
			info:	infoGenAcc,
			update:	updateGenAcc,
			getEnv: getEnvGenAcc,
		}
	}

	return &ah
}

func githubResolveName(token string) (string, error) {
	rsp, err := swyhttp.MarshalAndPost(&swyhttp.RestReq{
			Method: "GET",
			Address: "https://api.github.com/user?access_token=" + token,
		}, nil)
	if err != nil {
		return "", err
	}

	var u GitHubUser
	err = swyhttp.ReadAndUnmarshalResp(rsp, &u)
	if err != nil {
		return "", err
	}

	return u.Login, nil
}

func setupGithubAcc(ad *AccDesc, params *swyapi.AccAdd) *swyapi.GateErr {
	var err error

	/* If there's no name -- resolve it */
	if params.Name == "" {
		if params.Token == "" {
			return GateErrM(swy.GateBadRequest, "Need either name or token")
		}

		params.Name, err = githubResolveName(params.Token)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
	}

	return setupGenAcc(ad, params)
}

func getAccDesc(id *SwoId, params *swyapi.AccAdd) (*AccDesc, *swyapi.GateErr) {
	ad := &AccDesc { SwoId:	*id, Type: params.Type }
	cerr := ad.handler().setup(ad, params)
	if cerr != nil {
		return nil, cerr
	}

	return ad, nil
}

func (ad *AccDesc)toInfo(ctx context.Context, details bool) (*swyapi.AccInfo, *swyapi.GateErr) {
	ac := &swyapi.AccInfo {
		ID:	ad.ObjID.Hex(),
		Type:	ad.Type,
	}

	ad.handler().info(ad, ac, details)

	return ac, nil
}

func (ad *AccDesc)getEnv() map[string]string {
	return ad.handler().getEnv(ad)
}

func (ad *AccDesc)Add(ctx context.Context) *swyapi.GateErr {
	ad.ObjID = bson.NewObjectId()

	err := dbInsert(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (ad *AccDesc)Update(ctx context.Context, params *swyapi.AccUpdate) *swyapi.GateErr {
	cerr := ad.handler().update(ad, params)
	if cerr != nil {
		return cerr
	}

	err := dbUpdateAll(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}

func (ad *AccDesc)Del(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	err := dbRemove(ctx, ad)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}
