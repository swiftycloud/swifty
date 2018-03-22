package xwait

import (
	"time"
	"container/list"
)

type Waiter struct {
	key string
	s chan bool
	d chan bool
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
					w.s <-true
				}
			}

		case r := <-adds:
			w := &Waiter{key: r.key, s: make(chan bool), d: make(chan bool)}
			w.l = ws.PushBack(w)
			r.r <-w

		case r := <-dels:
			ws.Remove(r.l)
			close(r.d)
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
	start := time.Now()
	for {
		select {
		case <-time.After(*tmo):
			return true
		case <-w.s:
			elapsed := time.Since(start)
			if elapsed >= *tmo {
				*tmo = 0
			} else {
				*tmo -= elapsed
			}

			return false
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
