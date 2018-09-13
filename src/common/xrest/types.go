package xrest

import (
	"context"
	"net/url"
)

type Obj interface {
	Info(context.Context, url.Values, bool) (interface{}, *ReqErr)
	Del(context.Context) *ReqErr
	Upd(context.Context, interface{}) *ReqErr
	Add(context.Context, interface{}) *ReqErr
}

type Prop interface {
	Info(context.Context, Obj, url.Values) (interface{}, *ReqErr)
	Upd(context.Context, Obj, interface{}) *ReqErr
}

type Factory interface {
	Create(context.Context, interface{}) (Obj, *ReqErr)
	Iterate(context.Context, url.Values, func(context.Context, Obj) *ReqErr) *ReqErr
}
