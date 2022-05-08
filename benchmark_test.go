package rudp

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkFlush(b *testing.B) {
	rudp := NewRudp(1, func(buf []byte, size int) {})
	rudp.sndBuf = make([]segment, 1024)
	for k := range rudp.sndBuf {
		rudp.sndBuf[k].xmit = 1
		rudp.sndBuf[k].resendts = currentMs() + 10000
	}
	b.ResetTimer()
	b.ReportAllocs()
	var mu sync.Mutex
	for i := 0; i < b.N; i++ {
		mu.Lock()
		rudp.flush(false)
		mu.Unlock()
	}
}

func BenchmarkEchoSpeed4K(b *testing.B) {
	speedclient(b, 4096)
}

var baseport = uint32(10000)

func listenEcho(port int) (net.Listener, error) {
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
}
func handleEcho(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(4096, 4096)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}
func echoServer(port int) net.Listener {
	l, err := listenEcho(port)
	if err != nil {
		panic(err)
	}

	go func() {
		rudplistener := l.(*Listener)
		rudplistener.SetReadBuffer(4 * 1024 * 1024)
		rudplistener.SetWriteBuffer(4 * 1024 * 1024)
		rudplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			// coverage test
			s.(*UDPSession).SetReadBuffer(4 * 1024 * 1024)
			s.(*UDPSession).SetWriteBuffer(4 * 1024 * 1024)
			go handleEcho(s.(*UDPSession))
		}
	}()

	return l
}
func BenchmarkEchoSpeed64K(b *testing.B) {
	speedclient(b, 65536)
}

func BenchmarkEchoSpeed512K(b *testing.B) {
	speedclient(b, 524288)
}

func BenchmarkEchoSpeed1M(b *testing.B) {
	speedclient(b, 1048576)
}

func BenchmarkSinkSpeed4K(b *testing.B) {
	sinkclient(b, 4096)
}

func BenchmarkSinkSpeed64K(b *testing.B) {
	sinkclient(b, 65536)
}

func BenchmarkSinkSpeed256K(b *testing.B) {
	sinkclient(b, 524288)
}

func BenchmarkSinkSpeed1M(b *testing.B) {
	sinkclient(b, 1048576)
}
func sinkServer(port int) net.Listener {
	l, err := listenSink(port)
	if err != nil {
		panic(err)
	}

	go func() {
		rudplistener := l.(*Listener)
		rudplistener.SetReadBuffer(4 * 1024 * 1024)
		rudplistener.SetWriteBuffer(4 * 1024 * 1024)
		rudplistener.SetDSCP(46)
		for {
			s, err := l.Accept()
			if err != nil {
				return
			}

			go handleSink(s.(*UDPSession))
		}
	}()

	return l
}
func listenSink(port int) (net.Listener, error) {
	return ListenWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
}
func dialSink(port int) (*UDPSession, error) {
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetStreamMode(true)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, err
}
func sink_tester(cli *UDPSession, msglen, msgcount int) error {
	// sender
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		if _, err := cli.Write(buf); err != nil {
			return err
		}
	}
	return nil
}
func handleSink(conn *UDPSession) {
	conn.SetStreamMode(true)
	conn.SetWindowSize(4096, 4096)
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetDSCP(46)
	conn.SetMtu(1400)
	conn.SetACKNoDelay(false)
	conn.SetReadDeadline(time.Now().Add(time.Hour))
	conn.SetWriteDeadline(time.Now().Add(time.Hour))
	buf := make([]byte, 65536)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}
func echo_tester(cli net.Conn, msglen, msgcount int) error {
	buf := make([]byte, msglen)
	for i := 0; i < msgcount; i++ {
		// send packet
		if _, err := cli.Write(buf); err != nil {
			return err
		}

		// receive packet
		nrecv := 0
		for {
			n, err := cli.Read(buf)
			if err != nil {
				return err
			} else {
				nrecv += n
				if nrecv == msglen {
					break
				}
			}
		}
	}
	return nil
}
func dialEcho(port int) (*UDPSession, error) {
	sess, err := DialWithOptions(fmt.Sprintf("127.0.0.1:%v", port), nil, 0, 0)
	if err != nil {
		panic(err)
	}

	sess.SetStreamMode(true)
	sess.SetStreamMode(false)
	sess.SetStreamMode(true)
	sess.SetWindowSize(1024, 1024)
	sess.SetReadBuffer(16 * 1024 * 1024)
	sess.SetWriteBuffer(16 * 1024 * 1024)
	sess.SetStreamMode(true)
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetMtu(1400)
	sess.SetMtu(1600)
	sess.SetMtu(1400)
	sess.SetACKNoDelay(true)
	sess.SetACKNoDelay(false)
	sess.SetDeadline(time.Now().Add(time.Minute))
	return sess, err
}
func speedclient(b *testing.B, nbytes int) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := echoServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialEcho(port)
	if err != nil {
		panic(err)
	}

	if err := echo_tester(cli, nbytes, b.N); err != nil {
		b.Fail()
	}
	b.SetBytes(int64(nbytes))
	cli.Close()
}
func sinkclient(b *testing.B, nbytes int) {
	port := int(atomic.AddUint32(&baseport, 1))
	l := sinkServer(port)
	defer l.Close()

	b.ReportAllocs()
	cli, err := dialSink(port)
	if err != nil {
		panic(err)
	}

	sink_tester(cli, nbytes, b.N)
	b.SetBytes(int64(nbytes))
	cli.Close()
}
