/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xrest

import (
	"context"
	"net/url"
	"net/http"
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
	Get(context.Context, *http.Request) (Obj, *ReqErr)
	Create(context.Context, interface{}) (Obj, *ReqErr)
	Iterate(context.Context, url.Values, func(context.Context, Obj) *ReqErr) *ReqErr
}
