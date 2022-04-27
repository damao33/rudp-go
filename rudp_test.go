package rudp

import (
	"fmt"
	"github.com/xtaci/lossyconn"
	"io"
	"net"
	"testing"
	"time"
)

func testlink(t *testing.T, client *lossyconn.LossyConn, server *lossyconn.LossyConn, nodelay, interval, resend, nc int) {
	t.Log("testing with nodelay parameters:", nodelay, interval, resend, nc)
	sess, _ := NewConn2(server.LocalAddr(), nil, 0, 0, client)
	listener, _ := ServeConn(nil, 0, 0, server)
	echoServer := func(l *Listener) {
		for {
			conn, err := l.AcceptRUDP()
			if err != nil {
				return
			}
			go func() {
				conn.SetNoDelay(nodelay, interval, resend, nc)
				buf := make([]byte, 65536)
				for {
					//t.Log("bout to read")
					n, err := conn.Read(buf)
					//t.Log("read finish")
					if err != nil {
						return
					}
					_, err = conn.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}()
		}
	}

	echoTester := func(s *UDPSession, raddr net.Addr) {
		s.SetNoDelay(nodelay, interval, resend, nc)
		buf := make([]byte, 64)
		var rtt time.Duration
		for i := 0; i < 16; i++ {
			start := time.Now()
			_, err := s.Write(buf)
			if err != nil {
				return
			}
			_, err = io.ReadFull(s, buf)
			if err != nil {
				return
			}
			rtt += time.Since(start)
		}

		t.Log("client:", client)
		t.Log("server:", server)
		t.Log("avg rtt:", rtt/16)
		t.Logf("total time: %v for %v round trip:", rtt, 16)
	}

	go echoServer(listener)
	echoTester(sess, server.LocalAddr())
}
func TestLossyConn1(t *testing.T) {
	t.Log("testing loss rate 10%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 1")
	client, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 1)
}

func TestLossyConn4(t *testing.T) {
	t.Log("testing loss rate 10%, rtt 200ms")
	t.Log("testing link with nodelay parameters:1 10 2 0")
	client, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}

	server, err := lossyconn.NewLossyConn(0.1, 100)
	if err != nil {
		t.Fatal(err)
	}
	testlink(t, client, server, 1, 10, 2, 0)
}

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
