/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"log"
	"errors"
	"encoding/hex"
)

type rqShow struct {}
var showAll bool

func (*rqShow)config(mc map[string]interface{}) error {
	x, ok := mc["all"].(bool)
	if !ok {
		return errors.New("all must be bool")
	}

	showAll = x
	log.Printf("Will show requests (all: %v)\n", showAll)
	return nil
}

func (*rqShow)request(conid string, rq *maria_req) error {
	rq.show(conid)
	return nil
}

func (rq *maria_req)show(conid string) {
	if rq.cmd != 0xff {
		if showAll {
			switch rq.cmd {
			case COM_SLEEP, COM_QUIT, COM_INIT_DB, COM_QUERY, COM_FIELD_LIST, COM_CREATE_DB,
					COM_DROP_DB, COM_REFRESH, COM_SHUTDOWN, COM_STATISTICS, COM_PROCESS_INFO,
					COM_CONNECT, COM_PROCESS_KILL, COM_DEBUG, COM_PING, COM_TIME, COM_DELAYED_INSERT,
					COM_CHANGE_USER, COM_RESET_CONNECTION, COM_DAEMON:
				log.Printf("%s: %d:%s\n", conid, rq.cmd, string(rq.data[1:]))
			default:
				log.Printf("%s: %d---\n%s---\n", conid, rq.cmd, hex.Dump(rq.data[1:]))
			}
		}
		return
	}

	log.Printf("%s: %d ======\n%s============================\n", conid, rq.seq, hex.Dump(rq.data))
}
