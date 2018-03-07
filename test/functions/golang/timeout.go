package main

import (
	"time"
	"strconv"
)

func Main(args map[string]string) interface{} {
	tmo, _ := strconv.Atoi(args["tmo"])
	time.Sleep(time.Duration(tmo) * time.Millisecond)
	return "slept:" + args["tmo"]
}
