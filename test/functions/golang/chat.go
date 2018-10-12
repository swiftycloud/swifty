package main

import (
	"fmt"
	"os"
	"bytes"
	"net/http"
	"encoding/json"
)

func Main(rq *Request) (interface{}, *Responce) {
	fmt.Printf("Go message\n")
	fmt.Printf("`- from: %s\n", rq.Args["cid"])
	fmt.Printf("`- type: %s\n", rq.Args["mtype"])
	fmt.Printf("`- message: [%s]\n", rq.Body)

	url := os.Getenv("MWARE_WEBSOCKETCHAT_URL")
	url = "http://159.69.216.175:8684/websockets/3557960b7e5b5b26cee6b8d70675c7736c783b69043bfff0ec4fc13f49a02376"
	fmt.Printf("WS: %s\n", url)

	tok := os.Getenv("MWARE_WEBSOCKETCHAT_TOKEN")
	fmt.Printf(" `: %s\n", tok)

	body, _ := json.Marshal(map[string]interface{} {
		"msg_type": 1,
		"msg_payload": []byte("Text message"),
	})

	fmt.Printf("-> REQ with [%s]\n", string(body))
	req, err := http.NewRequest("POST", url + "/conns/" + rq.Args["cid"], bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Error req: %s\n", err.Error())
		return "ok", nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WS-Token", tok)

	fmt.Printf("-> POST to [%s]\n", url + "/" + rq.Args["cid"])
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error post: %s\n", err.Error())
		return "ok", nil
	}

	return "ok", nil
}
