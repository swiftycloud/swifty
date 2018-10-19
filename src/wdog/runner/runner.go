package main

import (
	"fmt"
	"encoding/json"
	"xqueue"
)

type Request struct {
	Event		string			`json:"event"`
	Args		map[string]string	`json:"args,omitempty"`
	ContentType	string			`json:"content,omitempty"`
	Body		string			`json:"body,omitempty"`
	Claims		map[string]interface{}	`json:"claims,omitempty"` // JWT
	Method		string			`json:"method,omitempty"`
	Path		string			`json:"path,omitempty"`

	B		*Body			`json:"-"`
}

type Responce struct {
	Status	int
}

type RunnerRes struct {
	Res	int
	Ret	string
	Status	int
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

		if req.ContentType == "application/json" {
			var b Body

			err = json.Unmarshal([]byte(req.Body), &b)
			if err == nil {
				req.B = &b
			}
		}

		res, resp := Main(&req)

		var b []byte
		b, err = json.Marshal(res)
		if err != nil {
			fmt.Printf("Can't marshal the result: %s", err.Error())
			return
		}

		out := &RunnerRes { Res: 0, Ret: string(b) }

		if resp != nil {
			out.Status = resp.Status
		}

		err = q.Send(out)
		if err != nil {
			fmt.Printf("Can't send responce: %s", err.Error())
			return
		}
	}
}
