/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"encoding/binary"
)

const (
	COM_SLEEP			= 0x00
	COM_QUIT			= 0x01
	COM_INIT_DB			= 0x02
	COM_QUERY			= 0x03
	COM_FIELD_LIST			= 0x04
	COM_CREATE_DB			= 0x05
	COM_DROP_DB			= 0x06
	COM_REFRESH			= 0x07
	COM_SHUTDOWN			= 0x08
	COM_STATISTICS			= 0x09
	COM_PROCESS_INFO		= 0x0a
	COM_CONNECT			= 0x0b
	COM_PROCESS_KILL		= 0x0c
	COM_DEBUG			= 0x0d
	COM_PING			= 0x0e
	COM_TIME			= 0x0f
	COM_DELAYED_INSERT		= 0x10
	COM_CHANGE_USER			= 0x11
	COM_BINLOG_DUMP			= 0x12
	COM_TABLE_DUMP			= 0x13
	COM_CONNECT_OUT			= 0x14
	COM_REGISTER_SLAVE		= 0x15
	COM_STMT_PREPARE		= 0x16
	COM_STMT_EXECUTE		= 0x17
	COM_STMT_SEND_LONG_DATA		= 0x18
	COM_STMT_CLOSE			= 0x19
	COM_STMT_RESET			= 0x1a
	COM_SET_OPTION			= 0x1b
	COM_STMT_FETCH			= 0x1c
	COM_DAEMON			= 0x1d
	COM_BINLOG_DUMP_GTID		= 0x1e
	COM_RESET_CONNECTION		= 0x1f
)

type maria_req struct {
	rlen	int
	seq	byte
	data	[]byte
	err	string

	cmd	byte
}

func decode_maria_req(data []byte) *maria_req {
	if len(data) < 4 {
		return nil
	}

	var hdr [4]byte
	hdr[0] = data[0]
	hdr[1] = data[1]
	hdr[2] = data[2]
	hdr[3] = 0 /* in data there's seq here */

	ln := binary.LittleEndian.Uint32(hdr[:])
	if len(data) < int(ln) {
		return nil
	}

	rq := &maria_req{ rlen: int(ln) + 4, seq: data[3], data: data[4:4+ln] }

	if rq.seq == 0 && ln >= 5 {
		rq.cmd = data[4]
	} else {
		rq.cmd = 0xff
	}

	return rq
}

func (*mariaConsumer)Try(conid string, data []byte) (int, error) {
	rq := decode_maria_req(data)
	if rq == nil {
		return 0, nil
	}

	err := pipelineRun(conid, rq)
	if err != nil {
		return 0, err
	}

	return rq.rlen, nil
}

func (*mariaConsumer)Done(conid string) {
}

type mariaConsumer struct { }
