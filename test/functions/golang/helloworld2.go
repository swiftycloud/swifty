package main

import (
	"fmt"
)

func Main(rq *Request) (interface{}, *Response) {
	fmt.Printf("Called with %v\n", rq.Args)
	return map[string]string{"message": "hw2:golang:" + rq.Args["name"]}, nil
}
