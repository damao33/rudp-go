package rudp

import (
	"encoding/binary"
	"sync/atomic"
	"time"
)

const (
	IRUDP_RTO_NDL     = 30  // no delay min rto
	IRUDP_RTO_MIN     = 100 // normal min rto
	IRUDP_RTO_DEF     = 200
	IRUDP_RTO_MAX     = 60000
	IRUDP_CMD_PUSH    = 81 // cmd: push data 具体的数据包
	IRUDP_CMD_ACK     = 82 // cmd: ack 通知包，通知对端收到那些数据包
	IRUDP_CMD_WASK    = 83 // cmd: window probe (ask) 探测包，探测对端窗口大小
	IRUDP_CMD_WINS    = 84 // cmd: window size (tell) 告诉对端窗口大小
	IRUDP_ASK_SEND    = 1  // probe: need to send IRUDP_CMD_WASK
	IRUDP_ASK_TELL    = 2  // probe: need to send IRUDP_CMD_WINS
	IRUDP_WND_SND     = 32
	IRUDP_WND_RCV     = 32
	IRUDP_MTU_DEF     = 1400
	IRUDP_ACK_FAST    = 3
	IRUDP_INTERVAL    = 100 //flush刷新间隔，对系统循环效率有非常重要影响
	IRUDP_OVERHEAD    = 24
	IRUDP_DEADLINK    = 20
	IRUDP_THRESH_INIT = 2
	IRUDP_THRESH_MIN  = 2
	IRUDP_PROBE_INIT  = 7000 // 7 secs to probe window size
	IRUDP_PROBE_MIN   = 7000
	IRUDP_PROBE_LIMIT = 120000 // up to 120 secs to probe window
	IRUDP_SN_OFFSET   = 12
)

// RUDP 报文定义
// 0               4   5   6       8 (BYTE)
// +---------------+---+---+-------+
// |     conv      |cmd|frg|  wnd  |
// +---------------+---+---+-------+   8
// |     ts        |     sn        |
// +---------------+---------------+  16
// |     una       |     len       |
// +---------------+---------------+  24
// |                               |
// |        DATA (optional)        |
// |                               |
// +-------------------------------+
type segment struct {
	// 会话编号，两方一致才能通信
	conv uint32
	// 指令类型，可以同时有多个指令通过与操作设置进来
	cmd uint8
	// 分片编号，表示倒数第几个分片
	frg uint8
	// 本方剩余接收窗口大小。即接收窗口大小-接收队列大小
	wnd uint16
	// 当前毫秒时间戳
	ts uint32
	// 确认序列号
	sn uint32
	// 代表编号前面的所有报都收到了的标志
	una uint32
	rto uint32
	// 重传次数
	xmit uint32
	// 重传时间戳，超过这个时间重发这个包
	resendts uint32
	// 快速应答数量，记录被跳过的次数，统计在这个封包的序列号之前有多少报已经应答了。
	// 比如1，2，3三个封包，收到2的时候知道1被跳过了，此时1的fastack加一，收到3的时候继续加一，超过一定阈值直接重传1这个封包。
	// 该阈值由noDelay函数设置，默认为0
	fastack uint32
	// 标记本报文是否已响应
	acked uint32
	// 数据
	data []byte
}

// RUDP 结构体
type RUDP struct {
	// mss：Maximum Segment Size，最大报文长度
	// mtu：Maximum Transmission Unit，最大传输单元
	conv, mtu, mss, state uint32
	// sndUna：最小的未ack序列号，即这个编号前面的所有报都收到了的标志
	// sndNxt：下一个待发送的序列号
	// rcvNxt：下一个待接收的序列号，会通过包头中的una字段通知对端
	sndUna, sndNxt, rcvNxt uint32
	// ssthresh：slow start threshold，慢启动阈值
	ssthresh uint32
	// RTT：Round Trip Time，往返时间
	// rxRTTVal：最近四次RTT的平均值
	// rxSRTT：RTT的一个加权RTT平均值，平滑值
	rxRTTVal, rxSRTT int32
	// rxRTO：估算后的rto
	// rxMinRTO：最小rto，系统启动时配置
	rxRTO, rxMinRTO uint32
	// sndWnd：发送的窗口大小
	// rcvWnd：接收的窗口大小rcv_wnd
	// rmtWnd：远端（rmt=remote）拥塞窗口大小
	// cwnd： 拥塞窗口大小
	// probe：存储探测标志位，Irudp_ASK_TELL表示告知远端窗口大小，Irudp_ASK_SEND表示请求远端告知窗口大小
	sndWnd, rcvWnd, rmtWnd, cwnd, probe uint32
	// interval：内部flush刷新间隔
	// tsFlush：下次flush刷新时间戳
	interval, tsFlush uint32
	// noDelay: 0 不启用，1启用快速重传模式；
	// updated：是否调用过update函数
	noDelay, updated uint32
	// tsProbe：探查窗口的时间戳
	// probeWait：探查窗口需要等待的时间
	tsProbe, probeWait uint32
	// deadLink： 最大重传次数，被认为连接中断（丢失次数）
	// incr：可发送最大数据量
	deadLink, incr uint32
	// 快速重传：触发快速重传的ack数
	fastResend int32
	// noCwnd：是否关闭流控，0表示不关闭，默认值为0
	// stream：是否启用流传输模式，0消息模式，1流模式
	noCwnd, stream int32

	// 接收和发送队列
	sndQueue []segment
	rcvQueue []segment
	// 接收和发送缓冲区
	sndBuf []segment
	rcvBuf []segment

	// ack列表
	ackList []ackItem

	// mtu缓冲区
	buffer []byte
	// 缓冲区保留多少buff
	reserved int
	// 回调
	output outputCallback
}

type ackItem struct {
	sn uint32
	ts uint32
}
type outputCallback func(buf []byte, size int)

func NewRudp(conv uint32, output outputCallback) *RUDP {
	rudp := new(RUDP)
	rudp.conv = conv
	rudp.mtu = IRUDP_MTU_DEF
	rudp.mss = rudp.mtu - IRUDP_OVERHEAD
	rudp.sndWnd = IRUDP_WND_SND
	rudp.rcvWnd = IRUDP_WND_RCV
	rudp.rmtWnd = IRUDP_WND_RCV
	rudp.ssthresh = IRUDP_THRESH_INIT
	rudp.rxRTO = IRUDP_RTO_DEF
	rudp.rxMinRTO = IRUDP_RTO_MIN
	rudp.interval = IRUDP_INTERVAL
	rudp.tsFlush = IRUDP_INTERVAL
	rudp.deadLink = IRUDP_DEADLINK
	rudp.buffer = make([]byte, rudp.mtu)
	rudp.output = output
	return rudp
}

func (rudp *RUDP) newSegment(size int) (seg segment) {
	seg.data = xmitBuf.Get().([]byte)[:size]
	return
}

// delSegment 回收seg
func (rudp *RUDP) delSegment(seg *segment) {
	if seg.data != nil {
		xmitBuf.Put(seg.data)
		seg = nil
	}
}

// Send 把buffer中的数据转化成分片，添加到send_queue末尾
func (rudp *RUDP) Send(buffer []byte) int {
	if len(buffer) == 0 {
		return -1
	}

	// 在流式模式下将buffer填充到最后一个分片
	if rudp.stream != 0 {
		n := len(rudp.sndQueue)
		if n > 0 {
			// 取出最后一个分片，计算填充空间
			lastSegment := &rudp.sndQueue[n-1]
			capacity := (int(rudp.mss)) - len(lastSegment.data)
			extend := capacity
			if len(buffer) < capacity {
				extend = len(buffer)
			}

			// 填充
			lastSegment.data = append(lastSegment.data, buffer[:extend]...)
			buffer = buffer[extend:]
		}
		if len(buffer) == 0 {
			return 0
		}
	}

	var count int
	// 计算新分片数量
	if len(buffer) < int(rudp.mss) {
		count = 1
	} else {
		count = (len(buffer) + int(rudp.mss) - 1) / int(rudp.mss)
	}

	// 判断接收窗口
	if count > 255 {
		return -2
	}
	if count == 0 {
		return 1
	}

	for i := 0; i < count; i++ {
		var size int
		if len(buffer) > int(rudp.mss) {
			size = int(rudp.mss)
		} else {
			size = len(buffer)
		}
		seg := rudp.newSegment(size)
		copy(seg.data, buffer[:size])
		buffer = buffer[size:]
		// 如果是流模式则其分片编号为0，否则为相应数量编号
		if rudp.stream == 0 {
			seg.frg = uint8(count - i - 1)
		} else {
			seg.frg = 0
		}
		//seg.frg = uint8(count - i - 1)
		rudp.sndQueue = append(rudp.sndQueue, seg)
	}
	return 0
}

//
// Input
// @Description: 从rcvBuf处理接收到的数据，添加到rcvQueue
// @receiver rudp
// @param data
// @param regular
// @param ackNoDelay
// @return int 正确则返回0
//
func (rudp *RUDP) Input(data []byte, regular, ackNoDelay bool) int {
	if len(data) < IRUDP_OVERHEAD {
		return -1
	}
	oldSndUna := rudp.sndUna
	var flag int
	var latest uint32
	var inSegs uint64
	var windowSlide bool
	for {
		if len(data) < IRUDP_OVERHEAD {
			break
		}
		var conv, ts, sn, una, length uint32
		var wnd uint16
		var cmd, frg uint8

		data = decodeUint32(data, &conv)
		if conv != rudp.conv {
			return -1
		}
		data = decodeUint8(data, &cmd)
		data = decodeUint8(data, &frg)
		data = decodeUint16(data, &wnd)
		data = decodeUint32(data, &ts)
		data = decodeUint32(data, &sn)
		data = decodeUint32(data, &una)
		data = decodeUint32(data, &length)
		if len(data) < int(length) {
			return -2
		}

		// cmd 不合法
		if cmd != IRUDP_CMD_PUSH && cmd != IRUDP_CMD_ACK && cmd != IRUDP_CMD_WASK && cmd != IRUDP_CMD_WINS {
			return -3
		}

		if regular {
			rudp.rmtWnd = uint32(wnd)
		}
		if rudp.parseUna(una) > 0 {
			windowSlide = true
		}
		rudp.updateSndUna()

		switch cmd {
		case IRUDP_CMD_PUSH:
			repeat := true
			// when rcvNxt updated : Receive() parseData()
			if sn < rudp.rcvNxt+rudp.rcvWnd {
				rudp.pushAck(sn, ts)
				if sn >= rudp.rcvNxt {
					seg := segment{
						conv: conv,
						cmd:  cmd,
						frg:  frg,
						wnd:  wnd,
						ts:   ts,
						sn:   sn,
						una:  una,
						data: data[:length],
					}
					// 将该分片添加到rcvBuf中，可能会添加到rcvQueue中
					repeat = rudp.parseData(seg)
				}
			}
			if regular && repeat {
				atomic.AddUint64(&DefaultSnmp.RepeatSegs, 1)
			}
		case IRUDP_CMD_ACK:
			rudp.parseAck(sn)
			rudp.parseFastAck(sn, ts)
			flag |= 1
			latest = ts
		case IRUDP_CMD_WASK:
			// 窗口探测包，
			// 下一次发送数据时带上窗口大小通知remote端
			rudp.probe |= IRUDP_ASK_TELL
		case IRUDP_CMD_WINS:
			// 如果是告诉我远端窗口大小，什么也不做
		default:
			return -3
		}
		inSegs++
		data = data[length:]
	}
	atomic.AddUint64(&DefaultSnmp.InSegs, inSegs)

	// 根据新的rtt更新rto
	if flag != 0 && regular {
		current := currentMs()
		rtt := timeDiff(current, latest)
		if rtt >= 0 {
			rudp.updateRTO(rtt)
		}
	}

	// 当启动拥塞控制时，更新拥塞窗口cwnd
	if rudp.noCwnd == 0 {
		// 接收到新ack
		if rudp.sndUna > oldSndUna {
			if rudp.cwnd < rudp.rmtWnd {
				mss := rudp.mss
				// 慢启动
				if rudp.cwnd < rudp.ssthresh {
					rudp.cwnd++
					rudp.incr += mss
				} else {
					// 最小边界
					if rudp.incr < rudp.mss {
						rudp.incr = rudp.mss
					}
					// 拥塞避免, see : https://luyuhuang.tech/2020/12/09/rudp.html
					rudp.incr += (mss*mss)/rudp.incr + (mss / 16)
					// 当 incr 累计增加的值超过一个 mss 时, cwnd 增加 1
					if (rudp.cwnd+1)*mss <= rudp.incr {
						rudp.cwnd++
						// TODO : 判断 mss>0 mss<0 ??
					}
				}
				if rudp.cwnd > rudp.rmtWnd {
					rudp.cwnd = rudp.rmtWnd
					rudp.incr = rudp.cwnd * mss
				}
			}
		}
	}

	if windowSlide {
		rudp.flush(false)
	} else if ackNoDelay && len(rudp.ackList) > 0 {
		rudp.flush(true)
	}
	return 0
}

//
// Receive
// @Description: 从rcvQueue复制数据到buffer中，且从rcvBuf中移动分片数据到rcvQueue中
// @receiver rudp
// @param buffer
// @return n 读取到的字节数，-1：没有可读数据，-2：buffer长度小于peekSize
//
func (rudp *RUDP) Receive(buffer []byte) (n int) {
	peekSize := rudp.peekSize()
	if peekSize < 0 {
		return -1
	}

	if peekSize > len(buffer) {
		return -2
	}

	fastRecover := false
	if len(rudp.rcvQueue) >= int(rudp.rcvWnd) {
		fastRecover = true
	}

	// 合并分片
	count := 0
	for i := range rudp.rcvQueue {
		seg := &rudp.rcvQueue[i]
		copy(buffer, seg.data)
		buffer = buffer[len(seg.data):]
		n += len(seg.data)
		count++
		rudp.delSegment(seg)
		if seg.frg == 0 {
			break
		}
	}
	if count > 0 {
		rudp.rcvQueue = rudp.removeFront(rudp.rcvQueue, count)
	}

	// 将rcvBuf数据转移到rcvQueue
	count = 0
	for i := range rudp.rcvBuf {
		seg := &rudp.rcvBuf[i]
		if seg.sn == rudp.rcvNxt && len(rudp.rcvQueue)+count < int(rudp.rcvWnd) {
			rudp.rcvNxt++
			count++
		} else {
			break
		}
	}
	if count > 0 {
		rudp.rcvQueue = append(rudp.rcvQueue, rudp.rcvBuf[:count]...)
		rudp.rcvBuf = rudp.removeFront(rudp.rcvBuf, count)
	}

	// 快速恢复
	if len(rudp.rcvQueue) < int(rudp.rcvWnd) && fastRecover {
		// 通知远端窗口大小
		rudp.probe |= IRUDP_ASK_TELL
	}
	return
}

// parseUna 将sndBuf中sn小于una的包删除
func (rudp *RUDP) parseUna(una uint32) int {
	removeCount := 0
	for i := range rudp.sndBuf {
		seg := &rudp.sndBuf[i]
		if una > seg.sn {
			rudp.delSegment(seg)
			removeCount++
		} else {
			break
		}
	}
	if removeCount > 0 {
		rudp.sndBuf = rudp.removeFront(rudp.sndBuf, removeCount)
	}
	return removeCount
}

// removeFront 删除buf前removeCount个元素
// TODO 效率
func (rudp *RUDP) removeFront(buf []segment, removeCount int) []segment {
	if removeCount <= cap(buf)/2 {
		return buf[removeCount:]
	} else {
		newLen := copy(buf, buf[removeCount:])
		return buf[:newLen]
	}
}

// updateUna 根据sndBuf更新sndUna
func (rudp *RUDP) updateSndUna() {
	if len(rudp.sndBuf) > 0 {
		seg := rudp.sndBuf[0]
		rudp.sndUna = seg.sn
	} else {
		rudp.sndUna = rudp.sndNxt
	}
}

/**
 * rxSRTT: smoothed round trip time，平滑后的RTT
 * rxRTTVal：RTT的变化量，代表连接的抖动情况
 * interval：内部flush刷新间隔，对系统循环效率有非常重要影响
 *
 * 该函数主要意图在于更新与ack有关的RTO时间
 * 	RTO相关：通过请求应答时间（RTT）计算出超时重传时间（RTO）
 */
// updateRTO 通过rtt更新rto
func (rudp *RUDP) updateRTO(rtt int32) {
	// https://tools.ietf.org/html/rfc6298
	var rto uint32
	if rudp.rxRTTVal == 0 {
		rudp.rxSRTT = rtt
		rudp.rxRTTVal = rtt >> 1
	} else {
		// 平滑抖动算法
		delta := rudp.rxSRTT - rtt
		// 取delta绝对值
		if delta < 0 {
			delta = -delta
		}

		rudp.rxRTTVal = (3*rudp.rxRTTVal + delta) / 4
		rudp.rxSRTT = (7*rudp.rxSRTT + rtt) / 8
		if rudp.rxSRTT < 1 {
			rudp.rxSRTT = 1
		}
	}
	// 通过抖动情况与内部调度间隔计算出RTO时间
	rto = uint32(rudp.rxSRTT) + max(rudp.interval, uint32(rudp.rxRTTVal)<<2)
	// 使得最后结果在minrto <= x <=  RUDP_RTO_MAX 之间
	rudp.rxRTO = bound(rudp.rxMinRTO, rto, IRUDP_RTO_MAX)
}

//
// parseAck 解析ack
// @Description: 从snd_buf删除对应编号分片
// @receiver rudp
// @param sn
//
func (rudp *RUDP) parseAck(sn uint32) {
	// 当前确认数据包ack的编号小于已经接收到的编号(una)或数据包的ack编号大于待分配的编号则不合法
	if sn < rudp.sndUna || sn > rudp.sndNxt {
		return
	}
	// 遍历snd_buf释放该编号分片
	for i := range rudp.sndBuf {
		seg := &rudp.sndBuf[i]
		if sn == seg.sn {
			seg.acked = 1
			rudp.delSegment(seg)
			break
		}
		if sn < seg.sn {
			break
		}
	}
}

func (rudp *RUDP) parseFastAck(sn, ts uint32) {
	// 当前确认数据包ack的编号小于已经接收到的编号(una)或数据包的ack编号大于待分配的编号则不合法
	if sn < rudp.sndUna || sn > rudp.sndNxt {
		return
	}
	for i := range rudp.sndBuf {
		seg := &rudp.sndBuf[i]
		if sn < seg.sn {
			break
		} else if sn != seg.sn && seg.ts <= ts {
			seg.fastack++
		}
	}
}

//
// pushAck
// @Description: 添加该数据分片编号的ack确认包进acklist中
// @receiver rudp
// @param sn
// @param ts
//
func (rudp *RUDP) pushAck(sn, ts uint32) {
	rudp.ackList = append(rudp.ackList, ackItem{sn, ts})
}

//
// parseData 解析数据
// @Description: 将该分片添加到rcv_buf中，可能会添加到rcv_queue中
// @receiver rudp
// @param seg
// @return bool  true：如果seg重复了
//
func (rudp *RUDP) parseData(seg segment) bool {
	if seg.sn >= rudp.rcvNxt+rudp.rcvWnd || seg.sn < rudp.rcvNxt {
		return true
	}

	// 判断该包是否重复
	repeat := false
	insertIndex := 0
	for i := len(rudp.rcvBuf) - 1; i >= 0; i-- {
		s := rudp.rcvBuf[i]
		if s.sn == seg.sn {
			repeat = true
			break
		}
		if seg.sn > s.sn {
			insertIndex = i + 1
			break
		}
	}

	if !repeat {
		// TODO IMPORTANT: why replicate the data?
		dataCopy := xmitBuf.Get().([]byte)[:len(seg.data)]
		copy(dataCopy, seg.data)
		seg.data = dataCopy

		// 把新数据添加到rcvBuf对应位置
		rudp.rcvBuf = append(rudp.rcvBuf[:insertIndex], append([]segment{seg}, rudp.rcvBuf[insertIndex:]...)...)
	}

	// 把可用数据从rcvBuf转移到rcvQueue
	count := 0
	for i := range rudp.rcvBuf {
		s := &rudp.rcvBuf[i]
		// 确保rcvQueue中数据有序
		if s.sn == rudp.rcvNxt && len(rudp.rcvQueue)+count < int(rudp.rcvWnd) {
			rudp.rcvNxt++
			count++
		} else {
			break
		}
	}
	if count > 0 {
		rudp.rcvQueue = append(rudp.rcvQueue, rudp.rcvBuf[:count]...)
		rudp.rcvBuf = rudp.removeFront(rudp.rcvBuf, count)
	}
	return repeat
}

//
// flush
// @Description: 发送数据、更新状态
// @receiver rudp
// @param ackOnly true:只遍历发送ack
// @return uint32
//
func (rudp *RUDP) flush(ackOnly bool) uint32 {
	// 处理IRUDP_CMD_ACK
	var seg segment
	seg.conv = rudp.conv
	seg.cmd = IRUDP_CMD_ACK
	seg.wnd = rudp.unusedWnd()
	seg.una = rudp.rcvNxt
	buffer := rudp.buffer
	ptr := buffer[rudp.reserved:]

	// 确保buffer中有足够的空间
	makeSpace := func(space int) {
		size := len(buffer) - len(ptr)
		if size+space > int(rudp.mtu) {
			rudp.output(buffer, size)
			ptr = buffer[rudp.reserved:]
		}
	}
	// 发送buffer中剩余的字节
	flushBuffer := func() {
		size := len(buffer) - len(ptr)
		if size > rudp.reserved {
			rudp.output(buffer, size)
		}
	}

	// 将ack分片添加到buffer中
	for i, ack := range rudp.ackList {
		makeSpace(IRUDP_OVERHEAD)
		if ack.sn >= rudp.rcvNxt || len(rudp.ackList)-1 == i {
			seg.sn, seg.ts = ack.sn, ack.ts
			// 把seg打包到buffer中
			ptr = seg.encodeOverHead(ptr)
		}
	}
	rudp.ackList = rudp.ackList[:0]
	// 发送剩余ack段
	if ackOnly {
		flushBuffer()
		return rudp.interval
	}

	// 如果远端窗口为0需要探测
	if rudp.rmtWnd == 0 {
		currentTime := currentMs()
		// 初始化探测间隔和探测时间戳
		if rudp.probeWait == 0 {
			rudp.probeWait = IRUDP_PROBE_INIT
			rudp.tsProbe = currentTime + rudp.probeWait
		} else {
			if timeDiff(currentTime, rudp.tsProbe) >= 0 {
				if rudp.probeWait < IRUDP_PROBE_MIN {
					rudp.probeWait = IRUDP_PROBE_MIN
				}
				rudp.probeWait += rudp.probeWait / 2
				rudp.probeWait = min(IRUDP_PROBE_LIMIT, rudp.probeWait)
				rudp.tsProbe = currentTime + rudp.probeWait
				rudp.probe |= IRUDP_ASK_SEND
			}
		}
	} else {
		rudp.probeWait = 0
		rudp.tsProbe = 0
	}
	// 处理IRUDP_ASK_SEND
	// 检查是否需要发送窗口探测报文
	if (rudp.probe & IRUDP_ASK_SEND) != 0 {
		seg.cmd = IRUDP_CMD_WASK
		makeSpace(IRUDP_OVERHEAD)
		ptr = seg.encodeOverHead(ptr)
	}
	// 处理IRUDP_ASK_TELL
	// 检查是否需要发送窗口通知报文
	if (rudp.probe & IRUDP_ASK_TELL) != 0 {
		seg.cmd = IRUDP_CMD_WINS
		makeSpace(IRUDP_OVERHEAD)
		ptr = seg.encodeOverHead(ptr)
	}
	rudp.probe = 0

	// 取发送窗口和远端窗口最小值得到拥塞窗口大小
	cwnd := min(rudp.sndWnd, rudp.rmtWnd)
	// 如果设置了 noCwnd, 则 cwnd 只取决于 sndWnd 和 rmtWnd
	if rudp.noCwnd == 0 {
		cwnd = min(rudp.cwnd, cwnd)
	}

	// 处理IRUDP_CMD_PUSH
	// 流量控制滑动窗口，把sndQueue的数据转移到sndBuf
	newSegCount := 0
	for i := range rudp.sndQueue {
		if rudp.sndNxt >= rudp.sndUna+cwnd {
			break
		}
		newSeg := rudp.sndQueue[i]
		newSeg.conv = rudp.conv
		newSeg.cmd = IRUDP_CMD_PUSH
		newSeg.sn = rudp.sndNxt
		rudp.sndBuf = append(rudp.sndBuf, newSeg)
		rudp.sndNxt++
		newSegCount++
	}
	if newSegCount > 0 {
		rudp.sndQueue = rudp.removeFront(rudp.sndQueue, newSegCount)
	}

	// 计算resent
	resent := uint32(rudp.fastResend)
	// resent为0不执行快速重传
	if rudp.fastResend <= 0 {
		resent = 0xffffffff
	}
	minRto := int32(rudp.interval)
	current := currentMs()
	var change, fastRetransSegs, earlyRetransSegs, lostSegs uint64
	ref := rudp.sndBuf[:len(rudp.sndBuf)]
	for i := range ref {
		s := &rudp.sndBuf[i]
		needSend := false
		if s.acked == 1 {
			continue
		}
		// 该分片首次发送
		if s.xmit == 0 {
			needSend = true
			s.rto = rudp.rxRTO
			s.resendts = current + s.rto
		} else if s.fastack >= resent {
			// 快速重传
			needSend = true
			s.fastack = 0
			s.rto = rudp.rxRTO
			s.resendts = current + s.rto
			change++
			fastRetransSegs++
		} else if s.fastack > 0 && newSegCount == 0 {
			// 早期重传
			needSend = true
			s.fastack = 0
			s.rto = rudp.rxRTO
			s.resendts = current + s.rto
			change++
			earlyRetransSegs++
		} else if timeDiff(current, s.resendts) >= 0 {
			// 超时重传
			needSend = true
			if rudp.noDelay == 0 {
				s.rto += rudp.rxRTO
			} else {
				s.rto += rudp.rxRTO / 2
			}
			s.fastack = 0
			s.resendts = current + s.rto
			lostSegs++
		}

		if needSend {
			current = currentMs()
			s.xmit++
			s.ts = current
			s.wnd = seg.wnd
			s.una = seg.una

			needSpace := IRUDP_OVERHEAD + len(s.data)
			makeSpace(needSpace)
			ptr = s.encodeOverHead(ptr)
			ptr = s.encodeData(ptr)
			//判断该分片重传次数是否大于最大重传次数
			if s.xmit >= rudp.deadLink {
				// 断开连接
				rudp.state = 0xffffffff
			}
		}
		// 获得最近rto
		if rto := timeDiff(s.resendts, current); rto > 0 && rto < minRto {
			minRto = rto
		}
	}
	// 发送剩余数据
	flushBuffer()

	sum := lostSegs
	if lostSegs > 0 {
		atomic.AddUint64(&DefaultSnmp.LostSegs, lostSegs)
	}
	if fastRetransSegs > 0 {
		atomic.AddUint64(&DefaultSnmp.FastRetransSegs, fastRetransSegs)
		sum += fastRetransSegs
	}
	if earlyRetransSegs > 0 {
		atomic.AddUint64(&DefaultSnmp.EarlyRetransSegs, earlyRetransSegs)
		sum += earlyRetransSegs
	}
	if sum > 0 {
		atomic.AddUint64(&DefaultSnmp.RetransSegs, sum)
	}

	// 更新拥塞窗口cwnd
	if rudp.noCwnd == 0 {
		// 更新慢启动阈值
		// rate halving, https://tools.ietf.org/html/rfc6937
		// 发生快速重传，触发快速恢复
		if change > 0 {
			// 当前发送窗口大小
			inflight := rudp.sndNxt - rudp.sndUna
			rudp.ssthresh = inflight / 2
			rudp.ssthresh = max(rudp.ssthresh, IRUDP_THRESH_MIN)
			rudp.cwnd = rudp.ssthresh + resent
			rudp.incr = rudp.cwnd * rudp.mss
		}

		// congestion control, https://tools.ietf.org/html/rfc5681
		// 发生超时重传，进入慢启动
		if lostSegs > 0 {
			rudp.ssthresh = cwnd / 2
			rudp.ssthresh = max(rudp.ssthresh, IRUDP_THRESH_MIN)
			rudp.cwnd = 1
			rudp.incr = rudp.mss
		}

		if rudp.cwnd < 1 {
			rudp.cwnd = 1
			rudp.incr = rudp.mss
		}
	}

	return uint32(minRto)
}

//
// unusedWnd
// @Description: 计算可接收长度，以包为单位
// @receiver rudp
// @return uint16
//
func (rudp *RUDP) unusedWnd() uint16 {
	if len(rudp.rcvQueue) < int(rudp.rcvWnd) {
		return uint16(int(rudp.rcvWnd) - len(rudp.rcvQueue))
	}
	return 0
}

//
// peekSize
// @Description: 检查rcv_queue中第一个包的大小，需要考虑分片
// @receiver rudp
// @return length rcvQueue 中第一个包的大小
//
func (rudp *RUDP) peekSize() (length int) {
	if len(rudp.rcvQueue) == 0 {
		return -1
	}

	seg := rudp.rcvQueue[0]
	if seg.frg == 0 {
		return len(seg.data)
	}

	if len(rudp.rcvQueue) < int(seg.frg+1) {
		return -1
	}

	for i := range rudp.rcvQueue {
		seg = rudp.rcvQueue[i]
		length += len(seg.data)
		if seg.frg == 0 {
			break
		}
	}
	return
}

//
// ReserveBytes
// @Description: 保留buffer的前size个字节
// @receiver rudp
// @param size
// @return bool n >= mss返回false
//
func (rudp *RUDP) ReserveBytes(reserve int) bool {
	if reserve >= int(rudp.mtu-IRUDP_OVERHEAD) || reserve < 0 {
		return false
	}
	rudp.reserved = reserve
	rudp.mss = rudp.mtu - IRUDP_OVERHEAD - uint32(reserve)
	return true
}

//
// ReleaseTX
// @Description: 释放缓存的要发送的数据
// @receiver rudp
//
func (rudp *RUDP) ReleaseTX() {
	for i := range rudp.sndQueue {
		if rudp.sndQueue[i].data != nil {
			xmitBuf.Put(rudp.sndQueue[i].data)
		}
	}
	for i := range rudp.sndBuf {
		if rudp.sndBuf[i].data != nil {
			xmitBuf.Put(rudp.sndBuf[i].data)
		}
	}
	rudp.sndBuf, rudp.sndQueue = nil, nil
}

// WaitSnd 返回待发送的数据数量
func (rudp *RUDP) WaitSnd() int {
	return len(rudp.sndBuf) + len(rudp.sndQueue)
}

//
// NoDelay
// @Description:
// @receiver rudp
// @param noDelay 0:禁用(默认), 1:开启
// @param interval 内部定时器刷新间隔，默认100ms
// @param resend 0:禁用快速重传(默认), 1:开启快速重传
// @param noCwnd 0:启动拥塞控制(默认), 1:禁用拥塞控制
//
func (rudp *RUDP) NoDelay(noDelay int, interval int, resend int, noCwnd int) {
	if noDelay >= 0 {
		rudp.noDelay = uint32(noDelay)
		if noDelay != 0 {
			rudp.rxMinRTO = IRUDP_RTO_NDL
		} else {
			rudp.rxMinRTO = IRUDP_RTO_MIN
		}
	}
	if interval >= 0 {
		if interval > 5000 {
			interval = 5000
		} else if interval < 10 {
			interval = 10
		}
		rudp.interval = uint32(interval)
	}
	if resend >= 0 {
		rudp.fastResend = int32(resend)
	}
	if noCwnd >= 0 {
		rudp.noCwnd = int32(noCwnd)
	}
}

// SetMtu changes MTU size, default is 1400
func (rudp *RUDP) SetMtu(mtu int) int {
	if mtu < 50 || mtu < IRUDP_OVERHEAD {
		return -1
	}
	if rudp.reserved >= int(rudp.mtu-IRUDP_OVERHEAD) || rudp.reserved < 0 {
		return -1
	}

	buffer := make([]byte, mtu)
	if buffer == nil {
		return -2
	}
	rudp.mtu = uint32(mtu)
	rudp.mss = rudp.mtu - IRUDP_OVERHEAD - uint32(rudp.reserved)
	rudp.buffer = buffer
	return 0
}

// WndSize sets maximum window size: sndwnd=32, rcvwnd=32 by default
func (rudp *RUDP) WndSize(sndwnd int, rcvwnd int) int {
	if sndwnd > 0 {
		rudp.sndWnd = uint32(sndwnd)
	}
	if rcvwnd > 0 {
		rudp.rcvWnd = uint32(rcvwnd)
	}
	return 0
}

// 取中值
func bound(low, mid, high uint32) uint32 {
	return min(max(low, mid), high)
}

func min(a, b uint32) uint32 {
	if a <= b {
		return a
	}
	return b
}

func max(a, b uint32) uint32 {
	if a >= b {
		return a
	}
	return b
}

//
// encodeOverHead
// @Description: 把seg头部打包到buffer中
// @receiver seg
// @param ptr
// @return []byte
//
func (seg segment) encodeOverHead(ptr []byte) []byte {
	ptr = encodeUint32(ptr, seg.conv)
	ptr = encodeUint8(ptr, seg.cmd)
	ptr = encodeUint8(ptr, seg.frg)
	ptr = encodeUint16(ptr, seg.wnd)
	ptr = encodeUint32(ptr, seg.ts)
	ptr = encodeUint32(ptr, seg.sn)
	ptr = encodeUint32(ptr, seg.una)
	ptr = encodeUint32(ptr, uint32(len(seg.data)))
	atomic.AddUint64(&DefaultSnmp.OutSegs, 1)
	return ptr
}

func (seg segment) encodeData(ptr []byte) []byte {
	copy(ptr, seg.data)
	return ptr[len(seg.data):]
}

func encodeUint8(b []byte, u uint8) []byte {
	b[0] = u
	return b[1:]
}

func decodeUint8(data []byte, u *byte) []byte {
	*u = data[0]
	return data[1:]
}

func encodeUint16(b []byte, u uint16) []byte {
	binary.LittleEndian.PutUint16(b, u)
	return b[2:]
}

func decodeUint16(data []byte, u *uint16) []byte {
	*u = binary.LittleEndian.Uint16(data)
	return data[2:]
}

func encodeUint32(b []byte, u uint32) []byte {
	binary.LittleEndian.PutUint32(b, u)
	return b[4:]
}

func decodeUint32(data []byte, u *uint32) []byte {
	*u = binary.LittleEndian.Uint32(data)
	return data[4:]
}

func timeDiff(later, earlier uint32) int32 {
	return int32(later - earlier)
}

var startTime time.Time = time.Now()

// currentMs 返回从程序运行开始计时到现在的毫秒数
func currentMs() uint32 {
	return uint32(time.Since(startTime) / time.Millisecond)
}
