package swyapi

import (
	"time"
)

type TracerHello struct {
	ID	string
}

type TracerEvent struct {
	Ts	time.Time
	RqID	uint64
	Type	string
	Data	map[string]interface{}
}
