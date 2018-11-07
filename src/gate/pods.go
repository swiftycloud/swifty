package main

import (
	"context"
	"sync"
)

type fnPods struct {
	lock	sync.RWMutex
	pods	map[string]*k8sPod
}

var fnPodsStore sync.Map

func findFnPods(fnid string) *fnPods {
	x, ok := fnPodsStore.Load(fnid)
	if !ok {
		return nil
	}

	return x.(*fnPods)
}

func makeFnPods() *fnPods {
	x := &fnPods{}
	x.pods = make(map[string]*k8sPod)
	return x
}

func podsDel(ctx context.Context, fnid string, pod *k8sPod) {
	fnp := findFnPods(fnid)
	if fnp != nil {
		fnp.lock.Lock()
		delete(fnp.pods, pod.UID)
		fnp.lock.Unlock()
	}
}

func podsAdd(ctx context.Context, fnid string, pod *k8sPod) {
	x, ok := fnPodsStore.Load(fnid)
	if !ok {
		x, _ = fnPodsStore.LoadOrStore(fnid, makeFnPods())
	}

	fnp := x.(*fnPods)

	fnp.lock.Lock()
	fnp.pods[pod.UID] = pod
	fnp.lock.Unlock()
}

func podsDelAll(ctx context.Context, fnid string) {
	fnPodsStore.Delete(fnid)
}

func podsFindExact(ctx context.Context, fnid, version string) *podConn {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil
	}

	fnp.lock.RLock()
	defer fnp.lock.RUnlock()

	for _, pod := range fnp.pods {
		if pod.Version == version {
			return pod.conn()
		}
	}

	return nil
}

func podsFindAll(ctx context.Context, fnid string) []*podConn {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil
	}

	fnp.lock.RLock()
	defer fnp.lock.RUnlock()

	ret := []*podConn{}
	for _, pod := range fnp.pods {
		ret = append(ret, pod.conn())
	}

	return ret
}

func podsListVersions(ctx context.Context, fnid string) []string {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return []string{}
	}

	fnp.lock.RLock()
	defer fnp.lock.RUnlock()

	vers := make(map[string]bool)

	for _, pod := range fnp.pods {
		vers[pod.Version] = true
	}

	ret := []string{}
	for v, _ := range vers {
		ret = append(ret, v)
	}

	return ret
}
