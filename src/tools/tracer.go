package main

import (
	"net"
	"encoding/json"
	"os"
	"fmt"
	"../apis"
)

func tracerConnect(ten, addr string) (*net.UnixConn, error) {
	ua, err := net.ResolveUnixAddr("unixpacket", addr)
	if err != nil {
		return nil, err
	}

	sk, err := net.DialUnix("unixpacket", nil, ua)
	if err != nil {
		return nil, err
	}

	hm := swyapi.TracerHello{ Tenant: ten }
	data, _ := json.Marshal(&hm)
	_, err = sk.Write(data)
	if err != nil {
		sk.Close()
		return nil, err
	}

	return sk, nil
}

func main() {
	fmt.Printf("Tracing reqs for %s (@%s)\n", os.Args[1], os.Args[2])

	sk, err := tracerConnect(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
		return
	}

	defer sk.Close()

	var prevr uint64
	prevr = 0

	msg := make([]byte, 1024)
	for {
		l, err := sk.Read(msg)
		if err != nil {
			fmt.Printf("Error reading from tracer: %s\n", err.Error())
			break
		}

		var tm swyapi.TracerEvent
		err = json.Unmarshal(msg[:l], &tm)
		if err != nil {
			fmt.Printf("Error parsing message: %s\n", err.Error())
			break
		}

		var rqid string
		if tm.RqID == prevr {
			rqid = "      `-"
		} else {
			rqid = fmt.Sprintf("%08d", tm.RqID)
		}
		fmt.Printf("%s %s%6s:  ", tm.Ts.Format("15:04:05.000"), rqid, tm.Type)
		prevr = tm.RqID

		switch tm.Type {
		case "req":
			fmt.Printf("%s %s\n", tm.Data["method"], tm.Data["path"])
		case "resp":
			fmt.Printf("%s\n", tm.Data["values"])
		case "error":
			fmt.Printf("%d %s\n", tm.Data["code"], tm.Data["message"])
		default:
			fmt.Printf("%v\n", tm.Data)
		}
	}
}
