package xwait

import (
	"time"
	"sync"
	"container/list"
)

type Waiter struct {
	key string
	evt bool
	lock sync.Mutex
	wake *sync.Cond
	l *list.Element
}

type wreq struct {
	key string
	r chan *Waiter
}

var adds chan *wreq
var dels chan *Waiter
var evnts chan string

func serve() {
	ws := list.New()

	for {
		select {
		case key := <-evnts:
			for el := ws.Front(); el != nil; el = el.Next() {
				w := el.Value.(*Waiter)
				if w.key == key {
					w.lock.Lock()
					if !w.evt {
						w.evt = true
						w.wake.Signal()
					}
					w.lock.Unlock()
				}
			}

		case r := <-adds:
			w := &Waiter{key: r.key}
			w.wake = sync.NewCond(&w.lock)
			w.l = ws.PushBack(w)
			r.r <-w

		case r := <-dels:
			ws.Remove(r.l)
		}
	}
}

func init() {
	adds = make(chan *wreq)
	dels = make(chan *Waiter)
	evnts = make(chan string)

	go serve()
}

func Prepare(key string) *Waiter {
	r := make(chan *Waiter)
	adds <-&wreq{key: key, r: r}
	return <-r
}

func (w *Waiter)Wait(tmo *time.Duration) bool {
	w.lock.Lock()
	defer w.lock.Unlock()

	if w.evt {
		w.evt = false
		return false
	}

	start := time.Now()
	t := time.AfterFunc(*tmo, func() { w.wake.Signal() })
	w.wake.Wait()
	t.Stop()

	if !w.evt {
		return true
	}

	w.evt = false
	elapsed := time.Since(start)
	if elapsed >= *tmo {
		*tmo = 0
	} else {
		*tmo -= elapsed
	}

	return false
}

func (w *Waiter)Done() {
	dels <-w
}

func Event(key string) {
	evnts <-key
}
