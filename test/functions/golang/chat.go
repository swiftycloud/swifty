package main

import (
	"fmt"
	"os"
	"bytes"
	"net/http"
	"strings"
	"encoding/json"
)

func Main(rq *Request) (interface{}, *Responce) {
	chname := strings.ToUpper(os.Getenv("CHAT_NAME"))
	url := os.Getenv("MWARE_WEBSOCKET" + chname + "_URL")
	tok := os.Getenv("MWARE_WEBSOCKET" + chname + "_TOKEN")

	msg := rq.Claims["userid"].(string) + ":" + rq.Body

	body, _ := json.Marshal(map[string]interface{} {
		"msg_type": 1,
		"msg_payload": []byte(msg),
	})

	req, err := http.NewRequest("POST", url + "/conns", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Error req: %s\n", err.Error())
		return "ok", nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WS-Token", tok)

	_, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error post: %s\n", err.Error())
		return "ok", nil
	}

	return "ok", nil
}
