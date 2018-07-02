package main

import (
	"fmt"
	"encoding/json"
	"xqueue"
)

type Request struct {
	Args		map[string]string	`json:"args,omitempty"`
	Body		string			`json:"body,omitempty"`
	Claims		map[string]interface{}	`json:"claims,omitempty"` // JWT
	Method		string			`json:"request,omitempty"`
	Path		string			`json:"path,omitempty"`
}

type Responce struct {
}

type RunnerRes struct {
	Res	int
	Ret	string
}

func use(resp *Responce) {}

func main() {

	q, err := xqueue.OpenQueue("3")
	if err != nil {
		fmt.Printf("Can't open queue 3: %s", err.Error())
		return
	}

	for {
		var req Request

		err = q.Recv(&req)
		if err != nil {
			fmt.Printf("Can't receive message: %s", err.Error())
			return
		}

		res, resp := Main(&req)

		use(resp)

		var b []byte
		b, err = json.Marshal(res)
		if err != nil {
			fmt.Printf("Can't marshal the result: %s", err.Error())
			return
		}

		err = q.Send(&RunnerRes{ Res: 0, Ret: string(b) })
		if err != nil {
			fmt.Printf("Can't send responce: %s", err.Error())
			return
		}
	}
}
