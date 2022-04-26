package rudp

import (
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

type (
	// Listener 等待监听所有传入的连接
	Listener struct {
		block        BlockCrypt
		dataShards   int
		parityShards int
		conn         net.PacketConn // net#PacketConn 是基于网络包的连接接口，net#Conn基于流式
		ownConn      bool           // true ：内部创建连接； false ：调用者提供连接

		sessions        map[string]*UDPSession
		sessionLock     sync.RWMutex
		chAccepts       chan *UDPSession
		chSessionClosed chan net.Addr // 会话关闭队列

		die     chan struct{} // Listener的关闭通知
		dieOnce sync.Once

		// socket错误处理
		socketReadError     atomic.Value
		chSocketReadError   chan struct{}
		socketReadErrorOnce sync.Once

		readDdl atomic.Value // Accept的readDdl
	}
)

func (l *Listener) Accept() (net.Conn, error) {
	return l.AcceptRUDP()
}

// Close 停止监听连接并关闭连接
func (l *Listener) Close() error {
	var once bool
	l.dieOnce.Do(func() {
		close(l.die)
		once = true
	})

	if once {
		if l.ownConn {
			return l.conn.Close()
		}
	} else {
		return errors.WithStack(io.ErrClosedPipe)
	}
	return nil
}

func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

func (l *Listener) packetInput(data []byte, from net.Addr) {
	decrypted := false
	if l.block != nil && len(data) >= cryptHeaderSize {
		// TODO crypt
	} else if l.block == nil {
		decrypted = true
	}

	if decrypted && len(data) >= IRUDP_OVERHEAD {
		l.sessionLock.RLock()
		s, ok := l.sessions[from.String()]
		l.sessionLock.RUnlock()

		var conv, sn uint32
		convRecovered := false
		// TODO FEC
		conv = binary.LittleEndian.Uint32(data[:4])
		sn = binary.LittleEndian.Uint32(data[IRUDP_SN_OFFSET:])
		convRecovered = true

		// 连接存在
		if ok {
			// FEC校验数据或有效会话
			if !convRecovered || conv == s.rudp.conv {
				s.rudpInput(data)
			} else if sn == 0 {
				// 替代现有连接
				s.Close()
				s = nil
			}
		}

		if s == nil && convRecovered {
			// 监听端还能继续监听连接
			if len(l.chAccepts) < cap(l.chAccepts) {
				s = newUDPSession(conv, l.conn, l.block, l.dataShards, l.parityShards, l, false, from)
				s.rudpInput(data)
				l.sessionLock.Lock()
				l.sessions[from.String()] = s
				l.sessionLock.Unlock()
				l.chAccepts <- s
			}
		}
	}
}

// closeSession 通知监听端一个会话关闭了
func (l *Listener) closeSession(remote net.Addr) bool {
	l.sessionLock.Lock()
	defer l.sessionLock.Unlock()
	if _, ok := l.sessions[remote.String()]; ok {
		delete(l.sessions, remote.String())
		return true
	} else {
		return false
	}
}

func (l *Listener) notifyReadError(err error) {
	l.socketReadErrorOnce.Do(func() {
		l.socketReadError.Store(err)
		close(l.chSocketReadError)

		// 把ReadError通知给所有会话
		l.sessionLock.RLock()
		for _, s := range l.sessions {
			s.notifyReadError(err)
		}
		l.sessionLock.RUnlock()
	})
}

func (l *Listener) AcceptRUDP() (*UDPSession, error) {
	var timeout <-chan time.Time
	if readDdl, ok := l.readDdl.Load().(time.Time); ok && !readDdl.IsZero() {
		timeout = time.After(time.Until(readDdl))
	}
	select {
	case <-timeout:
		return nil, errors.WithStack(errTimeout)
	case s := <-l.chAccepts:
		return s, nil
	case <-l.chSocketReadError:
		return nil, l.socketReadError.Load().(error)
	case <-l.die:
		return nil, errors.WithStack(io.ErrClosedPipe)
	}
}

// SetDSCP sets the 6bit DSCP field in IPv4 header, or 8bit Traffic Class in IPv6 header.
//
// if the underlying connection has implemented `func SetDSCP(int) error`, SetDSCP() will invoke
// this function instead.
func (l *Listener) SetDSCP(dscp int) error {
	// interface enabled
	if ts, ok := l.conn.(setDSCP); ok {
		return ts.SetDSCP(dscp)
	}

	if nc, ok := l.conn.(net.Conn); ok {
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

// SetReadBuffer sets the socket read buffer for the Listener
func (l *Listener) SetReadBuffer(bytes int) error {
	if nc, ok := l.conn.(setReadBuffer); ok {
		return nc.SetReadBuffer(bytes)
	}
	return errInvalidOperation
}

// SetWriteBuffer sets the socket write buffer for the Listener
func (l *Listener) SetWriteBuffer(bytes int) error {
	if nc, ok := l.conn.(setWriteBuffer); ok {
		return nc.SetWriteBuffer(bytes)
	}
	return errInvalidOperation
}
