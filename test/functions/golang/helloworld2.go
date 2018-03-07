package main

import (
	"fmt"
)

func Main(args map[string]string) interface{} {
	fmt.Printf("Called with %v\n", args)
	return map[string]string{"message": "hw2:golang:" + args["name"]}
}
