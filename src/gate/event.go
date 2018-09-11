package main

import (
	"strings"
	"context"
	"path/filepath"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../apis"
)

type EventOps struct {
	setup	func(*FnEventDesc, *swyapi.FunctionEvent)
	start	func(context.Context, *FunctionDesc, *FnEventDesc) error
	stop	func(context.Context, *FnEventDesc) error
}

var evtHandlers = map[string]*EventOps {
	"cron":	&cronOps,
	"s3":	&s3EOps,
	"url":	&urlEOps,
}

type FnEventS3 struct {
	Ns		string		`bson:"ns"`
	Bucket		string		`bson:"bucket"`
	Ops		string		`bson:"ops"`
	Pattern		string		`bson:"pattern"`
}

func (s3 *FnEventS3)hasOp(op string) bool {
	ops := strings.Split(s3.Ops, ",")
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

func (s3 *FnEventS3)matchPattern(oname string) bool {
	if s3.Pattern == "" {
		return true
	}

	m, err := filepath.Match(s3.Pattern, oname)
	return err == nil && m
}

type FnEventDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	Name		string		`bson:"name"`
	Source		string		`bson:"source"`
	Cron		*FnEventCron	`bson:"cron,omitempty"`
	S3		*FnEventS3	`bson:"s3,omitempty"`
}

func eventsInit(ctx context.Context, conf *YAMLConf) error {
	return cronInit(ctx, conf)
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

	evtHandlers[ed.Source].start(ctx, fn, ed)
	if err != nil {
		dbRemove(ctx, ed)
		return GateErrM(swy.GateGenErr, "Can't setup event")
	}

	err = dbUpdateAll(ctx, ed)
	if err != nil {
		eventStop(ctx, ed)
		dbRemove(ctx, ed)
		return GateErrD(err)
	}

	return nil
}

func eventStop(ctx context.Context, ed *FnEventDesc) error {
	return evtHandlers[ed.Source].stop(ctx, ed)
}

func (ed *FnEventDesc)Delete(ctx context.Context, fn *FunctionDesc) *swyapi.GateErr {
	err := eventStop(ctx, ed)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Can't stop event")
	}

	err = dbRemove(ctx, ed)
	if err != nil {
		return GateErrD(err)
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
		err = eventStop(ctx, e)
		if err != nil {
			return err
		}

		err = dbRemove(ctx, e)
		if err != nil {
			return err
		}
	}

	return nil
}
