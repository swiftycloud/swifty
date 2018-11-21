package main

type notify interface {
	request(string, *mongo_req) error
}

var pipeline []notify

func pipelineRun(conid string, rq *mongo_req) error {
	for _, n := range pipeline {
		err := n.request(conid, rq)
		if err != nil {
			return err
		}
	}

	return nil
}

func pipelineAdd(n notify) {
	pipeline = append(pipeline, n)
}
