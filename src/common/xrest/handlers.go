package xrest

import (
	"net/http"
	"context"
	"swifty/common/http"
)

var TraceFn func(context.Context, interface{})

func Respond(ctx context.Context, w http.ResponseWriter, result interface{}) *ReqErr {
	err := xhttp.Respond(w, result)
	if err != nil {
		return &ReqErr{BadResp, err.Error()}
	}

	if TraceFn != nil {
		TraceFn(ctx, result)
	}

	return nil
}

func HandleGetOne(ctx context.Context, w http.ResponseWriter, r *http.Request, desc Obj) *ReqErr {
	ifo, cerr := desc.Info(ctx, r.URL.Query(), true)
	if cerr != nil {
		return cerr
	}

	return Respond(ctx, w, ifo)
}

func HandleGetProp(ctx context.Context, w http.ResponseWriter, r *http.Request, o Obj, desc Prop) *ReqErr {
	ifo, cerr := desc.Info(ctx, o, r.URL.Query())
	if cerr != nil {
		return cerr
	}

	return Respond(ctx, w, ifo)
}

func HandleDeleteOne(ctx context.Context, w http.ResponseWriter, r *http.Request, desc Obj) *ReqErr {
	cerr := desc.Del(ctx)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func HandleUpdateOne(ctx context.Context, w http.ResponseWriter, r *http.Request, desc Obj, upd interface{}) *ReqErr {
	if upd == nil {
		return &ReqErr{2, "Not editable"}
	}

	err := xhttp.RReq(r, upd)
	if err != nil {
		return &ReqErr{BadRequest, err.Error()}
	}

	cerr := desc.Upd(ctx, upd)
	if cerr != nil {
		return cerr
	}

	ifo, _ := desc.Info(ctx, nil, false)
	return Respond(ctx, w, ifo)
}

func HandleUpdateProp(ctx context.Context, w http.ResponseWriter, r *http.Request, o Obj, desc Prop, upd interface{}) *ReqErr {
	if upd == nil {
		return &ReqErr{2, "Not editable"}
	}

	err := xhttp.RReq(r, upd)
	if err != nil {
		return &ReqErr{BadRequest, err.Error()}
	}

	cerr := desc.Upd(ctx, o, upd)
	if cerr != nil {
		return cerr
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func HandleCreateOne(ctx context.Context, w http.ResponseWriter, r *http.Request, fact Factory, add interface{}) *ReqErr {
	err := xhttp.RReq(r, add)
	if err != nil {
		return &ReqErr{BadRequest, err.Error()}
	}

	desc, cerr := fact.Create(ctx, add)
	if cerr != nil {
		return cerr
	}

	cerr = desc.Add(ctx, add)
	if cerr != nil {
		return cerr
	}

	ifo, _ := desc.Info(ctx, nil, false)
	return Respond(ctx, w, ifo)
}

func HandleGetList(ctx context.Context, w http.ResponseWriter, r *http.Request, fact Factory) *ReqErr {
	var ifos []interface{}

	q := r.URL.Query()
	details := (q.Get("details") != "")

	cerr := fact.Iterate(ctx, q, func(ctx context.Context, desc Obj) *ReqErr {
		ifo, cer2 := desc.Info(ctx, q, details)
		if cer2 != nil {
			return cer2
		}

		ifos = append(ifos, ifo)
		return nil
	})
	if cerr != nil {
		return cerr
	}

	return Respond(ctx, w, ifos)
}

func HandleMany(ctx context.Context, w http.ResponseWriter, r *http.Request, f Factory, add_param interface{}) *ReqErr {
	switch r.Method {
	case "GET":
		return HandleGetList(ctx, w, r, f)

	case "POST":
		return HandleCreateOne(ctx, w, r, f, add_param)
	}

	return nil
}

func HandleOne(ctx context.Context, w http.ResponseWriter, r *http.Request, f Factory, upd_param interface{}) *ReqErr {
	o, cer := f.Get(ctx, r)
	if cer != nil {
		return cer
	}

	switch r.Method {
	case "GET":
		return HandleGetOne(ctx, w, r, o)

	case "PUT":
		return HandleUpdateOne(ctx, w, r, o, upd_param)

	case "DELETE":
		return HandleDeleteOne(ctx, w, r, o)
	}

	return nil
}

func HandleProp(ctx context.Context, w http.ResponseWriter, r *http.Request, f Factory, p Prop, upd_param interface{}) *ReqErr {
	o, cer := f.Get(ctx, r)
	if cer != nil {
		return cer
	}

	switch r.Method {
	case "GET":
		return HandleGetProp(ctx, w, r, o, p)

	case "PUT":
		return HandleUpdateProp(ctx, w, r, o, p, upd_param)
	}

	return nil
}
