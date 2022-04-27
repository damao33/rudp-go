package rudp

import (
	"github.com/pkg/errors"
	"golang.org/x/net/ipv4"
	"sync/atomic"
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
	atomic.AddUint64(&DefaultSnmp.OutPkts, uint64(byteCount))
	atomic.AddUint64(&DefaultSnmp.OutBytes, uint64(pkgCount))
}
