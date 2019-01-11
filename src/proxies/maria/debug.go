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
	log.Printf("%s: %d ======\n%s============================\n", conid, rq.seq, hex.Dump(rq.data))
}
