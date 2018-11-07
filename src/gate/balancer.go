package main

import (
	"sync"
	"sync/atomic"
	"errors"
	"context"
	"fmt"

	"swifty/common/xrest"
	"swifty/apis"
)

type BalancerDat struct {
	rover		[2]uint32
	pods		[]*podConn
	goal		uint32
	wakeup		*sync.Cond
}

func (bd *BalancerDat)Flush() {
	bd.pods = []*podConn{}
}

func BalancerPodDel(ctx context.Context, pod *k8sPod) {
	fnid := pod.SwoId.Cookie()
	balancerPodsFlush(fnid)
	podsDel(ctx, fnid, pod)
	fnWaiterKick(fnid)
}

func BalancerPodAdd(ctx context.Context, pod *k8sPod) error {
	fnid := pod.SwoId.Cookie()

	err := podsAdd(ctx, fnid, pod)
	if err != nil {
		return fmt.Errorf("Add error: %s", err.Error())
	}

	balancerPodsFlush(fnid)
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

	podsDelAll(ctx, fnid)
	return nil
}

func BalancerCreate(ctx context.Context, fnid string) (error) {
	return nil
}

func BalancerInit() (error) {
	return nil
}

func balancerPodsFlush(fnid string) {
	fdm := memdGetCond(fnid)
	if fdm != nil {
		fdm.bd.Flush()
	}
}

func balancerGetConnExact(ctx context.Context, cookie, version string) (*podConn, *xrest.ReqErr) {
	/*
	 * We can lookup id.Cookie() here, but ... it's manual run,
	 * let's also make sure the FN exists at all
	 */
	ap, err := podsFindExact(ctx, cookie, version)
	if ap == nil {
		if err == nil {
			return nil, GateErrM(swyapi.GateGenErr, "Nothing to run (yet)")
		}

		ctxlog(ctx).Errorf("balancer-db: Can't find pod %s/%s: %s",
				cookie, version, err.Error())
		return nil, GateErrD(err)
	}

	return ap, nil
}

func balancerGetConnAny(ctx context.Context, fdm *FnMemData) (*podConn, error) {
	var aps []*podConn
	var err error

	aps = fdm.bd.pods
	if len(aps) == 0 {
		aps, err = podsFindAll(ctx, fdm.fnid)
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

	return aps[sc % uint32(len(aps))], nil
}

func balancerPutConn(fdm *FnMemData) {
	atomic.AddUint32(&fdm.bd.rover[1], 1)
}
