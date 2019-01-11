/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"log"
)

var pipeline []module

func pipelineRun(conid string, rq *maria_req) error {
	for _, n := range pipeline {
		err := n.request(conid, rq)
		if err != nil {
			log.Printf("%s: notify error: %s\n", conid, err.Error())
			return err
		}
	}

	return nil
}

func pipelineAdd(n module) {
	pipeline = append(pipeline, n)
}
