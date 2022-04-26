package rudp

import (
	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
)

func (s *UDPSession) defaultTx(txqueue []ipv4.Message) {
	byteCount := 0
	pkgCount := 0
	for i := range txqueue {
		if n, err := s.conn.WriteTo(s.txqueue[i].Buffers[0], s.txqueue[i].Addr); err == nil {
			byteCount += n
			pkgCount++
		} else {
			s.notifyWriteError(errors.WithStack(err))
			break
		}
	}
	// TODO SNMP
}
