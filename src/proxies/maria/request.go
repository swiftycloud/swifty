/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"encoding/binary"
)

type maria_req struct {
	rlen	int
	seq	byte
	data	[]byte
	err	string
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

type mariaConsumer struct { }
