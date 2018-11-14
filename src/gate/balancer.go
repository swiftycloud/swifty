/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"sync"
	"sync/atomic"
	"errors"
	"context"

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

func balancerPodsFlush(fnid string) {
	fdm := memdGetCond(fnid)
	if fdm != nil {
		fdm.bd.Flush()
	}
}

func BalancerPodDel(ctx context.Context, pod *k8sPod) {
	podsDel(ctx, pod)
	balancerPodsFlush(pod.FnId)
}

func BalancerPodAdd(ctx context.Context, pod *k8sPod) {
	podsAdd(ctx, pod)
	balancerPodsFlush(pod.FnId)
}

func BalancerDelete(ctx context.Context, fnid string) (error) {
	fdm := memdGetCond(fnid)
	if fdm != nil {
		fdm.bd.Flush()
		scalerStop(ctx, fdm)
	}

	return nil
}

func BalancerCreate(ctx context.Context, fnid string) (error) {
	return nil
}

func BalancerInit() (error) {
	return nil
}

func balancerGetConnExact(ctx context.Context, cookie, version string) (*podConn, *xrest.ReqErr) {
	/*
	 * We can lookup id.Cookie() here, but ... it's manual run,
	 * let's also make sure the FN exists at all
	 */
	var xer *xrest.ReqErr

	ap := podsFindExact(ctx, cookie, version)
	if ap == nil {
		xer = GateErrM(swyapi.GateGenErr, "Nothing to run (yet)")
	}

	return ap, xer
}

func balancerGetConnAny(ctx context.Context, fdm *FnMemData) (*podConn, error) {
	var aps []*podConn

	aps = fdm.bd.pods
	if len(aps) == 0 {
		aps = podsFindAll(ctx, fdm.fnid)
		if aps == nil {
			return nil, errors.New("No available PODs")
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
	scalerSetGoal(ctx, fdm, sc - fdm.bd.rover[1])

	return aps[sc % uint32(len(aps))], nil
}

func balancerPutConn(fdm *FnMemData) {
	atomic.AddUint32(&fdm.bd.rover[1], 1)
}
