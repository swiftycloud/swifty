package main

type notify interface {
	request(string, *mongo_req)
}

var pipeline []notify

func pipelineRun(conid string, rq *mongo_req) {
	for _, n := range pipeline {
		n.request(conid, rq)
	}
}

func pipelineAdd(n notify) {
	pipeline = append(pipeline, n)
}
