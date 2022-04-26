//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
)

func main() {
	addr := "127.0.0.1:8877"
	conn, err := net.Dial("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		line := input.Text()
		_, err = conn.Write([]byte(line))
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("send:", line)

		msg := make([]byte, 255)
		_, err := conn.Read(msg)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("rev:", string(msg))
	}

}
