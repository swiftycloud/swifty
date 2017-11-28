package main

import (
)

type FnStats struct {
	Called	uint64
}

type statsOpaque struct {
	Id string
}

func statsGet(fn *FunctionDesc) *FnStats {
	n, err := logGetCalls(&fn.SwoId)
	if err != nil {
		n = 0
	}

	return &FnStats{Called: uint64(n)}
}

func statsStartCollect(conf *YAMLConf, fn *FunctionDesc) {
}

func statsStopCollect(conf *YAMLConf, fn *FunctionDesc) {
}

func statsStart(id string) *statsOpaque {
	return &statsOpaque{Id: id}
}

func statsUpdate(op *statsOpaque) {
}

func statsInit(conf *YAMLConf) error {
	return nil
}
