package main

import (
	"context"
	"errors"
	"net/url"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../apis"
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
}

type FnEventDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	Name		string		`bson:"name"`
	Source		string		`bson:"source"`
	Cron		*FnEventCron	`bson:"cron,omitempty"`
	S3		*FnEventS3	`bson:"s3,omitempty"`
}

type Trigger struct {
	ed	*FnEventDesc
	fn	*FunctionDesc
}

func (t *Trigger)add(ctx context.Context, _ interface{}) *swyapi.GateErr {
	return t.ed.Add(ctx, t.fn)
}

func (t *Trigger)del(ctx context.Context) *swyapi.GateErr {
	return t.ed.Delete(ctx, t.fn)
}

func (t *Trigger)info(ctx context.Context, q url.Values, details bool) (interface{}, *swyapi.GateErr) {
	return t.ed.toInfo(t.fn), nil
}

func (t *Trigger)upd(context.Context, interface{}) *swyapi.GateErr { return GateErrC(swy.GateNotAvail) }

func eventsInit(ctx context.Context, conf *YAMLConf) error {
	return cronInit(ctx, conf)
}

type Triggers struct {
	fn	*FunctionDesc
}

func (ts Triggers)create(ctx context.Context, q url.Values, p interface{}) (Obj, *swyapi.GateErr) {
	params := p.(*swyapi.FunctionEvent)
	ed, cerr := getEventDesc(params)
	if cerr != nil {
		return nil, cerr
	}

	return &Trigger{ed, ts.fn}, nil
}

func (ts Triggers)iterate(ctx context.Context, q url.Values, cb func(context.Context, Obj) *swyapi.GateErr) *swyapi.GateErr {
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

func getEventDesc(evt *swyapi.FunctionEvent) (*FnEventDesc, *swyapi.GateErr) {
	ed := &FnEventDesc{
		Name: evt.Name,
		Source: evt.Source,
	}

	h, ok := evtHandlers[evt.Source]
	if !ok {
		return nil, GateErrM(swy.GateBadRequest, "Unsupported event type")
	}

	h.setup(ed, evt)
	return ed, nil
}

func (ed *FnEventDesc)Add(ctx context.Context, fn *FunctionDesc) *swyapi.GateErr {
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
		return GateErrM(swy.GateGenErr, "Can't setup event")
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

func (ed *FnEventDesc)Delete(ctx context.Context, fn *FunctionDesc) *swyapi.GateErr {
	h := evtHandlers[ed.Source]
	err := h.stop(ctx, ed)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Can't stop event")
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
