package main

import (
	"strings"
	"context"
	"path/filepath"
	"gopkg.in/robfig/cron.v2"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../apis/apps"
)

type FnEventCron struct {
	Tab		string			`bson:"tab"`
	Args		map[string]string	`bson:"args"`
	JobID		int			`bson:"eid"`
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

var cronRunner *cron.Cron

func cronEventStart(ctx context.Context, evt *FnEventDesc) error {
	id, err := cronRunner.AddFunc(evt.Cron.Tab, func() {
		cctx, done := mkContext("::cron")
		defer done(cctx)

		var fn FunctionDesc

		err := dbFind(cctx, bson.M{"cookie": evt.FnId}, &fn)
		if err != nil {
			glog.Errorf("Can't find FN %s to run Cron event", evt.FnId)
			return
		}

		if fn.State != swy.DBFuncStateRdy {
			return
		}

		_, err = doRun(cctx, &fn, "cron", &swyapi.SwdFunctionRun{Args: evt.Cron.Args})
		if err != nil {
			ctxlog(ctx).Errorf("cron: Error running FN %s", err.Error())
		}
	})

	if err == nil {
		evt.Cron.JobID = int(id)
	}

	return err
}

func cronEventStop(ctx context.Context, evt *FnEventDesc) error {
	cronRunner.Remove(cron.EntryID(evt.Cron.JobID))
	return nil
}

func eventsInit(ctx context.Context, conf *YAMLConf) error {
	cronRunner = cron.New()
	cronRunner.Start()

	var evs []*FnEventDesc

	err := dbFindAll(ctx, bson.M{"source":"cron"}, &evs)
	if err != nil {
		return err
	}

	for _, ed := range evs {
		err = cronEventStart(ctx, ed)
		if err != nil {
			return err
		}

		err = dbUpdateAll(ctx, ed)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *FnEventDesc)toAPI(fn *FunctionDesc) *swyapi.FunctionEvent {
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

func eventsAdd(ctx context.Context, fn *FunctionDesc, evt *swyapi.FunctionEvent) (string, *swyapi.GateErr) {
	ed := &FnEventDesc{
		ObjID: bson.NewObjectId(),
		Name: evt.Name,
		FnId: fn.Cookie,
		Source: evt.Source,
	}

	var err error

	switch evt.Source {
	case "cron":
		ed.Cron = &FnEventCron{
			Tab: evt.Cron.Tab,
			Args: evt.Cron.Args,
		}
	case "s3":
		ed.S3 = &FnEventS3{
			Bucket: evt.S3.Bucket,
			Ops: evt.S3.Ops,
			Pattern: evt.S3.Pattern,
		}
	case "url":
		/* Nothing (yet) */ ;
	default:
		return "", GateErrM(swy.GateBadRequest, "Unsupported event type")
	}

	err = dbInsert(ctx, ed)
	if err != nil {
		return "", GateErrD(err)
	}

	switch evt.Source {
	case "cron":
		err = cronEventStart(ctx, ed)
	case "s3":
		err = s3EventStart(ctx, fn, ed)
	case "url":
		err = urlEventStart(ctx, ed)
	}
	if err != nil {
		dbRemove(ctx, ed)
		return "", GateErrM(swy.GateGenErr, "Can't setup event")
	}

	err = dbUpdateAll(ctx, ed)
	if err != nil {
		eventStop(ctx, ed)
		dbRemove(ctx, ed)
		return "", GateErrD(err)
	}

	return ed.ObjID.Hex(), nil
}

func eventStop(ctx context.Context, ed *FnEventDesc) error {
	var err error

	switch ed.Source {
	case "cron":
		err = cronEventStop(ctx, ed)
	case "s3":
		err = s3EventStop(ctx, ed)
	case "url":
		err = urlEventStop(ctx, ed)
	}

	return err
}

func eventsDelete(ctx context.Context, fn *FunctionDesc, ed *FnEventDesc) *swyapi.GateErr {
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
