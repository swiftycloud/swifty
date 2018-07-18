package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"../apis/apps"
	"../common"
)

type GHDesc struct {
	Name		string		`bson:"name,omitempty"`
	Token		string		`bson:"token,omitempty"`
}

type AccDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Type		string		`bson:"type"`
	GH		*GHDesc		`bson:"gh,omitempty"`
}

var accHandlers = map[string] struct {
	setup func (*AccDesc, *swyapi.AccAdd)
	info func (*AccDesc, *swyapi.AccInfo, bool)
	update func (*AccDesc, *swyapi.AccUpdate)
} {
	"github":	{ setup: setupGithubAcc, info: infoGitHubAcc, update: updateGithubAcc },
}

func setupGithubAcc(ad *AccDesc, params *swyapi.AccAdd) {
	ad.GH = &GHDesc {
		Name:	params.GHName,
		Token:	params.GHToken,
	}
}

func infoGitHubAcc(ad *AccDesc, info *swyapi.AccInfo, detail bool) {
	info.GHName = ad.GH.Name
	info.GHToken = ad.GH.Token
}

func updateGithubAcc(ad *AccDesc, params *swyapi.AccUpdate) {
	if params.GHToken != nil {
		ad.GH.Token = *params.GHToken
	}
}

func getAccDesc(id *SwoId, params *swyapi.AccAdd) (*AccDesc, *swyapi.GateErr) {
	h, ok := accHandlers[params.Type]
	if !ok {
		return nil, GateErrM(swy.GateBadRequest, "Unknown acc type")
	}

	ad := &AccDesc { SwoId:	*id, Type: params.Type }
	h.setup(ad, params)
	return ad, nil
}

func (ad *AccDesc)toInfo(ctx context.Context, conf *YAMLConf, details bool) (*swyapi.AccInfo, *swyapi.GateErr) {
	ac := &swyapi.AccInfo {
		ID:	ad.ObjID.Hex(),
		Type:	ad.Type,
	}

	h, _ := accHandlers[ad.Type]
	h.info(ad, ac, details)

	return ac, nil
}

func (ad *AccDesc)Add(ctx context.Context, conf *YAMLConf) (string, *swyapi.GateErr) {
	ad.ObjID = bson.NewObjectId()

	err := dbInsert(ctx, ad)
	if err != nil {
		return "", GateErrD(err)
	}

	return ad.ObjID.Hex(), nil
}

func (ad *AccDesc)Update(ctx context.Context, params *swyapi.AccUpdate) *swyapi.GateErr {
	h, _ := accHandlers[ad.Type]
	h.update(ad, params)
	err := dbUpdateObj(ctx, ad.ObjID, ad)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}

func (ad *AccDesc)Del(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	err := dbRemoveId(ctx, &AccDesc{}, ad.ObjID)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}
