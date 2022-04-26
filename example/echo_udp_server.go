//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"log"
	"net"
	"strings"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:8877")
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	for {
		data := make([]byte, 255)
		_, raddr, err := conn.ReadFromUDP(data)
		if err != nil {
			fmt.Println("read error : ", err)
			continue
		}

		strData := string(data)
		fmt.Println("received: ", strData)

		upper := strings.ToUpper(strData)
		_, err = conn.WriteToUDP([]byte(upper), raddr)
		if err != nil {
			fmt.Println("write error : ", err)
			continue
		}
		fmt.Println("send : ", upper)
	}
}
