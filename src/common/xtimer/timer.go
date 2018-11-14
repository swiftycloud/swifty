/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package xtimer

import (
	"time"
	"sync"
)

type timer struct {
	cancel chan bool
	canceled bool
}

/*
 * sync.Map is told to work OK for stable keys. Our
 * keys are not such, so just Mutex and map
 */
var lock sync.Mutex
var timers map[string]*timer

func init() {
	timers = make(map[string]*timer)
}

func Create(name string) {
	t := &timer { cancel: make(chan bool), canceled: false }

	lock.Lock()
	timers[name] = t
	lock.Unlock()
}

func Start(name, arg string, tmo time.Duration, cb func(string, string)) {
	lock.Lock()
	t, ok := timers[name]
	/*
	 * It might have happened that timer is already canceled
	 * Oh, well...
	 */
	if ok {
		go func() {
			select {
			case <-time.After(tmo):
				lock.Lock()
				if t.canceled {
					lock.Unlock()
				} else {
					delete(timers, name)
					lock.Unlock()
					cb(name, arg)
				}
			case <-t.cancel:
				/* Just exit */ ;
			}
		}()
	}
	lock.Unlock()
}

/* Returns true is the cb was NOT fired yet */
func Cancel(name string) bool {
	lock.Lock()
	t, ok := timers[name]
	if ok {
		delete(timers, name)
		t.canceled = true
		t.cancel <- true
	}
	lock.Unlock()

	return ok
}
