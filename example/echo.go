package main

import (
	"log"
	"rudp"
	"strconv"
	"time"
)

func main() {

	if listener, err := rudp.ListenWithOptions("127.0.0.1:12344", nil, 0, 0); err == nil {
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
	buf := make([]byte, 50)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("server rcv:", string(buf[:n]))
		sndMsg := "hello back " + string(buf[n-1]) + " !"
		n, err = conn.Write([]byte(sndMsg))
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("server snd:", sndMsg)
	}
}

func client() {
	// wait for server to become ready
	time.Sleep(time.Second)

	// dial to the echo server
	if sess, err := rudp.DialWithOptions("127.0.0.1:12344", nil, 0, 0); err == nil {
		/*for {
			data := time.Now().String()
			buf := make([]byte, len(data))
			log.Println("sent:", data)
			if _, err := sess.Write([]byte(data)); err == nil {
				// read back the data
				if _, err := io.ReadFull(sess, buf); err == nil {
					log.Println("recv:", string(buf))
				} else {
					log.Fatal(err)
				}
			} else {
				log.Fatal(err)
			}
			time.Sleep(time.Second)
		}*/
		buf := make([]byte, 50)
		for i := 0; i < 30; i++ {
			sndMsg := "hello from client " + strconv.Itoa(i%10)
			if _, err := sess.Write([]byte(sndMsg)); err == nil {
				log.Println("client send:", sndMsg)
				if _, err := sess.Read(buf); err == nil {
					log.Println("client recv:", string(buf))
				} else {
					log.Fatal(err)
				}
			}
			//time.Sleep(2 * time.Second)
		}
	} else {
		log.Fatal(err)
	}
}
