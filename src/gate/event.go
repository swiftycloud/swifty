package main

import (
	"fmt"
	"strings"
	"context"
	"gopkg.in/robfig/cron.v2"
	"gopkg.in/mgo.v2/bson"
	"sync"
	"../common"
	"../apis/apps"
)

const FnEventURLId = "0" // Special ID for URL-triggered event

type FnEventCron struct {
	Tab		string			`bson:"tab"`
	Args		map[string]string	`bson:"args"`
}

type FnEventS3 struct {
	Ns		string		`bson:"ns"`
	Bucket		string		`bson:"bucket"`
	Ops		string		`bson:"ops"`
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

type FnEventDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	Name		string		`bson:"name"`
	Source		string		`bson:"source"`
	Cron		*FnEventCron	`bson:"cron,omitempty"`
	S3		*FnEventS3	`bson:"s3,omitempty"`
	start		func()
}

var runners map[string]*cron.Cron
var lock sync.Mutex

type EventOps struct {
	Setup func(ctx context.Context, conf *YAMLConf, id *SwoId, evt *FnEventDesc, on bool, started bool) error
	Devel bool
}

var evtHandlers = map[string]*EventOps {
	"url":		&EventURL,
	"cron":		&EventCron,
	"s3":		&EventS3,
	"mware":	&EventMware,
	"oneshot":	&EventOneShot,
}

func (evt *FnEventDesc)Start() {
	if evt.start != nil {
		evt.start()
	}
}

/* id in Prepare/Cancel/Stop MUST be by-value, as .setup modifies one */
func (evt *FnEventDesc)Prepare(ctx context.Context, conf *YAMLConf, id SwoId) error {
	return evt.setup(ctx, conf, &id, true, false)
}

func (evt *FnEventDesc)Cancel(ctx context.Context, conf *YAMLConf, id SwoId, started bool) error {
	return evt.setup(ctx, conf, &id, false, started)
}

func (evt *FnEventDesc)setup(ctx context.Context, conf *YAMLConf, id *SwoId, on bool, started bool) error {
	var err error

	if evt.Source != "" {
		eh, ok := evtHandlers[evt.Source]
		if ok && (SwyModeDevel || !eh.Devel) {
			if eh.Setup != nil {
				err = eh.Setup(ctx, conf, id, evt, on, started)
			}
		} else {
			err = fmt.Errorf("Unknown event type %s", evt.Source)
		}
	}

	return err
}

var EventOneShot = EventOps {
	Devel: true,
}

func cronEventSetupOne(c *cron.Cron, ce *FnEventCron, fnid *SwoId) error {
	_, err := c.AddFunc(ce.Tab, func() {
		fn, err := dbFuncFind(fnid)
		if err != nil {
			glog.Errorf("Can't find FN %s to run Cron event", fnid.Str())
			return
		}

		if fn.State != swy.DBFuncStateRdy {
			return
		}

		doRun(context.Background(), fn, "cron", ce.Args)
	})

	return err
}

func cronEventSetup(ctx context.Context, conf *YAMLConf, fnid *SwoId, evt *FnEventDesc, on bool, started bool) error {
	if on {
		c := cron.New()
		err := cronEventSetupOne(c, evt.Cron, fnid)
		if err != nil {
			ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fnid.Str())
			return err
		}

		id := fnid.Cookie()
		evt.start = func() {
			/* There can be another cron runner sitting under this
			 * id, so we defer inserting ourselves into the map
			 * till the previous one is removed (if at all)
			 */
			lock.Lock()
			runners[id] = c
			lock.Unlock()
			c.Start()
		}
	} else {
		if started {
			/* If this evt was not started, then we should not
			 * remove it from the runners map (chances are that
			 * there's an old chap sitting there).
			 */
			id := fnid.Cookie()

			lock.Lock()
			c := runners[id]
			delete(runners, id)
			lock.Unlock()

			if c != nil { c.Stop() }
		}
	}

	return nil
}

var EventCron = EventOps {
	Setup: cronEventSetup,
}

func eventsInit(conf *YAMLConf) error {
	runners = make(map[string]*cron.Cron)
	return nil
}

func (e *FnEventDesc)toAPI(withid bool) *swyapi.FunctionEvent {
	ae := swyapi.FunctionEvent{
		Name: e.Name,
		Source: e.Source,
	}

	if withid {
		ae.Id = e.ObjID.Hex()
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
		}
	}

	return &ae
}

func eventsList(fnid string) ([]swyapi.FunctionEvent, *swyapi.GateErr) {
	var ret []swyapi.FunctionEvent
	evs, err := dbListFnEvents(fnid)
	if err != nil {
		return ret, GateErrD(err)
	}

	for _, e := range evs {
		ret = append(ret, *e.toAPI(true))
	}
	return ret, nil
}

func eventsAdd(fnid string, evt *swyapi.FunctionEvent) (string, *swyapi.GateErr) {
	ed := &FnEventDesc{
		ObjID: bson.NewObjectId(),
		Name: evt.Name,
		FnId: fnid,
		Source: evt.Source,
	}

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
		}
	case "url":
		;
	default:
		return "", GateErrM(swy.GateBadRequest, "Unsupported event type")
	}

	err := dbAddEvent(ed)
	if err != nil {
		return "", GateErrD(err)
	}

	return ed.ObjID.Hex(), nil
}

func eventsGet(fnid, eid string) (*swyapi.FunctionEvent, *swyapi.GateErr) {
	ed, err := dbFindEvent(eid)
	if err != nil {
		return nil, GateErrD(err)
	}

	if ed.FnId != fnid {
		return nil, GateErrC(swy.GateNotFound)
	}

	return ed.toAPI(false), nil
}

func eventsDelete(fnid, eid string) *swyapi.GateErr {
	ed, err := dbFindEvent(eid)
	if err != nil {
		return GateErrD(err)
	}

	if ed.FnId != fnid {
		return GateErrC(swy.GateNotFound)
	}

	err = dbRemoveEvent(eid)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}
