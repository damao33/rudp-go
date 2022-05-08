package main

import (
	"github.com/damao33/rudp-go"
	"io"
	"log"
	"time"
)

func main() {

	if listener, err := rudp.ListenWithOptions("127.0.0.1:12345", nil, 0, 0); err == nil {
		// spin-up the client
		go client()
		for {
			s, err := listener.AcceptRUDP()
			if err != nil {
				log.Fatal(err)
			}
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
		time.Sleep(10 * time.Millisecond)
		n, err = conn.Write(buf[:n])
		if err != nil {
			log.Println(err)
			return
		}
	}
}

func client() {
	// wait for server to become ready
	time.Sleep(time.Second)

	// dial to the echo server
	if sess, err := rudp.DialWithOptions("127.0.0.1:12345", nil, 0, 0); err == nil {
		for {
			timenow := time.Now()
			data := make([]byte, 16*1024)
			buf := make([]byte, len(data))
			//log.Println("sent:", data)
			if _, err := sess.Write([]byte(data)); err == nil {
				// read back the data
				if _, err := io.ReadFull(sess, buf); err == nil {
					//log.Println("recv:", string(buf))
					rcvtime := time.Now()
					log.Println("recvRTT:", rcvtime.Sub(timenow))
				} else {
					log.Fatal(err)
				}
			} else {
				log.Fatal(err)
			}
			time.Sleep(time.Second)
		}
	} else {
		log.Fatal(err)
	}
}
