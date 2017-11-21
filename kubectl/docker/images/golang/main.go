package main

import (
	"swycode"
	"os"
	"strings"
)

func main() {
	args := make(map[string]string)
	for _, v := range os.Args[1:] {
		vs := strings.SplitN(v, "=", 2)
		args[vs[0]] = vs[1]
	}
	swyfn.Function(args)
}
