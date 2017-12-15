package main

import (
	"sync/atomic"
)

type FnStats struct {
	Called	uint64
}

type statsOpaque struct {
}

func statsGet(fn *FunctionDesc) *FnStats {
	n, err := logGetCalls(&fn.SwoId)
	if err != nil {
		n = 0
	}

	return &FnStats{Called: uint64(n)}
}

func statsStart() *statsOpaque {
	return &statsOpaque{}
}

func statsUpdate(fmd *FnMemData, op *statsOpaque) {
	atomic.AddUint32(&fmd.calls, 1)
}

func statsInit(conf *YAMLConf) error {
	return nil
}
