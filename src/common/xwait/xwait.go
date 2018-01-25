package xwait

import (
	"time"
	"container/list"
)

type Waiter struct {
	k string
	s chan string
	d chan bool
	l *list.Element
}

var adds chan chan *Waiter
var dels chan *Waiter
var evnts chan string

func serve() {
	ws := list.New()

	for {
		select {
		case key := <-evnts:
			for el := ws.Front(); el != nil; el = el.Next() {
				w := el.Value.(*Waiter)
				w.s <-key
			}

		case r := <-adds:
			w := &Waiter{s: make(chan string), d: make(chan bool)}
			w.l = ws.PushBack(w)
			r <-w

		case r := <-dels:
			ws.Remove(r.l)
			close(r.d)
		}
	}
}

func init() {
	adds = make(chan chan *Waiter)
	dels = make(chan *Waiter)
	evnts = make(chan string)

	go serve()
}

func Prepare(key string) *Waiter {
	r := make(chan *Waiter)
	adds <-r
	w := <-r
	w.k = key
	return w
}

func (w *Waiter)Wait(tmo time.Duration) bool {
	for {
		select {
		case <-time.After(tmo):
			return true
		case e := <-w.s:
			if e == w.k {
				return false
			}
		}
	}
}

func (w *Waiter)Done() {
	/*
	 * The serve() may be right now sitting in event
	 * fanout loop waiting for us to get one, and we'll
	 * no be able to do send ourselves to dels. To
	 * avoid this deadlock -- start an events drainer
	 * goroutine
	 */
	go func() {
		for {
			select {
			case <-w.s:
				;
			case <-w.d:
				return
			}
		}
	}()

	dels <-w
}

func Event(key string) {
	evnts <-key
}
