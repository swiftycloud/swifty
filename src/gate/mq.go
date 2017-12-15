package main

import (
	"github.com/streadway/amqp"
)

type mqConsumer struct {
	counter		int
	conn		*amqp.Connection
	channel		*amqp.Channel
	done		chan bool
}

type mqListenerCb func(string, []byte)

// FIXME -- isn't there out-of-the-box factory engine in go?
type mq_listener_req struct {
	user	string
	pass	string
	url	string
	queue	string
	cb	mqListenerCb
	add	bool
	resp	chan error
}

func (req *mq_listener_req)hkey() string {
	return req.url + ":" + req.queue
}

var consumers map[string]*mqConsumer
var factory_ch chan *mq_listener_req

func factoryMakeReq(req *mq_listener_req) error {
	req.resp = make(chan error)
	factory_ch <- req
	return <-req.resp
}

func init() {
	consumers = make(map[string]*mqConsumer)
	factory_ch = make(chan *mq_listener_req)
	go func() {
		for req := range factory_ch {
			var err error
			if req.add {
				err = startListener(req)
			} else {
				stopListener(req)
			}
			req.resp <- err
		}
	}()
}

func stopListener(req *mq_listener_req) {
	key := req.hkey()
	cons, ok := consumers[key]
	if !ok {
		log.Errorf("mq: FATAL: no consumer for %s found", key)
		return
	}

	cons.counter--
	if cons.counter == 0 {
		log.Debugf("mq: Stopping mq listener @%s", key)
		cons.done <-true
		cons.channel.Close()
		cons.conn.Close()
		delete(consumers, key)
	}
}

func startListener(req *mq_listener_req) error {
	var err error

	key := req.hkey()
	cons := consumers[key]
	if cons != nil {
		cons.counter++
		return nil
	}

	cons = &mqConsumer{counter: 1}

	cons.done = make(chan bool)

	log.Debugf("mq: Starting mq listener @%s", key)

	/* FIXME -- can there be one connection? */
	cons.conn, err = amqp.Dial("amqp://" + req.user + ":" + req.pass + "@" + req.url)
	if err != nil {
		return err
	}

	log.Debugf("mq:\tchan")
	cons.channel, err = cons.conn.Channel()
	if err != nil {
		return err
	}

	log.Debugf("mq:\tqueue")
	q, err := cons.channel.QueueDeclare(req.queue, false, false, false, false, nil)
	if err != nil {
		return err
	}

	log.Debugf("mq:\tconsume")
	msgs, err := cons.channel.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}

	go func() {
		log.Debugf("mq: Getting messages for %s", key)
	loop:
		for {
			log.Debugf("mq: >")
			select {
			case d := <-msgs:
				log.Debugf("mq: Received message [%s] from [%s]", d.Body, d.UserId)
				req.cb(d.UserId, d.Body)
			case <-cons.done:
				log.Debugf("mq: Done")
				break loop
			}
			log.Debugf("mq: <")
		}
		log.Debugf("mq: Stop getting messages")
	}()

	consumers[key] = cons
	log.Debugf("mq: ... Done");
	return nil
}

func mqStartListener(user, pass, url, queue string, cb mqListenerCb) error {
	return factoryMakeReq(&mq_listener_req{
			user: user,
			pass: gateSecrets[pass],
			url: url,
			queue: queue,
			cb: cb,
			add: true,
		})
}

func mqStopListener(url, queue string) {
	factoryMakeReq(&mq_listener_req{
			url: url,
			queue: queue,
		})
}
