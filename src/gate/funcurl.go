package main

import (
	"context"
)

func urlSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	/* FIXME -- set up public IP address/port for this FN */
	if on {
		fn.URLCall = true
	}
	return nil
}
