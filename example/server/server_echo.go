package main

import (
	"github.com/damao33/rudp-go"
	"log"
	"time"
)

func main() {
	if listener, err := rudp.ListenWithOptions("127.0.0.1:12345", nil, 0, 0); err == nil {
		// spin-up the client
		log.Println("server listening on port:12345...")
		for {
			s, err := listener.AcceptRUDP()
			if err != nil {
				log.Fatal(err)
			}
			log.Println("listen:", s.LocalAddr())
			go handleEcho(s)
		}
	} else {
		log.Fatal(err)
	}
}

// handleEcho send back everything it received
func handleEcho(conn *rudp.UDPSession) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("server rcv:", string(buf[:n]))
		time.Sleep(10 * time.Millisecond)
		n, err = conn.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("server snd:", string(buf[:n]))
	}
}
