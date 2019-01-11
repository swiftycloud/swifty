/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"strings"
	"encoding/binary"
	"gopkg.in/mgo.v2/bson"
)

type mr_info struct {
	typ	string
	act	string
	db	string
	col	string
}

type mongo_req struct {
	rlen	int
	reqid	uint32
	respto	uint32
	opcode	uint32

	q	*mongo_query
	c	*mongo_cmd
	data	[]byte
	err	string

	inf	*mr_info
}

type mongo_query struct {
	flags	uint32
	skip	uint32
	retn	uint32
	col	string
	doc	bson.D
}

type mongo_cmd struct {
	db	string
	cmd	string
	doc	bson.D
}

func read_cstring(buf []byte) (string, int) {
	for i := 0; i < len(buf); i++ {
		if buf[i] == '\x00' {
			return string(buf[:i]), i + 1
		}
	}

	return "", -1
}

func read_document(buf []byte) (bson.D, string, int) {
	if len(buf) < 4 {
		return nil, "empty doc", 0
	}

	/* Now the query doc */
	dln := int(binary.LittleEndian.Uint32(buf))
	if dln < 4 || len(buf) < dln {
		return nil, "short doc", dln
	}

	var doc bson.D
	err := bson.Unmarshal(buf[:dln], &doc)
	if err != nil {
		return nil, "doc decode " + err.Error(), dln
	}

	return doc, "", dln
}

var query_ops = map[string]bool {
	"find":			true,
	"update":		true,
	"insert":		true,
	"delete":		true,
	"count":		true,
	"aggregate":		true,
	"createIndexes":	true,
}

var service_ops = map[string]bool {
	"ping":			true,
	"ismaster":		true,
	"isMaster":		true,
	"getnonce":		true,
	"logout":		true,
	"saslStart":		true,
	"saslContinue":		true,
}

func decode_doc(typ, db string, doc bson.D) *mr_info {
	name := doc[0].Name

	_, ok := query_ops[name]
	if ok {
		return &mr_info {
			typ:	typ,
			act:	name,
			db:	db,
			col:	doc[0].Value.(string),
		}
	}

	_, ok = service_ops[name]
	if ok {
		return &mr_info {
			typ:	typ,
			act:	name,
			db:	db,
		}
	}

	return nil
}

func decode_mongo_query(buf []byte) (*mongo_query, *mr_info, string) {
	/* First come int and nul-terminated string */
	if len(buf) < 4 {
		return nil, nil, "short data"
	}

	/* Query is int, cstring, int, int then doc */
	col, ln := read_cstring(buf[4:])
	if ln < 0 || len(buf) < 12 + ln {
		return nil, nil, "colname overflow"
	}

	doc, errm, _ := read_document(buf[12+ln:])

	q := &mongo_query {
		col:	col,
		doc:	doc,
		flags:	binary.LittleEndian.Uint32(buf[0:]),
		skip:	binary.LittleEndian.Uint32(buf[4+ln:]),
		retn:	binary.LittleEndian.Uint32(buf[8+ln:]),
	}

	var inf *mr_info

	cs := strings.SplitN(col, ".", 2)
	if len(cs) == 2 && cs[1] == "$cmd" {
		inf = decode_doc("query", cs[0], doc)
	}

	return q, inf, errm
}

func decode_mongo_cmd(buf []byte) (*mongo_cmd, *mr_info, string) {
	db, dln := read_cstring(buf)
	if dln < 0 {
		return nil, nil, "db overflow"
	}

	cmd, cln := read_cstring(buf[dln:])
	if cln < 0 {
		return nil, nil, "cmd overflow"
	}

	doc, errm, _ := read_document(buf[dln+cln:])
	c := &mongo_cmd{ db: db, cmd: cmd, doc: doc }

	var inf *mr_info

	if doc[0].Name == cmd {
		inf = &mr_info {
			typ:	"cmd",
			act:	cmd,
			db:	db,
		}
		switch y := doc[0].Value.(type) {
		case string:
			inf.col = y
		}
	}

	return c, inf, errm
}

func decode_mongo_req(data []byte) *mongo_req {
	if len(data) < 16 {
		return nil
	}

	rq := &mongo_req {
		rlen:	int(binary.LittleEndian.Uint32(data[0:])),
		reqid:	binary.LittleEndian.Uint32(data[4:]),
		respto:	binary.LittleEndian.Uint32(data[8:]),
		opcode:	binary.LittleEndian.Uint32(data[12:]),
	}

	if len(data) < rq.rlen {
		return nil
	}

	rq.data = data[:rq.rlen]
	if rq.rlen >= 16 {
		switch rq.opcode {
		case 2004:
			rq.q, rq.inf, rq.err = decode_mongo_query(data[16:rq.rlen])
		case 2010:
			rq.c, rq.inf, rq.err = decode_mongo_cmd(data[16:rq.rlen])
		}
	}

	return rq
}

func (*mgoConsumer)Try(conid string, data []byte) (int, error) {
	rq := decode_mongo_req(data)
	if rq == nil {
		return 0, nil
	}

	err := pipelineRun(conid, rq)
	if err != nil {
		return 0, err
	}

	return rq.rlen, nil
}

type mgoConsumer struct { }
