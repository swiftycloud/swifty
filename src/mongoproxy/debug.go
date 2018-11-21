package main

import (
	"log"
	"encoding/hex"
	"gopkg.in/mgo.v2/bson"
)

type rqShow struct {}

func (*rqShow)request(conid string, rq *mongo_req) {
	rq.show(conid)
}

func (rq *mongo_req)show(conid string) {
	if rq.inf != nil {
		log.Printf("%s: %s.%s@%s.%s\n", conid, rq.inf.typ, rq.inf.act, rq.inf.db, rq.inf.col)
		return
	}

	log.Printf("---UNKNOWN MESSAGE---------------------------\n")
	log.Printf("%s: RQ %d(%d) op%d (%d bytes)\n", conid, rq.reqid, rq.respto, rq.opcode, rq.rlen)
	switch {
	case rq.q != nil:
		rq.q.show(conid)
	case rq.c != nil:
		rq.c.show(conid)
	case rq.data != nil:
		log.Printf("%s: RAW: ======\n%s============================\n", conid, hex.Dump(rq.data))
	}

	if rq.err != "" {
		log.Printf("%s: ERROR decoding: %s\n", conid, rq.err)
	}
	log.Printf("------------------8<-------------------------\n")
}

func show_doc(cid string, doc bson.D) {
	for _, de := range doc {
		log.Printf("%s: %s = %v\n", cid, de.Name, de.Value)
	}
}

func (mq *mongo_query)show(conid string) {
	log.Printf("%s: QUERY[%x](%d:%d) %s\n", conid, mq.flags, mq.skip, mq.retn, mq.col)
	show_doc(conid, mq.doc)
}

func (mc *mongo_cmd)show(conid string) {
	log.Printf("%s: CMD.%s@%s\n", conid, mc.cmd, mc.db)
	show_doc(conid, mc.doc)
}
