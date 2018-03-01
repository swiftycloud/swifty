package main

import (
	"fmt"
	"swifty"
	"encoding/json"
	"xqueue"
	"os"
	"syscall"
	"strconv"
)

type runnerRes struct {
	Code	int		`json:"code"`
	Return	string		`json:"return"`
}

func main() {
	q, err := xqueue.OpenQueue(os.Args[1])
	if err != nil {
		fmt.Printf("Can't open queue %s: %s", os.Args[1], err.Error())
		return
	}

	fd, _ := strconv.Atoi(os.Args[2])
	syscall.Dup2(fd, 1)
	syscall.Close(fd)
	fd, _ = strconv.Atoi(os.Args[3])
	syscall.Dup2(fd, 2)
	syscall.Close(fd)

	for {
		var args map[string]string

		err = q.Recv(&args)
		if err != nil {
			fmt.Printf("Can't receive message: %s", err.Error())
			return
		}

		res := swifty.Main(args)

		var resb []byte
		resb, err = json.Marshal(res)
		if err != nil {
			fmt.Printf("Can't marshal the result: %s", err.Error())
			return
		}

		err = q.Send(&runnerRes{Code: 0, Return: string(resb)})
		if err != nil {
			fmt.Printf("Can't send responce: %s", err.Error())
			return
		}
	}
}
