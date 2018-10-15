package main

import (
	"fmt"
	"os"
	"bytes"
	"net/http"
	"encoding/json"
	"encoding/base64"
)

func Main(rq *Request) (interface{}, *Responce) {
	url := os.Getenv("MWARE_WEBSOCKETCHAT_URL")
	tok := os.Getenv("MWARE_WEBSOCKETCHAT_TOKEN")

	data, err := base64.StdEncoding.DecodeString(rq.Body)
	body, _ := json.Marshal(map[string]interface{} {
		"msg_type": 1,
		"msg_payload": []byte(data),
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
