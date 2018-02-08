package main

import (
	"time"
)

const gcDefaultPeriod = uint32(10)

func gcInit(period uint32) error {
	if period == 0 { period = gcDefaultPeriod }

	go func() {
		for {
			gc_keys();
			time.Sleep(time.Duration(period) * time.Second)
		}
	}()

	return nil
}
