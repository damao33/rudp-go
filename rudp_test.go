package rudp

import (
	"fmt"
	"testing"
)

func TestSend(t *testing.T) {
	rudp := NewRudp(1, func(buf []byte, size int) {})
	rudp.stream = 1
	rudp.mss = 3
	sndQueue := make([]segment, 1)
	sndQueue[0].data = []byte{0, 1, 2}
	rudp.snd_queue = sndQueue
	for i := 0; i < len(rudp.snd_queue); i++ {
		fmt.Println(i)
		fmt.Println(rudp.snd_queue[i])
	}
	rudp.Send([]byte{3, 4, 5, 6})
	for i := 0; i < len(rudp.snd_queue); i++ {
		fmt.Println(i)
		fmt.Println(rudp.snd_queue[i])
	}
}

func TestXmitBuf(t *testing.T) {
	data := xmitBuf.Get().([]byte)[:2]
	fmt.Println(data)
	xmitBuf.Put(make([]byte, 10))
	data1 := xmitBuf.Get().([]byte)[:2]
	fmt.Println(data1)
}

func TestPeekSize(t *testing.T) {
	rudp := NewRudp(1, func(buf []byte, size int) {})
	rudp.rcv_queue = append(rudp.rcv_queue, segment{frg: 3, data: []byte{0, 1, 2, 3}})
	rudp.rcv_queue = append(rudp.rcv_queue, segment{frg: 2, data: []byte{0, 1, 2}})
	rudp.rcv_queue = append(rudp.rcv_queue, segment{frg: 1, data: []byte{0, 1}})
	rudp.rcv_queue = append(rudp.rcv_queue, segment{frg: 0, data: []byte{0}})
	rudp.rcv_queue = append(rudp.rcv_queue, segment{frg: 0, data: []byte{7, 8, 9}})
	fmt.Println(rudp.peekSize())
}
