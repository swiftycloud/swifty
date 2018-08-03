package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"../apis/apps"
	"../common"
	"../common/http"
	"../common/crypto"
)

type GHDesc struct {
	Name		string		`bson:"name,omitempty"`
	CypToken	string		`bson:"token,omitempty"`
}

func (gd *GHDesc)Token() (string, error) {
	var err error
	t := gd.CypToken
	if t != "" {
		t, err = swycrypt.DecryptString(gateSecPas, t)
	}
	return t, err
}

type AccDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Cookie		string		`bson:"cookie"`
	Type		string		`bson:"type"`
	GH		*GHDesc		`bson:"gh,omitempty"`
}

var accHandlers = map[string] struct {
	setup func (*AccDesc, *swyapi.AccAdd) *swyapi.GateErr
	info func (*AccDesc, *swyapi.AccInfo, bool)
	update func (*AccDesc, *swyapi.AccUpdate) *swyapi.GateErr
} {
	"github":	{ setup: setupGithubAcc, info: infoGitHubAcc, update: updateGithubAcc },
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

	/* All secrets must be encrypted */
	if params.Token != "" {
		params.Token, err = swycrypt.EncryptString(gateSecPas, params.Token)
		if err != nil {
			return GateErrE(swy.GateGenErr, err)
		}
	}

	ad.GH = &GHDesc {
		Name:		params.Name,
		CypToken:	params.Token,
	}

	ad.Cookie = cookifyS(ad.Type, ad.GH.Name)

	return nil
}

func infoGitHubAcc(ad *AccDesc, info *swyapi.AccInfo, detail bool) {
	t, err := ad.GH.Token()
	if err == nil {
		if len(t) > 6 {
			t = t[:6] + "..."
		} else {
			t = ""
		}
	} else {
		t = "<broken>"
	}

	info.Name = ad.GH.Name
	info.Token = t
}

func updateGithubAcc(ad *AccDesc, params *swyapi.AccUpdate) *swyapi.GateErr {
	if params.Token != nil {
		ad.GH.CypToken = *params.Token
		if ad.GH.CypToken != "" {
			var err error

			ad.GH.CypToken, err = swycrypt.EncryptString(gateSecPas, ad.GH.CypToken)
			if err != nil {
				return GateErrE(swy.GateGenErr, err)
			}
		}
	}

	return nil
}

func getAccDesc(id *SwoId, params *swyapi.AccAdd) (*AccDesc, *swyapi.GateErr) {
	h, ok := accHandlers[params.Type]
	if !ok {
		return nil, GateErrM(swy.GateBadRequest, "Unknown acc type")
	}

	ad := &AccDesc { SwoId:	*id, Type: params.Type }
	cerr := h.setup(ad, params)
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

	h, _ := accHandlers[ad.Type]
	h.info(ad, ac, details)

	return ac, nil
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
	h, _ := accHandlers[ad.Type]
	cerr := h.update(ad, params)
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
