package rudp

import (
	"crypto/rand"
	"encoding/binary"
	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TODO
const (
	// 16-bytes nonce for each packet
	nonceSize = 16

	// 4-bytes packet checksum
	crcSize = 4

	// overall crypto header size
	cryptHeaderSize = nonceSize + crcSize

	// maximum packet size
	mtuLimit = 1500

	// accept backlog
	acceptBacklog = 128
)

type (
	UDPSession struct {
		conn     net.PacketConn // 底层传输协议
		ownConn  bool           // true:内部创建连接 false:调用者提供连接
		rudp     *RUDP          // 可靠UDP协议
		listener *Listener
		block    BlockCrypt

		// rudp基于包传递消息
		// rcvBuf把包转换成流
		rcvBuf []byte
		bufPtr []byte

		// TODO ： FEC

		// 设置项
		remote     net.Addr // 远端地址
		readDdl    time.Time
		writeDdl   time.Time
		headerSize int
		ackNoDelay bool // 每个包马上发回ack
		writeDelay bool // 在Write中延迟flush
		dup        int  // 重复UDP包

		// 通知
		die          chan struct{} // 通知当前会话关闭
		dieOnce      sync.Once
		chReadEvent  chan struct{} // Read可以无阻塞被调用
		chWriteEvent chan struct{} // Write可以被无阻塞调用

		// socket错误处理
		socketReadError      atomic.Value
		socketWriteError     atomic.Value
		chSocketReadError    chan struct{}
		chSocketWriteError   chan struct{}
		socketReadErrorOnce  sync.Once
		socketWriteErrorOnce sync.Once

		// TODO nonce?

		// 等待发送的数据包
		txqueue         []ipv4.Message
		xconn           batchConn // for x/net
		xconnWriteError error

		mu sync.Mutex
	}
	setReadBuffer interface {
		SetReadBuffer(bytes int) error
	}

	setWriteBuffer interface {
		SetWriteBuffer(bytes int) error
	}

	setDSCP interface {
		SetDSCP(int) error
	}
)

var (
	xmitBuf = sync.Pool{
		New: func() interface{} {
			return make([]byte, mtuLimit)
		},
	}
)

// Read implements net.Conn
func (s *UDPSession) Read(b []byte) (n int, err error) {
	for {
		s.mu.Lock()
		// 从buffer中读取字节
		if len(s.bufPtr) > 0 {
			n = copy(b, s.bufPtr)
			s.bufPtr = s.bufPtr[n:]
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
			return n, nil
		}

		if size := s.rudp.peekSize(); size > 0 {
			if len(b) >= size {
				s.rudp.Receive(b)
				s.mu.Unlock()
				atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(size))
				return size, nil
			}
			// 如果有必要，重新调整rcvBuf大小以适应数据
			if cap(s.rcvBuf) < size {
				s.rcvBuf = make([]byte, size)
			}
			s.rcvBuf = s.rcvBuf[:size]
			s.rudp.Receive(s.rcvBuf)
			n = copy(b, s.rcvBuf)
			s.bufPtr = s.rcvBuf[n:]
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesReceived, uint64(n))
			return n, nil
		}

		var timeout *time.Timer
		var c <-chan time.Time
		if !s.readDdl.IsZero() {
			if time.Now().After(s.readDdl) {
				s.mu.Unlock()
				return 0, errors.WithStack(errTimeout)
			}
			delay := time.Until(s.readDdl)
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()
		select {
		case <-s.chReadEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errors.WithStack(errTimeout)
		case <-s.chSocketReadError:
			return 0, s.socketReadError.Load().(error)
		case <-s.die:
			return 0, io.ErrClosedPipe
		}
	}
}

// Write implements net.Conn
func (s *UDPSession) Write(b []byte) (n int, err error) {
	return s.WriteBuffers([][]byte{b})
}

func (s *UDPSession) WriteBuffers(buffers [][]byte) (n int, err error) {
	for {
		select {
		case <-s.chSocketWriteError:
			return 0, s.socketWriteError.Load().(error)
		case <-s.die:
			return 0, io.ErrClosedPipe
		default:
		}

		s.mu.Lock()
		waitSnd := s.rudp.WaitSnd()
		if waitSnd < int(s.rudp.snd_wnd) && waitSnd < int(s.rudp.rmt_wnd) {
			for _, buf := range buffers {
				n += len(buf)
				// 把buf放入snd_queue，数据过长则进行切片
				for {
					if len(buf) <= int(s.rudp.mss) {
						s.rudp.Send(buf)
						break
					} else {
						s.rudp.Send(buf[:s.rudp.mss])
						buf = buf[s.rudp.mss:]
					}
				}
			}
			waitSnd = s.rudp.WaitSnd()
			if waitSnd >= int(s.rudp.snd_wnd) || waitSnd >= int(s.rudp.rmt_wnd) || !s.writeDelay {
				s.rudp.flush(false)
				s.uncork()
			}
			s.mu.Unlock()
			atomic.AddUint64(&DefaultSnmp.BytesSent, uint64(n))
			return n, nil
		}

		var timeout *time.Timer
		var c <-chan time.Time
		if !s.writeDdl.IsZero() {
			if time.Now().After(s.writeDdl) {
				s.mu.Unlock()
				return 0, errors.WithStack(errTimeout)
			}
			delay := time.Until(s.writeDdl)
			timeout = time.NewTimer(delay)
			c = timeout.C
		}
		s.mu.Unlock()

		select {
		case <-s.chWriteEvent:
			if timeout != nil {
				timeout.Stop()
			}
		case <-c:
			return 0, errors.WithStack(errTimeout)
		case <-s.chSocketWriteError:
			return 0, s.socketWriteError.Load().(error)
		case <-s.die:
			return 0, io.ErrClosedPipe
		}
	}
}

// Close 关闭连接
func (s *UDPSession) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if once {
		atomic.AddUint64(&DefaultSnmp.CurrEstab, ^uint64(0))
		// 尽量发送队列中所有数据
		s.mu.Lock()
		s.rudp.flush(false)
		s.uncork()
		// 释放等待中的数据包
		s.rudp.ReleaseTX()
		// TODO release FEC
		s.mu.Unlock()

		if s.listener != nil { //监听端的连接
			s.listener.closeSession(s.remote)
			return nil
		} else if s.ownConn {
			// 客户端关闭socket
			return s.conn.Close()
		} else {
			return nil
		}
	} else {
		return errors.WithStack(io.ErrClosedPipe)
	}
}

func (s *UDPSession) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

func (s *UDPSession) RemoteAddr() net.Addr {
	return s.remote
}

func (s *UDPSession) SetDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readDdl = t
	s.writeDdl = t
	s.notifyReadEvent()
	s.notifyWriteEvent()
	return nil
}

func (s *UDPSession) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readDdl = t
	s.notifyReadEvent()
	return nil
}

func (s *UDPSession) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDdl = t
	s.notifyWriteEvent()
	return nil
}

func (s *UDPSession) SetNoDelay(noDelay, interval, resend, nc int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rudp.NoDelay(noDelay, interval, resend, nc)
}

// output RUDP发送包的处理过程
func (s *UDPSession) output(buf []byte) {
	// TODO FEC
	// TODO CRC32
	// TODO 加密

	// TxQueue
	var msg ipv4.Message
	bytes := xmitBuf.Get().([]byte)[:len(buf)]
	copy(bytes, buf)
	msg.Buffers = [][]byte{bytes}
	msg.Addr = s.remote
	s.txqueue = append(s.txqueue, msg)
}

// 数据包输入
func (s *UDPSession) packetInput(data []byte) {
	decrypted := false
	if s.block != nil && len(data) > cryptHeaderSize {
		// TODO decrypt
	} else {
		decrypted = true
	}
	if decrypted && len(data) >= IRUDP_OVERHEAD {
		s.rudpInput(data)
	}
}

func (s *UDPSession) rudpInput(data []byte) {
	// TODO FEC
	var rudpInErrors uint64
	s.mu.Lock()
	if ret := s.rudp.Input(data, true, s.ackNoDelay); ret != 0 {
		rudpInErrors++
	}
	if n := s.rudp.peekSize(); n > 0 {
		s.notifyReadEvent()
	}
	waitSnd := s.rudp.WaitSnd()
	if waitSnd < int(s.rudp.snd_wnd) && waitSnd < int(s.rudp.rmt_wnd) {
		s.notifyWriteEvent()
	}
	s.uncork()
	s.mu.Unlock()
	atomic.AddUint64(&DefaultSnmp.InPkts, 1)
	atomic.AddUint64(&DefaultSnmp.InBytes, uint64(len(data)))

	if rudpInErrors > 0 {
		atomic.AddUint64(&DefaultSnmp.rudpInErrors, rudpInErrors)
	}
}

func (s *UDPSession) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

func (s *UDPSession) notifyWriteEvent() {
	select {
	case s.chWriteEvent <- struct{}{}:
	default:
	}
}

// 如果txqueue有数据则发送
func (s *UDPSession) uncork() {
	if len(s.txqueue) > 0 {
		s.tx(s.txqueue)
		// 回收
		for i := range s.txqueue {
			xmitBuf.Put(s.txqueue[i].Buffers[0])
			s.txqueue[i].Buffers = nil
		}
		s.txqueue = s.txqueue[:0]
	}
}

func (s *UDPSession) notifyWriteError(err error) {
	s.socketWriteErrorOnce.Do(func() {
		s.socketWriteError.Store(err)
		close(s.chSocketWriteError)
	})
}

func (s *UDPSession) notifyReadError(err error) {
	s.socketReadErrorOnce.Do(func() {
		s.socketReadError.Store(err)
		close(s.chSocketReadError)
	})
}

// update rudp协议定时触发,间隔时间:interval
func (s *UDPSession) update() {
	select {
	case <-s.die:
	default:
		s.mu.Lock()
		interval := s.rudp.flush(false)
		waitSnd := s.rudp.WaitSnd()
		// 待发送数据数量小于双方窗口
		if waitSnd < int(s.rudp.snd_wnd) && waitSnd < int(s.rudp.rmt_wnd) {
			s.notifyWriteEvent()
		}
		s.uncork()
		s.mu.Unlock()
		SysTimeSched.Put(s.update, time.Now().Add(time.Duration(interval)*time.Millisecond))
	}
}

func Listen(laddr string) (net.Listener, error) {
	return ListenWithOptions(laddr, nil, 0, 0)
}

func ListenWithOptions(laddr string, block BlockCrypt, dataShards, parityShards int) (*Listener, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", laddr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	conn, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return serveConn(block, dataShards, parityShards, conn, true)
}

// ServeConn 为每个包提供RUDP连接
func ServeConn(block BlockCrypt, dataShards, parityShards int, conn net.PacketConn) (*Listener, error) {
	return serveConn(block, dataShards, parityShards, conn, false)
}

func serveConn(block BlockCrypt, dataShards, parityShards int, conn net.PacketConn, ownConn bool) (*Listener, error) {
	listener := &Listener{
		block:             block,
		dataShards:        dataShards,
		parityShards:      parityShards,
		conn:              conn,
		ownConn:           ownConn,
		sessions:          make(map[string]*UDPSession),
		chAccepts:         make(chan *UDPSession, acceptBacklog),
		chSessionClosed:   make(chan net.Addr),
		die:               make(chan struct{}),
		chSocketReadError: make(chan struct{}),
	}
	go listener.monitor()
	return listener, nil
}

func Dial(raddr string) (net.Conn, error) {
	return DialWithOptions(raddr, nil, 0, 0)
}

func DialWithOptions(raddr string, block BlockCrypt, dataShards, parityShards int) (*UDPSession, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	network := "udp4"
	if udpAddr.IP.To4() == nil {
		network = "udp"
	}

	conn, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var conv uint32
	// 随机生成conv
	binary.Read(rand.Reader, binary.LittleEndian, &conv)
	return newUDPSession(conv, conn, block, dataShards, parityShards, nil, true, udpAddr), nil
}

func newUDPSession(conv uint32, conn net.PacketConn, block BlockCrypt, dataShards, parityShards int, listener *Listener,
	ownConn bool, raddr net.Addr) *UDPSession {
	sess := &UDPSession{
		conn:               conn,
		ownConn:            ownConn,
		listener:           listener,
		block:              block,
		rcvBuf:             make([]byte, mtuLimit),
		remote:             raddr,
		headerSize:         0,
		die:                make(chan struct{}),
		chReadEvent:        make(chan struct{}, 1),
		chWriteEvent:       make(chan struct{}, 1),
		chSocketReadError:  make(chan struct{}),
		chSocketWriteError: make(chan struct{}),
	}

	if _, ok := conn.(*net.UDPConn); ok {
		addr, err := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
		if err == nil {
			if addr.IP.To4() != nil {
				sess.xconn = ipv4.NewPacketConn(conn)
			} else {
				sess.xconn = ipv6.NewPacketConn(conn)
			}
		}
	}

	// TODO FEC init

	if sess.block != nil {
		sess.headerSize += cryptHeaderSize
	}
	// TODO FEC header

	sess.rudp = NewRudp(conv, func(buf []byte, size int) {
		if size >= IRUDP_OVERHEAD+sess.headerSize {
			sess.output(buf[:size])
		}
	})
	sess.rudp.ReserveBytes(sess.headerSize)

	if sess.listener == nil { // 客户端连接
		go sess.readLoop()
		atomic.AddUint64(&DefaultSnmp.ActiveOpens, 1)
	} else {
		atomic.AddUint64(&DefaultSnmp.PassiveOpens, 1)
	}

	SysTimeSched.Put(sess.update, time.Now())

	currestab := atomic.AddUint64(&DefaultSnmp.CurrEstab, 1)
	maxconn := atomic.LoadUint64(&DefaultSnmp.MaxConn)
	if currestab > maxconn {
		atomic.CompareAndSwapUint64(&DefaultSnmp.MaxConn, maxconn, currestab)
	}

	return sess
}

// GetConv gets conversation id of a session
func (s *UDPSession) GetConv() uint32 { return s.rudp.conv }

// GetRTO gets current rto of the session
func (s *UDPSession) GetRTO() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rudp.rx_rto
}

// GetSRTT gets current srtt of the session
func (s *UDPSession) GetSRTT() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rudp.rx_srtt
}

// GetRTTVar gets current rtt variance of the session
func (s *UDPSession) GetSRTTVar() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rudp.rx_rttvar
}

func NewConn3(convid uint32, raddr net.Addr, block BlockCrypt, dataShards, parityShards int, conn net.PacketConn) (*UDPSession, error) {
	return newUDPSession(convid, conn, block, dataShards, parityShards, nil, false, raddr), nil
}

func NewConn2(raddr net.Addr, block BlockCrypt, dataShards, parityShards int, conn net.PacketConn) (*UDPSession, error) {
	var convid uint32
	binary.Read(rand.Reader, binary.LittleEndian, &convid)
	return NewConn3(convid, raddr, block, dataShards, parityShards, conn)
}

func NewConn(raddr string, block BlockCrypt, dataShards, parityShards int, conn net.PacketConn) (*UDPSession, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", raddr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return NewConn2(udpaddr, block, dataShards, parityShards, conn)
}

// SetWriteDelay delays write for bulk transfer until the next update interval
func (s *UDPSession) SetWriteDelay(delay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeDelay = delay
}

// SetWindowSize set maximum window size
func (s *UDPSession) SetWindowSize(sndwnd, rcvwnd int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rudp.WndSize(sndwnd, rcvwnd)
}

// SetMtu sets the maximum transmission unit(not including UDP header)
func (s *UDPSession) SetMtu(mtu int) bool {
	if mtu > mtuLimit {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rudp.SetMtu(mtu)
	return true
}

// SetStreamMode toggles the stream mode on/off
func (s *UDPSession) SetStreamMode(enable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enable {
		s.rudp.stream = 1
	} else {
		s.rudp.stream = 0
	}
}

// SetACKNoDelay changes ack flush option, set true to flush ack immediately,
func (s *UDPSession) SetACKNoDelay(nodelay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackNoDelay = nodelay
}

// SetDSCP sets the 6bit DSCP field in IPv4 header, or 8bit Traffic Class in IPv6 header.
//
// if the underlying connection has implemented `func SetDSCP(int) error`, SetDSCP() will invoke
// this function instead.
//
// It has no effect if it's accepted from Listener.
func (s *UDPSession) SetDSCP(dscp int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errInvalidOperation
	}

	// interface enabled
	if ts, ok := s.conn.(setDSCP); ok {
		return ts.SetDSCP(dscp)
	}

	if nc, ok := s.conn.(net.Conn); ok {
		var succeed bool
		if err := ipv4.NewConn(nc).SetTOS(dscp << 2); err == nil {
			succeed = true
		}
		if err := ipv6.NewConn(nc).SetTrafficClass(dscp); err == nil {
			succeed = true
		}

		if succeed {
			return nil
		}
	}
	return errInvalidOperation
}

// SetReadBuffer sets the socket read buffer, no effect if it's accepted from Listener
func (s *UDPSession) SetReadBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		if nc, ok := s.conn.(setReadBuffer); ok {
			return nc.SetReadBuffer(bytes)
		}
	}
	return errInvalidOperation
}

// SetWriteBuffer sets the socket write buffer, no effect if it's accepted from Listener
func (s *UDPSession) SetWriteBuffer(bytes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		if nc, ok := s.conn.(setWriteBuffer); ok {
			return nc.SetWriteBuffer(bytes)
		}
	}
	return errInvalidOperation
}
