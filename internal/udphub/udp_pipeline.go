package udphub

import (
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"

	"draarl/internal/config"
)

// UDP 收包流水线：少量 reader + worker 池，避免多 goroutine 争抢同一 socket。
const (
	udpJobQueueSize   = 4096
	udpPacketBufSize  = 1460
	defaultUDPReaders = 1
)

type udpDatagramJob struct {
	data       []byte
	remoteAddr *net.UDPAddr
	realAddr   *net.UDPAddr
}

var (
	udpJobQueue     chan udpDatagramJob
	udpWorkerWg     sync.WaitGroup
	udpReaderWg     sync.WaitGroup
	udpPipelineOnce sync.Once
	udpJobsDropped  int64
	udpJobsHandled  int64
)

var udpJobBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, udpPacketBufSize)
		return &b
	},
}

func getUDPJobBuf(n int) []byte {
	bp := udpJobBufPool.Get().(*[]byte)
	buf := *bp
	if cap(buf) < n {
		buf = make([]byte, n)
		return buf
	}
	return buf[:n]
}

func putUDPJobBuf(buf []byte) {
	if buf == nil || cap(buf) < udpPacketBufSize || cap(buf) > 8192 {
		return
	}
	b := buf[:cap(buf)]
	udpJobBufPool.Put(&b)
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := make(net.IP, len(addr.IP))
	copy(ip, addr.IP)
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}

// startUDPPipeline 启动 reader + worker。调用方已设置 globalConn / udpShutdown。
func startUDPPipeline(conn *net.UDPConn) {
	udpPipelineOnce = sync.Once{} // 允许重启
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 16 {
		workers = 16
	}
	readers := defaultUDPReaders
	if workers >= 8 {
		// 高核数时最多 2 个 reader，仍避免过多争抢
		readers = 2
	}

	udpJobQueue = make(chan udpDatagramJob, udpJobQueueSize)

	proxyEnabled := false
	if cfg := config.Get(); cfg != nil {
		proxyEnabled = cfg.System.ProxyProtocol == "v2"
	}

	for i := 0; i < workers; i++ {
		udpWorkerWg.Add(1)
		go udpWorkerLoop(conn)
	}
	for i := 0; i < readers; i++ {
		udpReaderWg.Add(1)
		go udpReaderLoop(conn, proxyEnabled)
	}

	log.Printf("[UDP] pipeline started: readers=%d workers=%d queue=%d", readers, workers, udpJobQueueSize)
}

func stopUDPPipeline() {
	// 关闭连接会让 reader 退出；再关闭队列排空 worker
	udpReaderWg.Wait()
	if udpJobQueue != nil {
		close(udpJobQueue)
	}
	udpWorkerWg.Wait()
	udpJobQueue = nil
	log.Printf("[UDP] pipeline stopped (handled=%d dropped=%d)",
		atomic.LoadInt64(&udpJobsHandled), atomic.LoadInt64(&udpJobsDropped))
}

func udpReaderLoop(conn *net.UDPConn, proxyEnabled bool) {
	defer udpReaderWg.Done()

	for {
		if isUDPShuttingDown() {
			return
		}
		buf := packetPool.Get().([]byte)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			packetPool.Put(buf)
			if isClosedConnError(err) || isUDPShuttingDown() {
				return
			}
			log.Printf("[ERROR] Read from UDP failed: %v", err)
			// 短暂错误：继续读
			continue
		}

		// 限速尽量在 reader 侧尽早丢弃，避免打满 worker 队列
		packetData := buf[:n]
		realAddr := remoteAddr
		if proxyEnabled {
			proxyInfo, payload, isProxy := ParseProxyProtocolV2(packetData)
			if isProxy && proxyInfo != nil && proxyInfo.IsProxy {
				realAddr = GetRealAddr(remoteAddr, proxyInfo)
				packetData = payload
			}
		}
		if realAddr != nil && !checkRateLimit(realAddr.String()) {
			packetPool.Put(buf)
			continue
		}

		// 拷贝到 job buffer，立刻归还读缓冲
		jobData := getUDPJobBuf(len(packetData))
		copy(jobData, packetData)
		packetPool.Put(buf)

		job := udpDatagramJob{
			data:       jobData,
			remoteAddr: cloneUDPAddr(remoteAddr),
			realAddr:   cloneUDPAddr(realAddr),
		}
		select {
		case udpJobQueue <- job:
		default:
			// 队列满：丢包保活实时性
			atomic.AddInt64(&udpJobsDropped, 1)
			putUDPJobBuf(jobData)
		}
	}
}

func udpWorkerLoop(conn *net.UDPConn) {
	defer udpWorkerWg.Done()
	for job := range udpJobQueue {
		processUDPJob(conn, job)
		putUDPJobBuf(job.data)
		atomic.AddInt64(&udpJobsHandled, 1)
	}
}

func processUDPJob(conn *net.UDPConn, job udpDatagramJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] Recovered from panic while processing packet from %v: %v", job.remoteAddr, r)
		}
	}()

	packetData := job.data
	remoteAddr := job.remoteAddr
	realAddr := job.realAddr
	if realAddr == nil {
		realAddr = remoteAddr
	}

	if len(packetData) >= 4 &&
		packetData[0] == 'D' &&
		packetData[1] == 'r' &&
		packetData[2] == 'a' &&
		packetData[3] == 'A' {
		processDraARLPacket(packetData, remoteAddr, realAddr, conn)
		return
	}
	// 未知协议：热路径静默丢弃，避免日志放大
}

// GetUDPPipelineStats 监控统计。
func GetUDPPipelineStats() map[string]int64 {
	qlen := int64(0)
	if udpJobQueue != nil {
		qlen = int64(len(udpJobQueue))
	}
	return map[string]int64{
		"handled": atomic.LoadInt64(&udpJobsHandled),
		"dropped": atomic.LoadInt64(&udpJobsDropped),
		"queued":  qlen,
	}
}
