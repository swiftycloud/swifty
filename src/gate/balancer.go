package main

import (
	"gopkg.in/mgo.v2/bson"
	"time"
	"sync/atomic"
	"errors"
	"net"
	"context"
	"fmt"

	"../common"
	"../apis/apps"
)

type BalancerRS struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	UID		string		`bson:"uid"`
	WdogAddr	string		`bson:"wdogaddr"`
	Version		string		`bson:"fnversion"`
}

func BalancerPodDel(pod *k8sPod) error {
	err := dbBalancerPodDel(pod)
	if err != nil {
		return fmt.Errorf("Pod del error: %s", err.Error())
	}

	return nil
}

func waitPort(addr_port string) error {
	wt := 100 * time.Millisecond
	var slept time.Duration
	for {
		conn, err := net.Dial("tcp", addr_port)
		if err == nil {
			conn.Close()
			break
		}

		if slept >= SwyPodStartTmo {
			return fmt.Errorf("Pod's port not up for too long")
		}

		/*
		 * Kuber sends us POD-Up event when POD is up, not when
		 * watchdog is ready :) But we need to make sure that the
		 * port is open and ready to serve connetions. Possible
		 * solution might be to make wdog ping us after openeing
		 * its socket, but ... will gate stand that ping flood?
		 *
		 * Moreover, this port waiter is only needed when the fn
		 * is being waited for.
		 */
		glog.Debugf("Port not open yet (%s) ... polling", err.Error())
		<-time.After(wt)
		slept += wt
		wt += 50 * time.Millisecond
	}

	return nil
}

func BalancerPodAdd(pod *k8sPod) error {
	fnid := pod.SwoId.Cookie()
	err := waitPort(pod.WdogAddr)
	if err != nil {
		return err
	}

	err = dbBalancerPodAdd(fnid, pod)
	if err != nil {
		return fmt.Errorf("Add error: %s", err.Error())
	}

	fnWaiterKick(fnid)
	return nil
}

func BalancerDelete(ctx context.Context, fnid string) (error) {
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

func balancerGetConnExact(ctx context.Context, cookie, version string) (string, *swyapi.GateErr) {
	/*
	 * We can lookup id.Cookie() here, but ... it's manual run,
	 * let's also make sure the FN exists at all
	 */
	ap, err := dbBalancerGetConnExact(cookie, version)
	if ap == "" {
		if err == nil {
			return "", GateErrM(swy.GateGenErr, "Nothing to run (yet)")
		}

		ctxlog(ctx).Errorf("balancer-db: Can't find pod %s/%s: %s",
				cookie, version, err.Error())
		return "", GateErrD(err)
	}

	return ap, nil
}

func balancerGetConnAny(ctx context.Context, cookie string, fdm *FnMemData) (string, error) {
	/* FIXME -- get conns right from fdm, don't go to mongo every call */
	aps, err := dbBalancerGetConnsByCookie(cookie)
	if aps == nil {
		if err == nil {
			return "", errors.New("No available PODs")
		}

		return "", err
	}

	if fdm == nil { /* FIXME -- balance here too */
		return aps[0], nil
	}

	/* Emulate simple RR balancing -- each next call picks next POD */
	cc := atomic.AddUint32(&fdm.rover, 1)
	return aps[cc % uint32(len(aps))], nil
}
