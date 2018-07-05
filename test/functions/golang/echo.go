package main

import (
	"fmt"
)

func Main(req *Request) (interface{}, *Responce) {
	fmt.Printf("Rq: %v\n", req)
	fmt.Printf("Claims: %v\n", req.Claims)
	return map[string]string{"message": "hw:golang:" + req.Args["name"]}, &Responce{Status: 201}
}
