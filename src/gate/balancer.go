package main

import (
	"sync"
	"sync/atomic"
	"errors"
	"context"
	"fmt"

	"../common"
	"../apis/apps"
)

type BalancerDat struct {
	rover		[2]uint32
	pods		[]podConn
	goal		uint32
	wakeup		*sync.Cond
}

func (bd *BalancerDat)Flush() {
	bd.pods = []podConn{}
}

func BalancerPodDel(pod *k8sPod) error {
	fnid := pod.SwoId.Cookie()
	balancerPodsFlush(fnid)

	err := dbBalancerPodDel(pod)
	if err != nil {
		return fmt.Errorf("Pod del error: %s", err.Error())
	}

	fnWaiterKick(fnid)
	return nil
}

func BalancerPodAdd(pod *k8sPod) error {
	fnid := pod.SwoId.Cookie()

	balancerPodsFlush(fnid)

	err := dbBalancerPodAdd(fnid, pod)
	if err != nil {
		return fmt.Errorf("Add error: %s", err.Error())
	}

	fnWaiterKick(fnid)
	return nil
}

func BalancerDelete(ctx context.Context, fnid string) (error) {
	fdm := memdGetCond(fnid)
	if fdm != nil {
		fdm.bd.Flush()
		fdm.lock.Lock()
		if fdm.bd.wakeup != nil {
			fdm.bd.goal = 0
			fdm.bd.wakeup.Signal()
		}
		fdm.lock.Unlock()
	}

	err := dbBalancerPodDelAll(fnid)
	if err != nil {
		return fmt.Errorf("POD del all error: %s", err.Error())
	}

	ctxlog(ctx).Debugf("Removed balancer for %s", fnid)
	return nil
}

func BalancerCreate(ctx context.Context, fnid string) (error) {
	return nil
}

func BalancerInit(conf *YAMLConf) (error) {
	return nil
}

func balancerPodsFlush(fnid string) {
	fdm := memdGetCond(fnid)
	if fdm != nil {
		fdm.bd.Flush()
	}
}

func balancerGetConnExact(ctx context.Context, cookie, version string) (*podConn, *swyapi.GateErr) {
	/*
	 * We can lookup id.Cookie() here, but ... it's manual run,
	 * let's also make sure the FN exists at all
	 */
	ap, err := dbBalancerGetConnExact(cookie, version)
	if ap == nil {
		if err == nil {
			return nil, GateErrM(swy.GateGenErr, "Nothing to run (yet)")
		}

		ctxlog(ctx).Errorf("balancer-db: Can't find pod %s/%s: %s",
				cookie, version, err.Error())
		return nil, GateErrD(err)
	}

	return ap, nil
}

func balancerGetConnAny(ctx context.Context, cookie string, fdm *FnMemData) (*podConn, error) {
	var aps []podConn
	var err error

	if fdm != nil {
		aps = fdm.bd.pods
	}

	if len(aps) == 0 {
		aps, err = dbBalancerGetConnsByCookie(cookie)
		if aps == nil {
			if err == nil {
				return nil, errors.New("No available PODs")
			}

			return nil, err
		}

		fdm.lock.Lock()
		if len(fdm.bd.pods) == 0 {
			fdm.bd.pods = aps
		} else {
			aps = fdm.bd.pods
		}
		fdm.lock.Unlock()
	}

	/* Emulate simple RR balancing -- each next call picks next POD */
	sc := atomic.AddUint32(&fdm.bd.rover[0], 1)
	balancerFnDepGrow(ctx, fdm, sc - fdm.bd.rover[1])

	return &aps[sc % uint32(len(aps))], nil
}
