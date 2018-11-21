package main

import (
	"flag"
)

func main() {
	var from string
	var to string
	var to_db, to_user, to_pass string

	flag.StringVar(&from, "from", ":27018", "Where to listen for connections")
	flag.StringVar(&to, "to", "127.0.0.1:27017", "Where to connect for mongo")

	flag.StringVar(&to_db, "d", "", "DB for policies")
	flag.StringVar(&to_user, "u", "", "User for policies")
	flag.StringVar(&to_pass, "p", "", "Pass for policies")
	flag.Parse()

	pipelineAdd(&rqShow{})
	pipelineAdd(&quota{})
	quotaSetCreds(to, to_user, to_pass, to_db)

	p := makeProxy(from, to)
	if p == nil {
		return
	}

	defer p.Close()

	p.Run()
}
