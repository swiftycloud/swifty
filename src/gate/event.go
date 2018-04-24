package main

import (
	"fmt"
	"context"
	"gopkg.in/robfig/cron.v2"
	"sync"
	"../common"
)

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

func cronEventSetupOne(c *cron.Cron, ce *FnCronDesc, fnid *SwoId) error {
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
		for _, ce := range(evt.Cron) {
			err := cronEventSetupOne(c, ce, fnid)
			if err != nil {
				ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fnid.Str())
				return err
			}
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

			c.Stop()
		}
	}

	return nil
}

var EventCron = EventOps {
	Setup: cronEventSetup,
}

func eventsInit(conf *YAMLConf) error {
	runners = make(map[string]*cron.Cron)

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
