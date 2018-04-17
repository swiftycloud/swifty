package main

import (
	"context"
)

func urlSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	/* FIXME -- set up public IP address/port for this FN */
	return nil
}

var EventURL = EventOps {
	Setup: urlSetup,
}
