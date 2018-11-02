package main

import (
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2/bson"
	"swifty/common/xrest"
	"path/filepath"
	"swifty/apis"
	"net/http"
	"net/url"
	"syscall"
	"context"
	"os"
)

type Packages struct {
	Lang	string
}

type PackageDesc struct {
	SwoId
	Lang	string
}

type PackagesStats struct {
	DU		uint64			`bson:"du,omitempty"`
}

type PackagesCache struct {
	ObjID		bson.ObjectId		`bson:"_id,omitempty"`
	Cookie		string			`bson:"cookie"`
	Tenant		string			`bson:"tenant"`

	Packages	map[string][]*swyapi.Package	`bson:"packages,omitempty"`
	Stats		map[string]*PackagesStats	`bson:"stats,omitempty"`
}

func init() {
	addSysctl("pkg_cache_flush", func() string { return "Set any value here" },
		func(_ string) error {
			ctx, done := mkContext("::pkg-flush")
			defer done(ctx)
			dbPackagesFlushAll(ctx)
			return nil
		})
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

	var pkgs []*swyapi.Package

	pc, _ := dbPackagesFind(ctx)
	if pc != nil && pc.Packages != nil {
		pkgs = pc.Packages[ps.Lang]
	}

	if pkgs == nil {
		var cerr *xrest.ReqErr

		pkgs, cerr = rtListPackages(ctx, h)
		if cerr != nil {
			return cerr
		}

		dbPackagesUpdateList(ctx, ps.Lang, pkgs)
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

/*
 * When installing a package we check current packages disk size
 * before the installation itself, so here's some "gap" between
 * the current usage and the limit at which we stop new installations
 */
var pkgLimitGap uint64 = uint64(32) << 10

func init() {
	addMemSysctl("pkg_disk_size_gap", &pkgLimitGap)
}

func (pkg *PackageDesc)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	td, err := tendatGet(ctx)
	if err != nil {
		return GateErrC(swyapi.GateGenErr)
	}

	if td.pkgl != nil && td.pkgl.DiskSizeK != 0 {
		ps, cer := packagesGetStats(ctx, false)
		if cer != nil {
			return cer
		}

		/* ps.DU is in Kb, pkgl.DiskSizeK is in Kb too */
		if ps.DU + (pkgLimitGap<<10) > td.pkgl.DiskSizeK {
			return GateErrM(swyapi.GateGenErr, "Too many packages already")
		}
	}

	h := rt_handlers[pkg.Lang]
	cerr := rtInstallPackage(ctx, h, pkg.SwoId)
	if cerr == nil {
		dbPackagesFlushList(ctx, pkg.Lang)
		rescanKick(ctx, pkg.Lang, false)
	}
	return cerr
}

func (pkg *PackageDesc)Del(ctx context.Context) *xrest.ReqErr {
	h := rt_handlers[pkg.Lang]
	cerr := rtRemovePackage(ctx, h, pkg.SwoId)
	if cerr == nil {
		dbPackagesFlushList(ctx, pkg.Lang)
		rescanKick(ctx, pkg.Lang, false)
	}
	return cerr
}

func (pkg *PackageDesc)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return &swyapi.PkgInfo{
		Id:	pkg.SwoId.Name,
	}, nil
}

func (pkg *PackageDesc)Upd(ctx context.Context, upd interface{}) *xrest.ReqErr {
	return GateErrC(swyapi.GateNotAvail)
}

func packagesStats(ctx context.Context, r *http.Request) (*swyapi.PkgStat, *xrest.ReqErr) {
	return packagesGetStats(ctx, true)
}

func packagesGetStats(ctx context.Context, brokenout bool) (*swyapi.PkgStat, *xrest.ReqErr) {
	ps, _ := dbPackagesFind(ctx)
	if ps == nil || ps.Stats == nil {
		rescanKick(ctx, "", true)

		var err error

		ps, err = dbPackagesFind(ctx)
		if err == nil {
			goto ok
		}

		return nil, GateErrD(err)
	}

ok:
	ret := &swyapi.PkgStat {}
	if brokenout {
		ret.Lang = map[string]*swyapi.PkgLangStat{}
	}

	var tot uint64
	for l, ls := range ps.Stats {
		tot += ls.DU
		if brokenout {
			ret.Lang[l] = &swyapi.PkgLangStat{ DU: ls.DU >> 10 }
		}
	}

	ret.DU = tot >> 10

	return ret, nil
}

type pkgScanReq struct {
	Ten	string
	Lang	string
	Sync	chan bool
}

var pkgScan chan *pkgScanReq

func getDirDU(dir string) (uint64, error) {
	var size uint64

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		stat, _ := info.Sys().(*syscall.Stat_t)
		size += uint64(stat.Blocks << 9)
		return nil
	})

	return size, err
}

func langStatScan(rq *pkgScanReq) {
	ctx, done := mkContext("::pkgscan")
	defer done(ctx)

	if rq.Lang != "" {
		langStatScanOne(ctx, rq)
	} else {
		for l, _ := range rt_handlers {
			rq.Lang = l
			langStatScanOne(ctx, rq)
		}
	}
}

func langStatScanOne(ctx context.Context, rq *pkgScanReq) {
	ctxlog(ctx).Debugf("Will re-scan %s/%s packages", rq.Ten, rq.Lang)
	du, err := getDirDU(packagesDir() + "/" + rq.Ten + "/" + rq.Lang)
	if err != nil {
		ctxlog(ctx).Errorf("Cannot san %s/%s packages", rq.Ten, rq.Lang)
		return
	}

	err = dbPackagesUpdateDU(ctx, rq.Ten, rq.Lang, du)
	if err != nil {
		ctxlog(ctx).Errorf("Cannot update %s/%s pkg stats", rq.Ten, rq.Lang)
		return
	}
}

func rescanKick(ctx context.Context, lang string, sync bool) {
	rq := pkgScanReq {
		Ten: gctx(ctx).Tenant,
		Lang: lang,
	}

	if sync {
		rq.Sync = make(chan bool)
	}

	pkgScan <-&rq

	if sync {
		<-rq.Sync
	}
}

func init() {
	pkgScan = make(chan *pkgScanReq)
	go func() {
		for {
			x := <-pkgScan
			pkgScans.Inc()
			langStatScan(x)
			if x.Sync != nil {
				x.Sync <-true
			}
		}
	}()
}
