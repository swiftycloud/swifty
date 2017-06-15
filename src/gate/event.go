package main

import (
	"fmt"
	"gopkg.in/robfig/cron.v2"
)

var cronRunner *cron.Cron

var evtHandlers = map[string]func(*YAMLConf, *FunctionDesc, bool) error {
	"mware": mwareEventSetup,
	"cron": cronEventSetup,
	"oneshot": oneshotEventSetup,
	"url": urlSetup,
}

func eventSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	evtHandler, ok := evtHandlers[fn.Event.Source]
	if ok {
		return evtHandler(conf, fn, on)
	} else {
		return fmt.Errorf("Unknown event type %s", fn.Event.Source)
	}
}

type faasCronJob struct {
	project, fname string
}

func (cj faasCronJob) Run () {
	log.Debugf("Will run %s.%s function", cj.project, cj.fname)
}

func oneshotEventSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	fn.OneShot = true
	return nil
}

func cronEventSetup(conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if on {
		id, err := cronRunner.AddJob(fn.Event.CronTab, faasCronJob{fn.Project, fn.FuncName})
		if err != nil {
			log.Errorf("Can't setup cron trigger for %s.%s", fn.Project, fn.FuncName)
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
		log.Errorf("Can't list functions with events: %s", err.Error())
		return err
	}

	for _, fn := range fns {
		log.Debugf("Restart event for %s:%s", fn.Project, fn.FuncName)
		err = eventSetup(conf, &fn, true)
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
