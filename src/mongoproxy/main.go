package main

import (
	"flag"
)

func main() {
	pipelineAdd(&rqShow{})

	var from string
	var to string

	flag.StringVar(&from, "from", ":27018", "Where to listen for connections")
	flag.StringVar(&to, "to", "127.0.0.1:27017", "Where to connect for mongo")
	flag.Parse()

	p := makeProxy(from, to)
	if p == nil {
		return
	}

	defer p.Close()

	p.Run()
}
