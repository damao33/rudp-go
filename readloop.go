package rudp

import "github.com/pkg/errors"

func (s *UDPSession) defaultReadLoop() {
	buf := make([]byte, mtuLimit)
	src := ""
	for {
		if n, addr, err := s.conn.ReadFrom(buf); err == nil {
			if src == "" {
				src = addr.String()
			} else if src != addr.String() {
				//TODO snmp
				continue
			}
			s.packetInput(buf[:n])
		} else {
			s.notifyReadError(errors.WithStack(err))
			return
		}
	}
}

func (l *Listener) defaultMonitor() {
	buf := make([]byte, mtuLimit)
	for {
		if n, from, err := l.conn.ReadFrom(buf); err == nil {
			l.packetInput(buf[:n], from)
		} else {
			l.notifyReadError(errors.WithStack(err))
			return
		}
	}
}
