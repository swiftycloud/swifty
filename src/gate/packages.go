package main

import (
	"gopkg.in/mgo.v2/bson"
	"swifty/common/xrest"
	"swifty/apis"
	"net/http"
	"net/url"
	"context"
)

const (
	DBPkgStateIns	string = "installing"
	DBPkgStateRem	string = "removing"
	DBPkgStateBrk	string = "broken"
	DBPkgStateRdy	string = "ready"
)

type Packages struct {
}

func (ps Packages)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.PkgAdd)
	h, ok := rt_handlers[params.Lang]
	if !ok || h.Install == nil {
		return nil, GateErrM(swyapi.GateNotFound, "Language not supported")
	}

	id := ctxSwoId(ctx, NoProject, params.Name)
	return &PackageDesc {
		SwoId:	*id,
		Lang:	params.Lang,
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

	lng := q.Get("lang")

	dbq := listReq(ctx, NoProject, []string{})
	if lng != "" {
		dbq = append(dbq, bson.DocElem{"lang", lng})
	}

	iter := dbIterAll(ctx, dbq, &pkg)
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
	State		string			`bson:"state"`
	Cookie		string			`bson:"cookie"`
	Lang		string			`bson:"lang"`
}

func (pkg *PackageDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	pkg.ObjID = bson.NewObjectId()
	pkg.Cookie = pkg.SwoId.Cookie()
	pkg.State = DBPkgStateIns

	err := dbInsert(ctx, pkg)
	if err != nil {
		return GateErrD(err)
	}

	go installPackage(pkg)

	return nil
}

func installPackage(pkg *PackageDesc) {
	ctx, done := mkContext("::pkg install")
	defer done(ctx)

	h := rt_handlers[pkg.Lang]
	err := h.Install(ctx, pkg.SwoId)

	if err != nil {
		pkg.State = DBPkgStateBrk
	} else {
		pkg.State = DBPkgStateRdy
	}

	dbUpdatePart(ctx, pkg, bson.M{ "state": pkg.State })
}

func (pkg *PackageDesc)Del(ctx context.Context) *xrest.ReqErr {
	err := dbUpdatePart(ctx, pkg, bson.M{"state": DBPkgStateRem})
	if err != nil {
		return GateErrD(err)
	}

	pkg.State = DBPkgStateRem

	h := rt_handlers[pkg.Lang]
	if h.Remove != nil {
		err = h.Remove(pkg.SwoId)
		if err != nil {
			return GateErrE(swyapi.GateFsError, err)
		}
	}

	err = dbRemove(ctx, pkg)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (pkg *PackageDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.PkgInfo{
		Id:	pkg.ObjID.Hex(),
		Name:	pkg.SwoId.Name,
		Lang:	pkg.Lang,
		State:	pkg.State,
	}, nil
}

func (pkg *PackageDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}
