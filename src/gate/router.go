package main

import (
	"../apis"
	"context"
	"gopkg.in/mgo.v2/bson"
)

type RouterDesc struct {
	// These objects are kept in Mongo, which requires the below two
	// fields to be present...
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Table		[]*swyapi.RouterEntry	`bson:"table"`
}

func getRouterDesc(id *SwoId, params *swyapi.RouterAdd) (*RouterDesc, *swyapi.GateErr) {
	rd := RouterDesc {
		SwoId:	*id,
		Table:	params.Table,
	}

	return &rd, nil
}

func (rt *RouterDesc)toInfo(ctx context.Context, details bool) *swyapi.RouterInfo {
	ri := swyapi.RouterInfo {
		Id:		rt.ObjID.Hex(),
		Name:		rt.SwoId.Name,
		Project:	rt.SwoId.Project,
	}

	return &ri
}

func (rd *RouterDesc)Create(ctx context.Context) *swyapi.GateErr {
	rd.ObjID = bson.NewObjectId()
	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (rd *RouterDesc)Remove(ctx context.Context) *swyapi.GateErr {
	err := dbRemove(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}
	return nil
}
