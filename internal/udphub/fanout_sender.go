package udphub

import (
	"errors"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"draarl/internal/config"
)

type fanoutFrameJob struct {
	data        []byte
	partitions  [][]domainReceiverEntry
	sourceID    int
	sourceUser  string
	sourceSSID  byte
	enqueuedAt  time.Time
	snapshotGen uint64
	validateGen bool
}

type fanoutWorkerJob struct {
	frame   fanoutFrameJob
	targets []domainReceiverEntry
	result  chan<- fanoutWriteResult
}

type fanoutWriteResult struct {
	attempted  int64
	sent       int64
	errors     int64
	noBuffer   int64
	wouldBlock int64
	tooLarge   int64
}

type fanoutWriter struct {
	conn  *net.UDPConn
	owned bool
	queue chan fanoutWorkerJob
}

// FanoutSender 以完整帧为队列单位。dispatcher 同时唤醒各个 writer，
// 每个 writer 使用独立的 Go poll.FD 视图，并稳定处理自己的目标分片。
type FanoutSender struct {
	writers []fanoutWriter
	frames  chan fanoutFrameJob
	wg      sync.WaitGroup

	lifecycleMu sync.RWMutex
	submitMu    sync.Mutex
	running     bool
	maxFrameAge time.Duration
	queueSize   int

	framesAccepted   int64
	framesSent       int64
	framesDropped    int64
	framesStale      int64
	targetsAttempted int64
	sent             int64
	writeErrors      int64
	noBufferErrors   int64
	wouldBlockErrors int64
	tooLargeErrors   int64
	dispatchNanos    int64
	maxDispatchNanos int64
}

const (
	defaultFanoutFrameQueue  = 64
	defaultFanoutMaxFrameAge = 120 * time.Millisecond
)

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
	if cfg := config.TryGet(); cfg != nil && cfg.UDP.SendWorkers > 0 {
		workers = cfg.UDP.SendWorkers
		if workers > 32 {
			workers = 32
		}
	}
	return workers
}

func fanoutRuntimeSettings() (queueSize int, maxAge time.Duration) {
	queueSize = defaultFanoutFrameQueue
	maxAge = defaultFanoutMaxFrameAge
	if cfg := config.TryGet(); cfg != nil {
		if cfg.UDP.FrameQueueSize > 0 {
			queueSize = cfg.UDP.FrameQueueSize
		}
		if cfg.UDP.MaxFrameAgeMS > 0 {
			maxAge = time.Duration(cfg.UDP.MaxFrameAgeMS) * time.Millisecond
		}
	}
	if queueSize > 4096 {
		queueSize = 4096
	}
	return queueSize, maxAge
}

func duplicateUDPConns(conn *net.UDPConn, count int) ([]*net.UDPConn, error) {
	if conn == nil {
		return nil, errors.New("nil UDP connection")
	}
	if count <= 0 {
		return nil, nil
	}

	duplicates := make([]*net.UDPConn, 0, count)
	source := conn
	for len(duplicates) < count {
		// Windows permits one FilePacketConn copy from each IOCP-associated
		// socket view. Chain the copies so every source is consumed once while
		// all resulting sockets still share the bound UDP endpoint.
		file, err := source.File()
		if err != nil {
			return duplicates, err
		}
		packetConn, duplicateErr := net.FilePacketConn(file)
		file.Close()
		if duplicateErr != nil {
			return duplicates, duplicateErr
		}
		udpConn, ok := packetConn.(*net.UDPConn)
		if !ok {
			packetConn.Close()
			return duplicates, errors.New("duplicated packet connection is not UDP")
		}
		duplicates = append(duplicates, udpConn)
		source = udpConn
	}
	return duplicates, nil
}

func newFanoutSender(conn *net.UDPConn, workers, queueSize int) *FanoutSender {
	return newFanoutSenderWithMaxAge(conn, workers, queueSize, 0)
}

func newFanoutSenderWithMaxAge(conn *net.UDPConn, workers, queueSize int, maxFrameAge time.Duration) *FanoutSender {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}

	s := &FanoutSender{
		frames:      make(chan fanoutFrameJob, queueSize),
		running:     true,
		maxFrameAge: maxFrameAge,
		queueSize:   queueSize,
	}
	if conn != nil {
		s.writers = append(s.writers, fanoutWriter{conn: conn, queue: make(chan fanoutWorkerJob)})
	}
	duplicates, duplicateErr := duplicateUDPConns(conn, workers-len(s.writers))
	for _, dup := range duplicates {
		s.writers = append(s.writers, fanoutWriter{conn: dup, owned: true, queue: make(chan fanoutWorkerJob)})
	}
	if duplicateErr != nil {
		log.Printf("[UDP] fan-out writer duplication stopped at %d/%d: %v", len(s.writers), workers, duplicateErr)
	}
	if len(s.writers) == 0 {
		s.running = false
		return s
	}

	for i := range s.writers {
		s.wg.Add(1)
		go s.worker(&s.writers[i])
	}
	s.wg.Add(1)
	go s.dispatcher()
	return s
}

// InitFanoutSender 初始化全局 fan-out 发送器。
func InitFanoutSender(conn *net.UDPConn) {
	globalFanoutMu.Lock()
	defer globalFanoutMu.Unlock()
	if globalFanoutSender != nil {
		return
	}
	queueSize, maxAge := fanoutRuntimeSettings()
	globalFanoutSender = newFanoutSenderWithMaxAge(
		conn,
		fanoutWorkerCount(),
		queueSize,
		maxAge,
	)
	log.Printf("[UDP] fan-out sender started: writers=%d frame_queue=%d max_age=%s",
		len(globalFanoutSender.writers), globalFanoutSender.queueSize, globalFanoutSender.maxFrameAge)
}

func getFanoutSender() *FanoutSender {
	globalFanoutMu.RLock()
	s := globalFanoutSender
	globalFanoutMu.RUnlock()
	return s
}

func currentFanoutWorkerCount() int {
	if s := getFanoutSender(); s != nil && len(s.writers) > 0 {
		return len(s.writers)
	}
	return 1
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
	close(s.frames)
	s.lifecycleMu.Unlock()
	s.wg.Wait()
}

func (s *FanoutSender) dispatcher() {
	defer s.wg.Done()
	defer func() {
		for i := range s.writers {
			close(s.writers[i].queue)
		}
	}()

	for frame := range s.frames {
		if s.frameExpired(frame) || (frame.validateGen && frame.snapshotGen != atomic.LoadUint64(&domainReceiverGen)) {
			atomic.AddInt64(&s.framesStale, 1)
			atomic.AddInt64(&s.framesDropped, 1)
			continue
		}
		started := time.Now()
		s.dispatchFrame(frame)
		elapsed := time.Since(started).Nanoseconds()
		atomic.AddInt64(&s.dispatchNanos, elapsed)
		updateMaxInt64(&s.maxDispatchNanos, elapsed)
		atomic.AddInt64(&s.framesSent, 1)
	}
}

func (s *FanoutSender) frameExpired(frame fanoutFrameJob) bool {
	return s.maxFrameAge > 0 && time.Since(frame.enqueuedAt) > s.maxFrameAge
}

func (s *FanoutSender) dispatchFrame(frame fanoutFrameJob) {
	resultCh := make(chan fanoutWriteResult, len(s.writers))
	jobs := 0
	for index, targets := range frame.partitions {
		if index >= len(s.writers) || !partitionHasTarget(targets, frame.sourceID, frame.sourceUser, frame.sourceSSID) {
			continue
		}
		jobs++
		s.writers[index].queue <- fanoutWorkerJob{frame: frame, targets: targets, result: resultCh}
	}

	var total fanoutWriteResult
	for i := 0; i < jobs; i++ {
		result := <-resultCh
		total.attempted += result.attempted
		total.sent += result.sent
		total.errors += result.errors
		total.noBuffer += result.noBuffer
		total.wouldBlock += result.wouldBlock
		total.tooLarge += result.tooLarge
	}
	atomic.AddInt64(&s.targetsAttempted, total.attempted)
	atomic.AddInt64(&s.sent, total.sent)
	atomic.AddInt64(&s.writeErrors, total.errors)
	atomic.AddInt64(&s.noBufferErrors, total.noBuffer)
	atomic.AddInt64(&s.wouldBlockErrors, total.wouldBlock)
	atomic.AddInt64(&s.tooLargeErrors, total.tooLarge)
}

func partitionHasTarget(targets []domainReceiverEntry, sourceID int, sourceUser string, sourceSSID byte) bool {
	for i := range targets {
		if !isSourceTarget(&targets[i], sourceID, sourceUser, sourceSSID) {
			return true
		}
	}
	return false
}

func isSourceTarget(target *domainReceiverEntry, sourceID int, sourceUser string, sourceSSID byte) bool {
	if target == nil {
		return true
	}
	if sourceID > 0 && target.deviceID == sourceID {
		return true
	}
	return sourceUser != "" && target.username == sourceUser && target.ssid == sourceSSID
}

func (s *FanoutSender) worker(writer *fanoutWriter) {
	defer s.wg.Done()
	defer func() {
		if writer.owned && writer.conn != nil {
			writer.conn.Close()
		}
	}()

	for job := range writer.queue {
		result := fanoutWriteResult{}
		for i := range job.targets {
			target := &job.targets[i]
			if isSourceTarget(target, job.frame.sourceID, job.frame.sourceUser, job.frame.sourceSSID) {
				continue
			}
			result.attempted++
			if _, err := writer.conn.WriteToUDPAddrPort(job.frame.data, target.addr); err == nil {
				result.sent++
			} else {
				result.errors++
				switch {
				case errors.Is(err, syscall.ENOBUFS):
					result.noBuffer++
				case errors.Is(err, syscall.EAGAIN):
					result.wouldBlock++
				case errors.Is(err, syscall.EMSGSIZE):
					result.tooLarge++
				}
			}
		}
		job.result <- result
	}
}

func updateMaxInt64(target *int64, value int64) {
	for {
		current := atomic.LoadInt64(target)
		if value <= current || atomic.CompareAndSwapInt64(target, current, value) {
			return
		}
	}
}

func (s *FanoutSender) enqueue(job fanoutFrameJob) bool {
	if s == nil || len(job.data) == 0 || len(job.partitions) == 0 {
		return false
	}

	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if !s.running || len(s.writers) == 0 {
		return false
	}

	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	accepted, evicted := enqueueLatestFrame(s.frames, job)
	if evicted {
		atomic.AddInt64(&s.framesDropped, 1)
	}
	if accepted {
		atomic.AddInt64(&s.framesAccepted, 1)
		return true
	}
	atomic.AddInt64(&s.framesDropped, 1)
	return false
}

func enqueueLatestFrame(queue chan fanoutFrameJob, job fanoutFrameJob) (accepted, evicted bool) {
	select {
	case queue <- job:
		return true, false
	default:
	}
	select {
	case <-queue:
		evicted = true
	default:
	}
	select {
	case queue <- job:
		return true, evicted
	default:
		return false, evicted
	}
}

func (s *FanoutSender) enqueueDomainFrame(data []byte, snap *domainReceiverSnap, sourceID int, sourceUser string, sourceSSID byte) bool {
	if s == nil || snap == nil || len(data) == 0 || len(snap.entries) == 0 || snap.workers != len(s.writers) {
		return false
	}
	return s.enqueue(fanoutFrameJob{
		data:        append([]byte(nil), data...),
		partitions:  snap.partitions,
		sourceID:    sourceID,
		sourceUser:  sourceUser,
		sourceSSID:  sourceSSID,
		enqueuedAt:  time.Now(),
		snapshotGen: snap.gen,
		validateGen: true,
	})
}

func writeUDPDomain(data []byte, snap *domainReceiverSnap, sourceID int, sourceUser string, sourceSSID byte) {
	if len(data) == 0 || snap == nil || len(snap.entries) == 0 {
		return
	}
	if s := getFanoutSender(); s != nil && s.enqueueDomainFrame(data, snap, sourceID, sourceUser, sourceSSID) {
		return
	}
	for i := range snap.entries {
		target := &snap.entries[i]
		if isSourceTarget(target, sourceID, sourceUser, sourceSSID) {
			continue
		}
		_, _ = globalConn.WriteToUDPAddrPort(data, target.addr)
	}
}

func (s *FanoutSender) queued() int64 {
	if s == nil {
		return 0
	}
	return int64(len(s.frames))
}

// GetFanoutSenderStats 返回低开销累计指标。
func GetFanoutSenderStats() map[string]int64 {
	s := getFanoutSender()
	if s == nil {
		return nil
	}
	return map[string]int64{
		"writers":              int64(len(s.writers)),
		"parallel_fds":         int64(max(0, len(s.writers)-1)),
		"frame_queue_capacity": int64(s.queueSize),
		"frames_accepted":      atomic.LoadInt64(&s.framesAccepted),
		"frames_sent":          atomic.LoadInt64(&s.framesSent),
		"frames_dropped":       atomic.LoadInt64(&s.framesDropped),
		"frames_stale":         atomic.LoadInt64(&s.framesStale),
		"targets_attempted":    atomic.LoadInt64(&s.targetsAttempted),
		"sent":                 atomic.LoadInt64(&s.sent),
		"write_errors":         atomic.LoadInt64(&s.writeErrors),
		"enobufs":              atomic.LoadInt64(&s.noBufferErrors),
		"would_block":          atomic.LoadInt64(&s.wouldBlockErrors),
		"message_too_large":    atomic.LoadInt64(&s.tooLargeErrors),
		"dispatch_ns":          atomic.LoadInt64(&s.dispatchNanos),
		"max_dispatch_ns":      atomic.LoadInt64(&s.maxDispatchNanos),
		"queued":               s.queued(),
	}
}
