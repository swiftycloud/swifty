package main

import (
	"fmt"
	"context"
	"gopkg.in/robfig/cron.v2"
)

var cronRunner *cron.Cron

var evtHandlers = map[string]func(context.Context, *YAMLConf, *FunctionDesc, bool) error {
	"mware": mwareEventSetup,
	"cron": cronEventSetup,
	"oneshot": oneshotEventSetup,
	"url": urlSetup,
}

func eventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if fn.Event.Source == "" {
		return nil
	}

	evtHandler, ok := evtHandlers[fn.Event.Source]
	if ok {
		return evtHandler(ctx, conf, fn, on)
	} else {
		return fmt.Errorf("Unknown event type %s", fn.Event.Source)
	}
}

type faasCronJob struct {
	Id SwoId
}

func (cj faasCronJob) Run () {
	glog.Debugf("Will run %s function", cj.Id.Str())
}

func oneshotEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	fn.OneShot = true
	return nil
}

func cronEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if on {
		id, err := cronRunner.AddJob(fn.Event.CronTab, faasCronJob{Id: fn.SwoId})
		if err != nil {
			ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fn.SwoId.Str())
			return err
		}

		fn.CronID = int(id)
	} else {
		cronRunner.Remove(cron.EntryID(fn.CronID))
	}

	return nil
}

func eventsRestart(conf *YAMLConf) error {
	fns, err := dbFuncListWithEvents()
	if err != nil {
		glog.Errorf("Can't list functions with events: %s", err.Error())
		return err
	}

	for _, fn := range fns {
		glog.Debugf("Restart event for %s", fn.SwoId.Str())
		err = eventSetup(context.Background(), conf, &fn, true)
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
