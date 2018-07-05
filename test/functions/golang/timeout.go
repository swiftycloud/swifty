package main

import (
	"time"
	"strconv"
)

func Main(rq *Request) (interface{}, *Responce) {
	tmo, _ := strconv.Atoi(rq.Args["tmo"])
	time.Sleep(time.Duration(tmo) * time.Millisecond)
	return "slept:" + rq.Args["tmo"], nil
}
