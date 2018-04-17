package main

import (
	"fmt"
	"context"
	"gopkg.in/robfig/cron.v2"
)

var cronRunner *cron.Cron

type EventOps struct {
	Setup func(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error
	Devel bool
}

var evtHandlers = map[string]*EventOps {
	"url":		&EventURL,
	"cron":		&EventCron,
	"mware":	&EventMware,
	"oneshot":	&EventOneShot,
}

func eventPrepare(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	return eventSetup(ctx, conf, fn, true)
}

func eventCancel(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	return eventSetup(ctx, conf, fn, false)
}

func eventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	var err error

	if fn.Event.Source != "" {
		eh, ok := evtHandlers[fn.Event.Source]
		if ok && (SwyModeDevel || !eh.Devel) {
			if eh.Setup != nil {
				err = eh.Setup(ctx, conf, fn, on)
			}
		} else {
			err = fmt.Errorf("Unknown event type %s", fn.Event.Source)
		}
	}

	return err
}

var EventOneShot = EventOps {
	Devel: true,
}

func cronEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if on {
		var fnid SwoId

		fnid = fn.SwoId
		id, err := cronRunner.AddFunc(fn.Event.CronTab, func() {
				glog.Debugf("Will run %s function, %s", fnid.Str())
			})
		if err != nil {
			ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fn.SwoId.Str())
			return err
		}

		fn.Event.CronID = int(id)
	} else {
		cronRunner.Remove(cron.EntryID(fn.Event.CronID))
	}

	return nil
}

var EventCron = EventOps {
	Setup: cronEventSetup,
	Devel: true,
}

func eventsRestart(conf *YAMLConf) error {
	fns, err := dbFuncListWithEvents()
	if err != nil {
		glog.Errorf("Can't list functions with events: %s", err.Error())
		return err
	}

	for _, fn := range fns {
		glog.Debugf("Restart event for %s", fn.SwoId.Str())
		err = eventPrepare(context.Background(), conf, &fn)
		if err != nil {
			return err
		}
	}

	return nil
}

func eventsInit(conf *YAMLConf) error {
	cronRunner = cron.New()
	if cronRunner == nil {
		return fmt.Errorf("can't start cron runner")
	}

	cronRunner.Start()

	return eventsRestart(conf)
}
