package main

import (
	"github.com/gorilla/mux"
	"swifty/common/xrest"
	"swifty/apis"
	"net/http"
	"net/url"
	"context"
)

type Packages struct {
	Lang	string
}

type PackageDesc struct {
	SwoId
	Lang	string
}

func (ps Packages)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.PkgAdd)
	_, ok := rt_handlers[ps.Lang]
	if !ok {
		return nil, GateErrM(swyapi.GateNotFound, "Language not supported")
	}

	id := ctxSwoId(ctx, NoProject, params.Name)
	return &PackageDesc {
		SwoId:	*id,
		Lang:	ps.Lang,
	}, nil
}

func (ps Packages)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	id := ctxSwoId(ctx, NoProject, mux.Vars(r)["pkgid"])
	return &PackageDesc {
		SwoId:	*id,
		Lang:	ps.Lang,
	}, nil
}

func (ps Packages)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	h, ok := rt_handlers[ps.Lang]
	if !ok {
		return GateErrC(swyapi.GateNotAvail)
	}

	pkgs, cerr := rtListPackages(ctx, h)
	if cerr != nil {
		return cerr
	}
	id := ctxSwoId(ctx, NoProject, "")

	for _, pkg := range pkgs {
		id.Name = pkg.Name
		cerr := cb(ctx, &PackageDesc {
			SwoId: *id,
			Lang:	ps.Lang,
		})
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (pkg *PackageDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	h := rt_handlers[pkg.Lang]
	return rtInstallPackage(ctx, h, pkg.SwoId)
}

func (pkg *PackageDesc)Del(ctx context.Context) *xrest.ReqErr {
	h := rt_handlers[pkg.Lang]
	return rtRemovePackage(ctx, h, pkg.SwoId)
}

func (pkg *PackageDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.PkgInfo{
		Id:	pkg.SwoId.Name,
	}, nil
}

func (pkg *PackageDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}
