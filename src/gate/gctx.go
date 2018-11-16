/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"context"
	"strconv"
	"sync/atomic"
	"swifty/apis"
	"gopkg.in/mgo.v2"
)

type gateContext struct {
	context.Context
	Desc	string
	Tenant	string
	Role	string
	ReqId	uint64
	S	*mgo.Session
}

var reqIds uint64

func init() {
	addRoSysctl("gate_req_rover", func() string { return strconv.FormatUint(reqIds, 10) } )
}

func mkContext3(desc, tenant, role string) (context.Context, func(context.Context)) {
	gatectx := &gateContext{
		context.Background(),
		desc,
		tenant,
		role,
		atomic.AddUint64(&reqIds, 1),
		session.Copy(),
	}

	contextRuns.WithLabelValues(desc).Inc()

	return gatectx, func(ctx context.Context) { gctx(ctx).S.Close() }
}

func mkContext2(desc string, role string) (context.Context, func(context.Context)) {
	return mkContext3(desc, "", role)
}

func mkContext(desc string) (context.Context, func(context.Context)) {
	return mkContext2(desc, swyapi.AdminRole) /* Internal contexts are admin always! */
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

func (gx *gateContext)Admin() bool {
	return gx.Role == swyapi.AdminRole
}
