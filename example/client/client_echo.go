package main

import (
	"flag"
	"github.com/damao33/rudp-go"
	"log"
	"reflect"
	"strconv"
	"time"
)

var ipaddr = flag.String("c", "127.0.0.1", "server addr")
var port = flag.Int("port", 12345, "port")

func main() {
	flag.Parse()
	client()
}

func client() {
	// wait for server to become ready
	time.Sleep(time.Second)

	// dial to the echo server
	addr := *ipaddr + ":" + strconv.Itoa(*port)
	log.Println("client connecting to", addr, "...")
	if sess, err := rudp.DialWithOptions(addr, nil, 0, 0); err == nil {
		log.Println("client connected to server successfully")
		for {
			timenow := time.Now()
			data := time.Now().String()
			buf := make([]byte, len(data))
			//log.Println("sent:", data)
			if _, err := sess.Write([]byte(data)); err == nil {
				log.Println("client snd:", data)
				// read back the data
				//if _, err := io.ReadFull(sess, buf); err == nil {
				if _, err := sess.Read(buf); err == nil {
					log.Println("client rcv:", string(buf))
					if reflect.DeepEqual(buf, []byte(data)) {
						log.Println("snd equals rcv")
					} else {
						log.Println("snd not equals rcv")
					}
					rcvtime := time.Now()
					log.Println("recvRTT:", rcvtime.Sub(timenow))
				} else {
					log.Fatal(err)
				}
			} else {
				log.Fatal(err)
			}
			time.Sleep(2 * time.Second)
		}
	} else {
		log.Fatal(err)
	}
}
