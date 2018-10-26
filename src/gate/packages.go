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
	h, ok := rt_handlers[ps.Lang]
	if !ok || h.Install == nil {
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
	h := rt_handlers[ps.Lang]
	if h.List == nil {
		return GateErrC(swyapi.GateNotAvail)
	}

	packages, err := h.List(ctx, gctx(ctx).Tenant)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	id := ctxSwoId(ctx, NoProject, "")

	for _, pkg := range packages {
		id.Name = pkg
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
	err := h.Install(ctx, pkg.SwoId)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	return nil
}

func (pkg *PackageDesc)Del(ctx context.Context) *xrest.ReqErr {
	h := rt_handlers[pkg.Lang]
	if h.Remove == nil {
		return GateErrC(swyapi.GateNotAvail)
	}

	err := h.Remove(ctx, pkg.SwoId)
	if err != nil {
		return GateErrE(swyapi.GateGenErr, err)
	}

	return nil
}

func (pkg *PackageDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.PkgInfo{
		Id:	pkg.SwoId.Name,
	}, nil
}

func (pkg *PackageDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}
