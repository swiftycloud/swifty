package xrest

import (
	"net/http"
	"context"
	"../http"
)

var TraceFn func(context.Context, interface{})

func Respond(ctx context.Context, w http.ResponseWriter, result interface{}) *ReqErr {
	err := swyhttp.MarshalAndWrite(w, result)
	if err != nil {
		return &ReqErr{3 /* XXX: GateBadResp */, err.Error()}
	}

	if TraceFn != nil {
		TraceFn(ctx, result)
	}

	return nil
}

