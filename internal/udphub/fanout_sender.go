package udphub

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
)

// fanoutJob 单次 UDP 写出任务。data 在所有引用它的任务完成前保持只读。
type fanoutJob struct {
	data []byte
	addr *net.UDPAddr
}

// FanoutSender 按目标地址分片发送。同一地址始终由同一 worker 串行处理，
// 不同地址可并行写出，避免破坏无序号语音协议的帧顺序。
type FanoutSender struct {
	conn   *net.UDPConn
	queues []chan fanoutJob
	wg     sync.WaitGroup

	lifecycleMu sync.RWMutex
	submitMu    sync.Mutex
	running     bool
	workers     int
	queueSize   int

	sent  int64
	drops int64
}

const defaultFanoutQueue = 8192

var (
	globalFanoutMu     sync.RWMutex
	globalFanoutSender *FanoutSender
)

func fanoutWorkerCount() int {
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	return workers
}

func newFanoutSender(conn *net.UDPConn, workers, queueSize int) *FanoutSender {
	if workers < 1 {
		workers = 1
	}
	if queueSize < workers {
		queueSize = workers
	}
	perQueueSize := queueSize / workers
	s := &FanoutSender{
		conn:      conn,
		queues:    make([]chan fanoutJob, workers),
		running:   true,
		workers:   workers,
		queueSize: perQueueSize * workers,
	}
	for i := range s.queues {
		s.queues[i] = make(chan fanoutJob, perQueueSize)
		s.wg.Add(1)
		go s.worker(s.queues[i])
	}
	return s
}

// InitFanoutSender 初始化全局 fan-out 发送器。
func InitFanoutSender(conn *net.UDPConn) {
	globalFanoutMu.Lock()
	defer globalFanoutMu.Unlock()
	if globalFanoutSender != nil {
		return
	}
	globalFanoutSender = newFanoutSender(conn, fanoutWorkerCount(), defaultFanoutQueue)
}

func getFanoutSender() *FanoutSender {
	globalFanoutMu.RLock()
	s := globalFanoutSender
	globalFanoutMu.RUnlock()
	return s
}

// StopFanoutSender 停止全局 fan-out 发送器并排空已入队任务。
func StopFanoutSender() {
	globalFanoutMu.Lock()
	s := globalFanoutSender
	globalFanoutSender = nil
	globalFanoutMu.Unlock()
	if s != nil {
		s.stop()
	}
}

func (s *FanoutSender) stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	if !s.running {
		s.lifecycleMu.Unlock()
		return
	}
	s.running = false
	for _, queue := range s.queues {
		close(queue)
	}
	s.lifecycleMu.Unlock()
	s.wg.Wait()
}

func (s *FanoutSender) worker(queue <-chan fanoutJob) {
	defer s.wg.Done()
	for job := range queue {
		if s.conn == nil || job.addr == nil || len(job.data) == 0 {
			continue
		}
		if _, err := s.conn.WriteToUDP(job.data, job.addr); err == nil {
			atomic.AddInt64(&s.sent, 1)
		}
	}
}

func (s *FanoutSender) queueIndex(addr *net.UDPAddr) int {
	return int(fnv32String(addr.String()) % uint32(len(s.queues)))
}

// enqueueFrame 将一帧按地址分片入队。submitMu 保证并发调用不会交错插入同一批目标；
// 队列满时丢弃新任务，不能绕过队列直发，否则会越过已排队的旧帧。
func (s *FanoutSender) enqueueFrame(data []byte, addrs []*net.UDPAddr) bool {
	if s == nil || len(data) == 0 || len(addrs) == 0 {
		return false
	}

	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if !s.running || s.conn == nil || len(s.queues) == 0 {
		return false
	}

	payload := append([]byte(nil), data...)
	accepted := false
	for _, addr := range addrs {
		if addr == nil {
			continue
		}
		queue := s.queues[s.queueIndex(addr)]
		select {
		case queue <- fanoutJob{data: payload, addr: addr}:
			accepted = true
		default:
			atomic.AddInt64(&s.drops, 1)
		}
	}
	return accepted
}

// writeUDPDirect 同步写出，仅用于 fan-out 发送器尚未初始化的启动阶段。
func writeUDPDirect(data []byte, addr *net.UDPAddr) {
	if globalConn == nil || addr == nil || len(data) == 0 {
		return
	}
	_, _ = globalConn.WriteToUDP(data, addr)
}

// writeUDPFanout 使用地址分片发送器并行写出；整帧只复制一次。
func writeUDPFanout(data []byte, addrs []*net.UDPAddr) {
	if len(data) == 0 || len(addrs) == 0 {
		return
	}
	if s := getFanoutSender(); s != nil {
		s.enqueueFrame(data, addrs)
		return
	}
	for _, addr := range addrs {
		writeUDPDirect(data, addr)
	}
}

func (s *FanoutSender) queued() int64 {
	if s == nil {
		return 0
	}
	var total int64
	for _, queue := range s.queues {
		total += int64(len(queue))
	}
	return total
}

// GetFanoutSenderStats 监控用统计。
func GetFanoutSenderStats() map[string]int64 {
	s := getFanoutSender()
	if s == nil {
		return nil
	}
	return map[string]int64{
		"sent":   atomic.LoadInt64(&s.sent),
		"drops":  atomic.LoadInt64(&s.drops),
		"direct": 0,
		"queued": s.queued(),
	}
}
