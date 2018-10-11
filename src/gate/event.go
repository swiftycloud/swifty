package main

import (
	"github.com/gorilla/mux"
	"context"
	"errors"
	"net/url"
	"net/http"
	"gopkg.in/mgo.v2/bson"
	"swifty/common/xrest"
	"swifty/apis"
)

type EventOps struct {
	setup	func(*FnEventDesc, *swyapi.FunctionEvent)
	start	func(context.Context, *FunctionDesc, *FnEventDesc) error
	stop	func(context.Context, *FnEventDesc) error
	cleanup	func(context.Context, *FnEventDesc)
}

var evtHandlers = map[string]*EventOps {
	"cron":	&cronOps,
	"s3":	&s3EOps,
	"url":	&urlEOps,
	"websocket": &wsEOps,
}

type FnEventDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	Name		string		`bson:"name"`
	Source		string		`bson:"source"`
	Cron		*FnEventCron	`bson:"cron,omitempty"`
	S3		*FnEventS3	`bson:"s3,omitempty"`
	WS		*FnEventWebsock	`bson:"ws,omitempty"`
}

type Trigger struct {
	ed	*FnEventDesc
	fn	*FunctionDesc
}

func (t *Trigger)Add(ctx context.Context, _ interface{}) *xrest.ReqErr {
	return t.ed.Add(ctx, t.fn)
}

func (t *Trigger)Del(ctx context.Context) *xrest.ReqErr {
	return t.ed.Delete(ctx, t.fn)
}

func (t *Trigger)Info(ctx context.Context, q url.Values, details bool) (interface{}, *xrest.ReqErr) {
	return t.ed.toInfo(t.fn), nil
}

func (t *Trigger)Upd(context.Context, interface{}) *xrest.ReqErr { return GateErrC(swyapi.GateNotAvail) }

func eventsInit(ctx context.Context) error {
	return cronInit(ctx)
}

type Triggers struct {
	fn	*FunctionDesc
}

func (ts Triggers)Create(ctx context.Context, p interface{}) (xrest.Obj, *xrest.ReqErr) {
	params := p.(*swyapi.FunctionEvent)
	ed, cerr := getEventDesc(params)
	if cerr != nil {
		return nil, cerr
	}

	return &Trigger{ed, ts.fn}, nil
}

func (ts Triggers)Get(ctx context.Context, r *http.Request) (xrest.Obj, *xrest.ReqErr) {
	var fn FunctionDesc

	cerr := objFindForReq(ctx, r, "fid", &fn)
	if cerr != nil {
		return nil, cerr
	}

	eid := mux.Vars(r)["eid"]
	if !bson.IsObjectIdHex(eid) {
		return nil, GateErrM(swyapi.GateBadRequest, "Bad event ID")
	}

	var ed FnEventDesc

	err := dbFind(ctx, bson.M{"_id": bson.ObjectIdHex(eid), "fnid": fn.Cookie}, &ed)
	if err != nil {
		return nil, GateErrD(err)
	}

	return &Trigger{&ed, &fn}, nil
}

func (ts Triggers)Iterate(ctx context.Context, q url.Values, cb func(context.Context, xrest.Obj) *xrest.ReqErr) *xrest.ReqErr {
	ename := q.Get("name")

	var evd []*FnEventDesc
	var err error

	fn := ts.fn

	if ename == "" {
		err = dbFindAll(ctx, bson.M{"fnid": fn.Cookie}, &evd)
		if err != nil {
			return GateErrD(err)
		}
	} else {
		var ev FnEventDesc

		err = dbFind(ctx, bson.M{"fnid": fn.Cookie, "name": ename}, &ev)
		if err != nil {
			return GateErrD(err)
		}

		evd = append(evd, &ev)
	}

	for _, e := range evd {
		cerr := cb(ctx, &Trigger{e, fn})
		if cerr != nil {
			return cerr
		}
	}

	return nil
}

func (e *FnEventDesc)toInfo(fn *FunctionDesc) *swyapi.FunctionEvent {
	ae := swyapi.FunctionEvent{
		Id:	e.ObjID.Hex(),
		Name:	e.Name,
		Source:	e.Source,
	}

	if e.Source == "url" {
		ae.URL = fn.getURL()
	}

	if e.Cron != nil {
		ae.Cron = &swyapi.FunctionEventCron {
			Tab: e.Cron.Tab,
			Args: e.Cron.Args,
		}
	}

	if e.S3 != nil {
		ae.S3 = &swyapi.FunctionEventS3 {
			Bucket: e.S3.Bucket,
			Ops: e.S3.Ops,
			Pattern: e.S3.Pattern,
		}
	}

	return &ae
}

func getEventDesc(evt *swyapi.FunctionEvent) (*FnEventDesc, *xrest.ReqErr) {
	ed := &FnEventDesc{
		Name: evt.Name,
		Source: evt.Source,
	}

	h, ok := evtHandlers[evt.Source]
	if !ok {
		return nil, GateErrM(swyapi.GateBadRequest, "Unsupported event type")
	}

	h.setup(ed, evt)
	return ed, nil
}

func (ed *FnEventDesc)Add(ctx context.Context, fn *FunctionDesc) *xrest.ReqErr {
	var err error

	ed.ObjID = bson.NewObjectId()
	ed.FnId = fn.Cookie

	err = dbInsert(ctx, ed)
	if err != nil {
		return GateErrD(err)
	}

	h := evtHandlers[ed.Source]
	h.start(ctx, fn, ed)
	if err != nil {
		dbRemove(ctx, ed)
		return GateErrM(swyapi.GateGenErr, "Can't setup event")
	}

	err = dbUpdateAll(ctx, ed)
	if err != nil {
		h.stop(ctx, ed)
		dbRemove(ctx, ed)
		if h.cleanup != nil {
			h.cleanup(ctx, ed)
		}
		return GateErrD(err)
	}

	return nil
}

func (ed *FnEventDesc)Delete(ctx context.Context, fn *FunctionDesc) *xrest.ReqErr {
	h := evtHandlers[ed.Source]
	err := h.stop(ctx, ed)
	if err != nil {
		return GateErrM(swyapi.GateGenErr, "Can't stop event")
	}

	err = dbRemove(ctx, ed)
	if err != nil {
		return GateErrD(err)
	}

	if h.cleanup != nil {
		h.cleanup(ctx, ed)
	}
	return nil
}

func clearAllEvents(ctx context.Context, fn *FunctionDesc) error {
	var evs []*FnEventDesc

	err := dbFindAll(ctx, bson.M{"fnid": fn.Cookie}, &evs)
	if err != nil {
		return err
	}

	for _, e := range evs {
		cer := e.Delete(ctx, fn)
		if cer != nil {
			return errors.New(cer.Message)
		}
	}

	return nil
}
