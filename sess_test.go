package rudp

import (
	"fmt"
	"testing"
)

func TestOutput1(t *testing.T) {
	buf := []byte{0, 1, 2, 3, 4}
	b := xmitBuf.Get().([]byte)
	fmt.Printf("%T", b)
	var buffers [][]byte
	buffers = [][]byte{b}
	fmt.Println(buffers)
	buffers = [][]byte{b[:len(buf)]}
	fmt.Println(buffers)
}
