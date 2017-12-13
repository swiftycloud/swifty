package main

import (
	"github.com/streadway/amqp"
	"gopkg.in/mgo.v2/bson"
)

type mqConsumer struct {
	counter		int
	channel		*amqp.Channel
}

type mqListenerCb func(*FunctionDesc, []byte)

// FIXME -- isn't there out-of-the-box factory engine in go?
type mq_listener_req struct {
	user	string
	pass	string
	addr	string
	vhost	string
	queue	string
	cb	mqListenerCb
	add	bool
	resp	chan error
}

func (req *mq_listener_req)hkey() string {
	return req.addr + "/" + req.vhost + ":" + req.queue
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
		cons.channel.Cancel(req.queue, false)
		delete(consumers, key)
	}
}

func startListener(req *mq_listener_req) error {
	key := req.hkey()
	cons := consumers[key]
	if cons != nil {
		cons.counter++
		return nil
	}

	cons = &mqConsumer{counter: 1}

	log.Debugf("mq: Starting mq listener @%s", key)

	/* FIXME -- can there be one connection? */
	conn, err := amqp.Dial("amqp://" + req.user + ":" + req.pass + "@" + req.addr +"/" + req.vhost)
	if err != nil {
		return err
	}

	log.Debugf("mq:\tchan")
	cons.channel, err = conn.Channel()
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
		for d := range msgs {
			log.Debugf("mq: Received message [%s] from [%s]", d.Body, d.UserId)
			if d.UserId == "" {
				continue
			}

			mware, err := dbMwareResolveClient(d.UserId)
			if err != nil {
				continue
			}

			log.Debugf("mq: Resolved client to project %s", mware.Project)

			funcs, err := dbFuncListMwEvent(&mware.SwoId, bson.M {
						"event.source": "mware",
						"event.mwid": mware.SwoId.Name,
						"event.mqueue": req.queue,
					})
			if err != nil {
				/* FIXME -- this should be notified? Or what? */
				log.Errorf("mq: Can't list functions for event")
				continue
			}

			for _, fn := range funcs {
				log.Debugf("mq: `- [%s]", fn)
				req.cb(&fn, d.Body)
			}
		}
		log.Debugf("mq: Bail out")
	}()

	consumers[key] = cons
	log.Debugf("mq: ... Done");
	return nil
}

func mqStartListener(conf *YAMLConfMw, vhost, queue string, cb mqListenerCb) error {
	return factoryMakeReq(&mq_listener_req{
			user: conf.Rabbit.Admin,
			pass: gateSecrets[conf.Rabbit.Pass],
			addr: conf.Rabbit.Addr,
			vhost: vhost,
			queue: queue,
			cb: cb,
			add: true,
		})
}

func mqStopListener(conf *YAMLConfMw, vhost, queue string) {
	factoryMakeReq(&mq_listener_req{
			addr: conf.Rabbit.Addr,
			vhost: vhost,
			queue: queue,
			add: false,
		})
}
