package main

import (
	"fmt"
	"context"
	"gopkg.in/robfig/cron.v2"
)

var cronRunner *cron.Cron

type EventOps struct {
	Setup func(ctx context.Context, conf *YAMLConf, id *SwoId, evt *FnEventDesc, on bool) error
	Devel bool
}

var evtHandlers = map[string]*EventOps {
	"url":		&EventURL,
	"cron":		&EventCron,
	"mware":	&EventMware,
	"oneshot":	&EventOneShot,
}

func (evt *FnEventDesc)setup(ctx context.Context, conf *YAMLConf, id *SwoId, on bool) error {
	var err error

	if evt.Source != "" {
		eh, ok := evtHandlers[evt.Source]
		if ok && (SwyModeDevel || !eh.Devel) {
			if eh.Setup != nil {
				err = eh.Setup(ctx, conf, id, evt, on)
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

func cronEventSetup(ctx context.Context, conf *YAMLConf, fnid *SwoId, evt *FnEventDesc, on bool) error {
	if on {
		id, err := cronRunner.AddFunc(evt.CronTab, func() {
				glog.Debugf("Will run %s function, %s", fnid.Str())
			})
		if err != nil {
			ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fnid.Str())
			return err
		}

		evt.CronID = int(id)
	} else {
		cronRunner.Remove(cron.EntryID(evt.CronID))
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
		err = fn.Event.Prepare(context.Background(), conf, fn.SwoId)
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
