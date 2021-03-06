/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"context"
	"sync"
	"swifty/common/xrest/sysctl"
)

type fnPods struct {
	lock	sync.RWMutex
	pods	map[string]*k8sPod
	dead	bool
}

var fnPodsStore sync.Map

var show string

func showPodsStore() string {
	if show == "" {
		return "set 'list' or $fnid here"
	}

	if show == "list" {
		x := ""
		fnPodsStore.Range(func(k, v interface{}) bool {
			x += " " + k.(string)
			return true
		})

		return x[1:]
	}

	fp := findFnPods(show)
	if fp == nil {
		return "---"
	}

	x := ""
	fp.lock.RLock()
	defer fp.lock.RUnlock()

	for _, p := range fp.pods {
		x += " " + p.WdogAddr + "/" + p.Version
	}

	return x[1:]
}

func init() {
	sysctl.AddSysctl("pods_store", showPodsStore, func(nv string) error { show = nv; return nil })
}

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

func podsDel(ctx context.Context, pod *k8sPod) {
	fnp := findFnPods(pod.FnId)
	if fnp != nil {
		fnp.lock.Lock()
		delete(fnp.pods, pod.UID)
		if len(fnp.pods) == 0 {
			/* All PODs are gone, we may kill the whole thing */
			fnp.dead = true
			fnPodsStore.Delete(pod.FnId)
		}
		fnp.lock.Unlock()
	}
}

func podsAdd(ctx context.Context, pod *k8sPod) {
again:
	x, ok := fnPodsStore.Load(pod.FnId)
	if !ok {
		x, _ = fnPodsStore.LoadOrStore(pod.FnId, makeFnPods())
	}

	fnp := x.(*fnPods)

	fnp.lock.Lock()
	if fnp.dead {
		fnp.lock.Unlock()
		goto again
	}
	fnp.pods[pod.UID] = pod
	fnp.lock.Unlock()
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
