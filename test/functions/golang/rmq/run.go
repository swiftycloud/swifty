package main

import (
	"os"
	"fmt"
	"github.com/streadway/amqp"
)

func main() {
	addr := os.Getenv("MWARE_TESTRABBIT_ADDR")
	user := os.Getenv("MWARE_TESTRABBIT_USER")
	pass := os.Getenv("MWARE_TESTRABBIT_PASS")
	vhost := os.Getenv("MWARE_TESTRABBIT_VHOST")

	conn, err := amqp.Dial("amqp://" + user + ":" + pass + "@" + addr + "/" + vhost)
	if err != nil {
		fmt.Println(err)
		panic("Can't dial")
	}
	defer conn.Close()

	chnl, err := conn.Channel()
	if err != nil {
		fmt.Println(err)
		panic("Can't make channel")
	}
	defer chnl.Close()

	q, err := chnl.QueueDeclare(os.Args[2], false, false, false, false, nil)
	if err != nil {
		fmt.Println(err)
		panic("Can't declare queue")
	}

	if os.Args[1] == "send" {
		err = chnl.Publish("", q.Name, false, false,
				amqp.Publishing {
					ContentType: "text/plain",
					UserId: user,
					Body: []byte(os.Args[3]),
				})
		if err != nil {
			fmt.Println(err)
			panic("Can't publish")
		}

		fmt.Println("Sent", os.Args[3])
	} else if os.Args[1] == "recv" {
		msgs, err := chnl.Consume(q.Name, "", true, false, false, false, nil)
		if err != nil {
			fmt.Println(err)
			panic("Can't consume")
		}

		for m := range msgs {
			fmt.Println("golang:mq:" + string(m.Body))
			break;
		}
	}
}
