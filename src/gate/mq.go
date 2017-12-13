package main

import (
	"github.com/streadway/amqp"
	"gopkg.in/mgo.v2/bson"
)

type mqConsumer struct {
	counter		int
	channel		*amqp.Channel
}

// FIXME -- isn't there out-of-the-box factory engine in go?
type factory_req struct {
	vhost	string
	queue	string
	add	bool
	resp	chan error
}

var consumers map[string]*mqConsumer
var factory_ch chan factory_req

func factoryMakeReq(req *factory_req) error {
	req.resp = make(chan error)
	factory_ch <- *req
	return <-req.resp
}

func init() {
	consumers = make(map[string]*mqConsumer)
	factory_ch = make(chan factory_req)
	go func() {
		for req := range factory_ch {
			var err error
			if req.add {
				err = startListener(&conf.Mware, req.vhost, req.queue)
			} else {
				stopListener(req.vhost, req.queue)
			}
			req.resp <- err
		}
	}()
}

func stopListener(vhost, queue string) {
	cons := consumers[vhost + ":" + queue]
	if cons == nil {
		log.Errorf("mq: FATAL: no consumer for %s:%s found", vhost, queue)
		return
	}

	cons.counter--
	if cons.counter == 0 {
		log.Debugf("mq: Stopping mq listener @%s.%s", vhost, queue)
		cons.channel.Cancel(queue, false)
		delete(consumers, vhost + ":" + queue)
	}
}

func startListener(conf *YAMLConfMw, vhost, queue string) error {
	cons := consumers[vhost + ":" + queue]
	if cons != nil {
		cons.counter++
		return nil
	}

	cons = &mqConsumer{counter: 1}

	log.Debugf("mq: Starting mq listener @%s.%s", vhost, queue)

	login := conf.Rabbit.Admin
	pass := conf.Rabbit.Pass
	addr := conf.Rabbit.Addr

	/* FIXME -- there should be one connection */
	conn, err := amqp.Dial("amqp://" + login + ":" + pass + "@" + addr +"/" + vhost)
	if err != nil {
		return err
	}

	log.Debugf("mq:\tchan")
	cons.channel, err = conn.Channel()
	if err != nil {
		return err
	}

	log.Debugf("mq:\tqueue")
	q, err := cons.channel.QueueDeclare(queue, false, false, false, false, nil)
	if err != nil {
		return err
	}

	log.Debugf("mq:\tconsume")
	msgs, err := cons.channel.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}

	go func() {
		log.Debugf("mq: Getting messages for %s.%s", vhost, queue)
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
						"event.mqueue": queue,
					})
			if err != nil {
				/* FIXME -- this should be notified? Or what? */
				log.Errorf("mq: Can't list functions for event")
				continue
			}

			for _, fn := range funcs {
				log.Debugf("mq: `- [%s]", fn)

				/* FIXME -- this is synchronous */
				_, _, err := doRun(fn.Cookie, "mware:" + mware.Name + ":" + queue,
						map[string]string{"body": string(d.Body)})

				if err != nil {
					log.Errorf("mq: Error running FN %s", err.Error())
				} else {
					log.Debugf("mq: Done, stdout")
				}
			}
		}
		log.Debugf("mq: Bail out")
	}()

	consumers[vhost + ":" + queue] = cons
	log.Debugf("mq: ... Done");
	return nil
}

func mqStartListener(conf *YAMLConfMw, vhost, queue string) error {
	return factoryMakeReq(&factory_req{vhost: vhost, queue: queue, add: true})
}

func mqStopListener(vhost, queue string) {
	factoryMakeReq(&factory_req{vhost: vhost, queue: queue, add: false})
}

