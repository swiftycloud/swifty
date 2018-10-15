package main

import (
	"flag"
	"log"
	"net/url"
	"os"
	"fmt"
	"bufio"
	"net/http"

	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "localhost:8080", "http service address")
var path = flag.String("path", "", "ws path")
var token = flag.String("token", "", "JWT value")

func main() {
	flag.Parse()
	log.SetFlags(0)

	u := url.URL{Scheme: "ws", Host: *addr, Path: *path}
	log.Printf("connecting to %s", u.String())

	h := http.Header{}
	h.Set("Authorization", "Bearer " + token)
	c, _, err := websocket.DefaultDialer.Dial(u.String(), h)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	log.Printf("connected\n")
	fmt.Print("_: ")

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read error:", err)
				return
			}
			log.Printf(":> %s\n", message)
			fmt.Print("_: ")
		}
	}()

	for {
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		if text == "" {
			break
		}

		err := c.WriteMessage(websocket.TextMessage, []byte(text))
		if err != nil {
			log.Println("write:", err)
			return
		}
	}

	c.Close()
}
