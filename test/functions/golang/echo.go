package main

import (
	"fmt"
)

func Main(req *Request) interface{} {
	fmt.Printf("%v\n", req)
	return map[string]string{"message": "hw:golang:" + req.Args["name"]}
}
