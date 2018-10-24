package main

import (
	"gopkg.in/mgo.v2/bson"
	"swifty/common/xrest"
	"swifty/apis"
	"net/http"
	"net/url"
	"context"
)

type Packages struct {
	Lang	string
}

func (ps Packages)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.PkgAdd)
	id := ctxSwoId(ctx, NoProject, params.Name)
	return &PackageDesc {
		SwoId:	*id,
		Lang:	ps.Lang,
	}, nil
}

func (ps Packages)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var pkg PackageDesc

	cerr := objFindForReq(ctx, r, "pkgid", &pkg)
	if cerr != nil {
		return nil, cerr
	}

	return &pkg, nil
}

func (ps Packages)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	var pkg PackageDesc

	iter := dbIterAll(ctx, listReq(ctx, NoProject, []string{}), &pkg)
	defer iter.Close()

	for iter.Next(&pkg) {
		cerr := cb(ctx, &pkg)
		if cerr != nil {
			return cerr
		}
	}

	err := iter.Err()
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

type PackageDesc struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	SwoId					`bson:",inline"`
	Cookie		string			`bson:"cookie"`
	Lang		string			`bson:"lang"`
}

func (pkg *PackageDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	pkg.ObjID = bson.NewObjectId()
	pkg.Cookie = pkg.SwoId.Cookie()

	err := dbInsert(ctx, pkg)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (pkg *PackageDesc)Del(ctx context.Context) *xrest.ReqErr {
	err := dbRemove(ctx, pkg)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (pkg *PackageDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.PkgInfo{
		Id:	pkg.ObjID.Hex(),
		Name:	pkg.SwoId.Name,
	}, nil
}

func (pkg *PackageDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}
