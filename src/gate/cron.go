package main

import (
	"context"
	"errors"
	"gopkg.in/robfig/cron.v2"
	"gopkg.in/mgo.v2/bson"
	"swifty/apis"
)

type FnEventCron struct {
	Tab		string			`bson:"tab"`
	Args		map[string]string	`bson:"args"`
	JobID		int			`bson:"eid"`
}

var cronRunner *cron.Cron

func cronEventStart(ctx context.Context, _ *FunctionDesc, evt *FnEventDesc) error {
	id, err := cronRunner.AddFunc(evt.Cron.Tab, func() {
		cctx, done := mkContext("::cron")
		defer done(cctx)

		var fn FunctionDesc

		err := dbFind(cctx, bson.M{"cookie": evt.FnId}, &fn)
		if err != nil {
			danglingEvents.WithLabelValues("cron").Inc()
			ctxlog(cctx).Errorf("Can't find FN %s to run Cron event", evt.FnId)
			return
		}

		if fn.State != DBFuncStateRdy {
			danglingEvents.WithLabelValues("cron").Inc()
			return
		}

		doRunBg(cctx, &fn, "cron", &swyapi.FunctionRun{Args: evt.Cron.Args})
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

var cronOps = EventOps {
	setup: func(ed *FnEventDesc, evt *swyapi.FunctionEvent) error {
		if evt.Cron == nil {
			return errors.New("No \"cron\" field")
		}

		ed.Cron = &FnEventCron{
			Tab: evt.Cron.Tab,
			Args: evt.Cron.Args,
		}

		return nil
	},
	start:	cronEventStart,
	stop:	cronEventStop,
}

func cronInit(ctx context.Context) error {
	cronRunner = cron.New()
	cronRunner.Start()

	var evs []*FnEventDesc

	err := dbFindAll(ctx, bson.M{"source":"cron"}, &evs)
	if err != nil {
		return err
	}

	for _, ed := range evs {
		err = cronEventStart(ctx, nil, ed)
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

