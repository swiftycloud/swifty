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

func podsDel(ctx context.Context, fnid string, pod *k8sPod) error {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil
	}

	fnp.lock.Lock()
	delete(fnp.pods, pod.UID)
	fnp.lock.Unlock()

	return nil
}

func podsAdd(ctx context.Context, fnid string, pod *k8sPod) error {
	x, ok := fnPodsStore.Load(fnid)
	if !ok {
		x, _ = fnPodsStore.LoadOrStore(fnid, makeFnPods())
	}

	fnp := x.(*fnPods)

	fnp.lock.Lock()
	fnp.pods[pod.UID] = pod
	fnp.lock.Unlock()

	return nil
}

func podsDelAll(ctx context.Context, fnid string) error {
	fnPodsStore.Delete(fnid)
	return nil
}

func podsFindExact(ctx context.Context, fnid, version string) (*podConn, error) {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil, nil
	}

	fnp.lock.RLock()
	defer fnp.lock.RUnlock()

	for _, pod := range fnp.pods {
		if pod.Version == version {
			return pod.conn(), nil
		}
	}

	return nil, nil
}

func podsFindAll(ctx context.Context, fnid string) ([]*podConn, error) {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil, nil
	}

	fnp.lock.RLock()
	defer fnp.lock.RUnlock()

	ret := []*podConn{}
	for _, pod := range fnp.pods {
		ret = append(ret, pod.conn())
	}

	return ret, nil
}

func podsListVersions(ctx context.Context, fnid string) ([]string, error) {
	fnp := findFnPods(fnid)
	if fnp == nil {
		return nil, nil
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

	return ret, nil
}
