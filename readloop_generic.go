//go:build !linux

package rudp

func (s *UDPSession) readLoop() {
	s.defaultReadLoop()
}

func (l *Listener) monitor() {
	l.defaultMonitor()
}
