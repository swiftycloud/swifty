package main

import (
	"io"
	"net"
	"log"
	"strconv"
)

type processor interface {
	dataReady([]byte) error
}

func (fw *sender)dataReady(data []byte) error {
	for {
		w, err := fw.to.Write(data)
		if err != nil {
			log.Printf("%s: Error writing: %s\n", fw.id, err.Error())
			return err
		}

		data = data[w:]
		if len(data) == 0 {
			return nil
		}
	}
}

type sender struct {
	id	string
	to	*net.TCPConn
}

func (f *collector)dataReady(data []byte) error {
	f.collected = append(f.collected, data...)

	for {
		cl, err := f.cons.try(f.sender.id, f.collected)
		if cl == 0 {
			return err
		}

		cons := f.collected[:cl]
		f.collected = f.collected[cl:]

		err = f.sender.dataReady(cons)
		if err != nil {
			return err
		}
	}
}

type consumer interface {
	try(string, []byte) (int, error)
}

type collector struct {
	sender
	collected	[]byte
	cons		consumer
}

func forward(conid string, from *net.TCPConn, prc processor, done chan bool) {
	data := make([]byte, 1024)

	for {
		r, err := from.Read(data)
		if err != nil {
			if err == io.EOF {
				log.Printf("%s: Client closed connection\n", conid)
				done <-true
				return
			}

			log.Printf("%s: Error reading: %s\n", conid, err.Error())
			done <-false
			return
		}

		err = prc.dataReady(data[:r])
		if err != nil {
			from.CloseRead()
			done <-false
			return
		}
	}
}

func handle(conid string, con *net.TCPConn, to *net.TCPAddr, cons consumer) {
	log.Printf("%s: Accepted conn from %s\n", conid, con.RemoteAddr())

	defer con.Close()

	tgt, err := net.DialTCP("tcp", nil, to)
	if err != nil {
		log.Printf("%s: Error connecting to target: %s\n", conid, err.Error())
		return
	}

	defer tgt.Close()

	done_ing := make(chan bool)
	done_oug := make(chan bool)

	ing := &collector {
		sender:		sender { id: conid, to: tgt },
		collected:	[]byte{},
		cons:		cons,
	}
	go forward(conid + ".ing", con, ing, done_ing)

	oug := &sender { id: conid, to: con }
	go forward(conid + ".oug", tgt, oug, done_oug)

	select {
	case <-done_ing:
		tgt.CloseWrite()
		<-done_oug
	case <-done_oug:
		con.CloseWrite()
		<-done_ing
	}

	log.Printf("%s: Proxy done\n", conid)
}

type Proxy struct {
	ls	*net.TCPListener
	tgt	*net.TCPAddr
	cons	consumer
}

func makeProxy(from, to string, cons consumer) *Proxy {
	x, err := net.ResolveTCPAddr("tcp", from)
	if err != nil {
		log.Printf("Error resolving local: %s\n", err.Error())
		return nil
	}

	ls, err := net.ListenTCP("tcp", x)
	if err != nil {
		log.Printf("Error starting listener: %s\n", err.Error())
		return nil
	}

	x, err = net.ResolveTCPAddr("tcp", to)
	if err != nil {
		log.Printf("Error resolving remote: %s\n", err.Error())
		return nil
	}

	return &Proxy { ls: ls, tgt: x, cons: cons }
}

func (p *Proxy)Run() {
	conid := 0
	for {
		con, err := p.ls.AcceptTCP()
		if err != nil {
			log.Printf("Error accepting; %s\n", err.Error())
			break
		}

		cid := strconv.Itoa(conid)
		conid++

		go handle(cid, con, p.tgt, p.cons)
	}
}

func (p *Proxy)Close() {
	p.ls.Close()
}
