package main

import (
	"fmt"
	"k8s.io/client-go/pkg/api/v1"
	"errors"
	"sync"
	"time"
)

func condWaitTmo(cond *sync.Cond, tmo time.Duration) {
	d := time.AfterFunc(tmo, func() { cond.Signal() })
	cond.Wait()
	d.Stop()
}

func balancerFnScaler(fdm *FnMemData) {
up:
	glog.Debugf("Scale %s up to %d", fdm.depname, fdm.bd.goal)
	goal := swk8sDepScaleUp(fdm.depname, fdm.bd.goal)

	fdm.lock.Lock()
	if fdm.bd.goal == 0 {
		goto fin
	}

	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}
relax:
	glog.Debugf("Wait1 %s, %d/%d", fdm.depname, goal, fdm.bd.goal)
	condWaitTmo(fdm.bd.wakeup, SwyDepScaleupRelax)
	glog.Debugf("`--> %s, %d/%d", fdm.depname, goal, fdm.bd.goal)

down:
	if fdm.bd.goal <= 1 {
		fdm.bd.wakeup = nil
		goto fin
	}
	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}

	fdm.bd.goal--
	glog.Debugf("Wait2 %s, %d/%d", fdm.depname, goal, fdm.bd.goal)
	condWaitTmo(fdm.bd.wakeup, SwyDepScaledownStep)
	glog.Debugf("`--> %s, %d/%d", fdm.depname, goal, fdm.bd.goal)
	if fdm.bd.goal == 0 {
		goto fin
	}
	if fdm.bd.goal == goal {
		goto relax
	}
	if fdm.bd.goal > goal {
		fdm.lock.Unlock()
		goto up
	}

	fdm.lock.Unlock()
	glog.Debugf("Scale %s down to %d", fdm.depname, fdm.bd.goal)
	goal = swk8sDepScaleDown(fdm.depname, fdm.bd.goal)
	fdm.lock.Lock()

	goto down

fin:
	fdm.lock.Unlock()
	glog.Debugf("Scaler %s done", fdm.depname)
}

func balancerFnDepGrow(fdm *FnMemData, goal uint32) {
	if goal <= fdm.bd.goal {
		return
	}

	fdm.lock.Lock()
	if goal <= fdm.bd.goal {
		fdm.lock.Unlock()
		return
	}

	fdm.bd.goal = goal

	if fdm.bd.wakeup == nil {
		fdm.bd.wakeup = sync.NewCond(&fdm.lock)
		go balancerFnScaler(fdm)
	} else {
		fdm.bd.wakeup.Signal()
	}
	fdm.lock.Unlock()
}

func listSwyDeps() error {
	depiface := swk8sClientSet.Extensions().Deployments(v1.NamespaceDefault)
	deps, err := depiface.List(v1.ListOptions{ LabelSelector: "swyrun" })
	if err != nil {
		glog.Errorf("Error listing DEPs: %s", err.Error())
		return errors.New("Error listing PODs")
	}

	/* FIXME -- tune up the BalancerRS DB for this deployment */

	for _, dep := range deps.Items {
		if *dep.Spec.Replicas <= 1 {
			continue
		}

		id := makeSwoId(
			dep.ObjectMeta.Labels["tenant"],
			dep.ObjectMeta.Labels["project"],
			dep.ObjectMeta.Labels["function"])
		glog.Debugf("Found %s grown-up (%d) deployment for %s", dep.Name, *dep.Spec.Replicas, id.Str())

		fdm := memdGet(id.Cookie())
		if fdm == nil {
			return fmt.Errorf("Can't get fdmd for %s", id.Str())
		}

		fdm.bd.goal = uint32(*dep.Spec.Replicas)
		fdm.bd.wakeup = sync.NewCond(&fdm.lock)
		go balancerFnScaler(fdm)
	}

	return nil
}

func scalerInit() error {
	err := listSwyDeps()
	if err != nil {
		return err
	}

	return nil
}
