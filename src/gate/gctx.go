package main

import (
	"context"
	"sync/atomic"
	"gopkg.in/mgo.v2"
)

type gateContext struct {
	context.Context
	Tenant	string
	Admin	bool
	ReqId	uint64
	S	*mgo.Session
}

var reqIds uint64

func mkContext2(tenant string, admin bool) (context.Context, func(context.Context)) {
	gatectx := &gateContext{
		context.Background(),
		tenant,
		admin,
		atomic.AddUint64(&reqIds, 1),
		session.Copy(),
	}

	return gatectx, func(ctx context.Context) { gctx(ctx).S.Close() }
}

func mkContext(tenant string) (context.Context, func(context.Context)) {
	return mkContext2(tenant, true) /* Internal contexts are admin always! */
}

func gctx(ctx context.Context) *gateContext {
	return ctx.(*gateContext)
}

func (gx *gateContext)tpush(tenant string) string {
	x := gx.Tenant
	gx.Tenant = tenant
	return x
}

func (gx *gateContext)tpop(tenant string) {
	gx.Tenant = tenant
}
