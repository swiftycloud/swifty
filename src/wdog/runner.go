package main

import (
	"fmt"
	"encoding/json"
	"xqueue"
)

type runnerRes struct {
	Code	int		`json:"code"`
	Return	string		`json:"return"`
}

func main() {

	q, err := xqueue.OpenQueue("3")
	if err != nil {
		fmt.Printf("Can't open queue 3: %s", err.Error())
		return
	}

	for {
		var args map[string]string

		err = q.Recv(&args)
		if err != nil {
			fmt.Printf("Can't receive message: %s", err.Error())
			return
		}

		res := Main(args)

		var resb []byte
		resb, err = json.Marshal(res)
		if err != nil {
			fmt.Printf("Can't marshal the result: %s", err.Error())
			return
		}

		err = q.SendBytes([]byte("0:" + string(resb)))
		if err != nil {
			fmt.Printf("Can't send responce: %s", err.Error())
			return
		}
	}
}
