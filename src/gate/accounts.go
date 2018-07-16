package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
	"../apis/apps"
)

type AccDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Type		string		`bson:"type"`
}

func getAccDesc(id *SwoId, params *swyapi.AccAdd) *AccDesc {
	return &AccDesc {
		SwoId:	*id,
		Type:	params.Type,
	}
}

func (ad *AccDesc)toInfo(ctx context.Context, conf *YAMLConf, details bool) (*swyapi.AccInfo, *swyapi.GateErr) {
	return &swyapi.AccInfo {
		ID:	ad.ObjID.Hex(),
		Type:	ad.Type,
	}, nil
}

func (ad *AccDesc)Add(ctx context.Context, conf *YAMLConf) (string, *swyapi.GateErr) {
	ad.ObjID = bson.NewObjectId()

	err := dbInsert(ctx, ad)
	if err != nil {
		return "", GateErrD(err)
	}

	return ad.ObjID.Hex(), nil
}

func (ad *AccDesc)Del(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	err := dbRemoveId(ctx, &AccDesc{}, ad.ObjID)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}
