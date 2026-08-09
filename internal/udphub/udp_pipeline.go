package udphub

import (
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/config"
)

// Type0Handler handles authenticated DraARL node packets on the same UDP
// socket as ordinary device traffic. Interconnect implements this interface;
// udphub remains independent of the interconnect package.
type Type0Handler interface {
	Handle(data []byte, addr *net.UDPAddr) bool
}

type Type0Writer interface {
	SetWriter(func(*net.UDPAddr, []byte) error)
}

var (
	type0HandlerMu sync.RWMutex
	type0Handler   Type0Handler
)

func SetType0Handler(handler Type0Handler) {
	type0HandlerMu.Lock()
	type0Handler = handler
	type0HandlerMu.Unlock()
	if writer, ok := handler.(Type0Writer); ok && globalConn != nil {
		conn := globalConn
		writer.SetWriter(func(addr *net.UDPAddr, data []byte) error {
			if addr == nil {
				return net.ErrClosed
			}
			_, err := conn.WriteToUDP(data, addr)
			return err
		})
	}
}

func getType0Handler() Type0Handler {
	type0HandlerMu.RLock()
	handler := type0Handler
	type0HandlerMu.RUnlock()
	return handler
}

// AllowEdgeDevicePacket applies the same ordinary-device IP/IP:Port limiter
// in the database-free edge endpoint. Authenticated Type 0 is checked before
// callers invoke this function and therefore uses its separate NodeSession
// resource accounting.
func AllowEdgeDevicePacket(addr *net.UDPAddr) bool {
	return addr == nil || checkRateLimit(addr)
}

const (
	udpJobQueueSize  = 4096
	udpPacketBufSize = 1460
)

type udpDatagramJob struct {
	data       []byte
	baseBuffer []byte
	remoteAddr *net.UDPAddr
	realAddr   *net.UDPAddr
	receivedAt time.Time
}

var (
	udpJobQueues      []chan udpDatagramJob
	udpWorkerWg       sync.WaitGroup
	udpReaderWg       sync.WaitGroup
	udpJobsRead       int64
	udpJobsDropped    int64
	udpJobsHandled    int64
	udpRateLimitDrops int64
	udpQueueHighWater int64
	udpQueueNanos     int64
	udpMaxQueueNanos  int64
)

func udpWorkerCount() int {
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 16 {
		workers = 16
	}
	if cfg := config.TryGet(); cfg != nil && cfg.UDP.IngressWorkers > 0 {
		workers = cfg.UDP.IngressWorkers
		if workers > 64 {
			workers = 64
		}
	}
	return workers
}

// startUDPPipeline 使用单 reader 避免同 FD readLock 竞争，再按源地址稳定分片。
func startUDPPipeline(conn *net.UDPConn) {
	workers := udpWorkerCount()
	perQueue := udpJobQueueSize / workers
	if perQueue < 64 {
		perQueue = 64
	}
	udpJobQueues = make([]chan udpDatagramJob, workers)

	proxyEnabled := false
	if cfg := config.Get(); cfg != nil {
		proxyEnabled = cfg.System.ProxyProtocol == "v2"
	}

	for i := 0; i < workers; i++ {
		udpJobQueues[i] = make(chan udpDatagramJob, perQueue)
		udpWorkerWg.Add(1)
		go udpWorkerLoop(conn, udpJobQueues[i])
	}
	if handler := getType0Handler(); handler != nil {
		if writer, ok := handler.(Type0Writer); ok {
			writer.SetWriter(func(addr *net.UDPAddr, data []byte) error {
				if conn == nil || addr == nil {
					return net.ErrClosed
				}
				_, err := conn.WriteToUDP(data, addr)
				return err
			})
		}
	}
	udpReaderWg.Add(1)
	go udpReaderLoop(conn, proxyEnabled)

	log.Printf("[UDP] pipeline started: readers=1 workers=%d queue=%d", workers, perQueue*workers)
}

func stopUDPPipeline() {
	udpReaderWg.Wait()
	for _, queue := range udpJobQueues {
		close(queue)
	}
	udpWorkerWg.Wait()
	udpJobQueues = nil
	log.Printf("[UDP] pipeline stopped (read=%d handled=%d dropped=%d rate_limited=%d)",
		atomic.LoadInt64(&udpJobsRead), atomic.LoadInt64(&udpJobsHandled),
		atomic.LoadInt64(&udpJobsDropped), atomic.LoadInt64(&udpRateLimitDrops))
}

func udpReaderLoop(conn *net.UDPConn, proxyEnabled bool) {
	defer udpReaderWg.Done()

	for {
		if isUDPShuttingDown() {
			return
		}
		base := packetPool.Get().([]byte)
		n, remoteAddr, err := conn.ReadFromUDP(base)
		if err != nil {
			packetPool.Put(base)
			if isClosedConnError(err) || isUDPShuttingDown() {
				return
			}
			log.Printf("[ERROR] Read from UDP failed: %v", err)
			continue
		}
		atomic.AddInt64(&udpJobsRead, 1)

		packetData := base[:n]
		realAddr := remoteAddr
		if proxyEnabled {
			proxyInfo, payload, isProxy := ParseProxyProtocolV2(packetData)
			if isProxy && proxyInfo != nil && proxyInfo.IsProxy {
				realAddr = GetRealAddr(remoteAddr, proxyInfo)
				packetData = payload
			}
		}
		// Authenticated Type 0 is verified before the ordinary device limiter.
		// The exemption is bound to a live TLS-created NodeSession, never to an
		// IP address. Forged packets return false and remain rate-limited.
		if handler := getType0Handler(); handler != nil && handler.Handle(packetData, remoteAddr) {
			packetPool.Put(base)
			continue
		}
		if realAddr != nil && !checkRateLimit(realAddr) {
			atomic.AddInt64(&udpRateLimitDrops, 1)
			packetPool.Put(base)
			continue
		}

		job := udpDatagramJob{
			data: packetData, baseBuffer: base,
			remoteAddr: remoteAddr, realAddr: realAddr, receivedAt: time.Now(),
		}
		index := udpDatagramShard(packetData, realAddr, len(udpJobQueues))
		queue := udpJobQueues[index]
		select {
		case queue <- job:
			updateMaxInt64(&udpQueueHighWater, int64(totalUDPQueued()))
		default:
			atomic.AddInt64(&udpJobsDropped, 1)
			packetPool.Put(base)
		}
	}
}

func udpDatagramShard(data []byte, fallback *net.UDPAddr, shards int) int {
	if shards <= 1 {
		return 0
	}
	if len(data) >= 51 && data[0] == 'D' && data[1] == 'r' && data[2] == 'a' && data[3] == 'A' {
		const offset64 = uint64(1469598103934665603)
		const prime64 = uint64(1099511628211)
		h := offset64
		nonZero := false
		for _, value := range data[6:38] {
			if value != 0 {
				nonZero = true
			}
			h ^= uint64(value)
			h *= prime64
		}
		if nonZero {
			h ^= uint64(data[50])
			h *= prime64
			return int(h % uint64(shards))
		}
	}
	return udpAddrShard(fallback, shards)
}

func udpWorkerLoop(conn *net.UDPConn, queue <-chan udpDatagramJob) {
	defer udpWorkerWg.Done()
	localHandled := int64(0)
	defer func() { atomic.AddInt64(&udpJobsHandled, localHandled) }()
	for job := range queue {
		queuedFor := time.Since(job.receivedAt).Nanoseconds()
		atomic.AddInt64(&udpQueueNanos, queuedFor)
		updateMaxInt64(&udpMaxQueueNanos, queuedFor)
		processUDPJob(conn, job)
		packetPool.Put(job.baseBuffer)
		localHandled++
		if localHandled >= 256 {
			atomic.AddInt64(&udpJobsHandled, localHandled)
			localHandled = 0
		}
	}
}

func processUDPJob(conn *net.UDPConn, job udpDatagramJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] Recovered from panic while processing packet from %v: %v", job.remoteAddr, r)
		}
	}()

	realAddr := job.realAddr
	if realAddr == nil {
		realAddr = job.remoteAddr
	}
	if len(job.data) >= 4 && job.data[0] == 'D' && job.data[1] == 'r' && job.data[2] == 'a' && job.data[3] == 'A' {
		processDraARLPacket(job.data, job.remoteAddr, realAddr, conn)
	}
}

func totalUDPQueued() int {
	total := 0
	for _, queue := range udpJobQueues {
		total += len(queue)
	}
	return total
}

func GetUDPPipelineStats() map[string]int64 {
	return map[string]int64{
		"read":             atomic.LoadInt64(&udpJobsRead),
		"handled":          atomic.LoadInt64(&udpJobsHandled),
		"dropped":          atomic.LoadInt64(&udpJobsDropped),
		"rate_limit_drops": atomic.LoadInt64(&udpRateLimitDrops),
		"queued":           int64(totalUDPQueued()),
		"queue_high_water": atomic.LoadInt64(&udpQueueHighWater),
		"queue_ns":         atomic.LoadInt64(&udpQueueNanos),
		"max_queue_ns":     atomic.LoadInt64(&udpMaxQueueNanos),
		"workers":          int64(len(udpJobQueues)),
	}
}
