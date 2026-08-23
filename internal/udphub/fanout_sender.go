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
	"draarl/internal/protocol"
)

type fanoutFrameJob struct {
	data            []byte
	sourceGroupData []byte
	partitions      [][]domainReceiverEntry
	sourceID        int
	sourceUser      string
	sourceSSID      byte
	sourceSessionID string
	enqueuedAt      time.Time
	snapshotGen     uint64
	validateGen     bool
	generation      *atomic.Uint64
	onComplete      func(fanoutWriteResult)
	collect         *fanoutCollector // 【H6】异步完成收集器（onComplete 帧使用）
}

type fanoutWorkerJob struct {
	frame   fanoutFrameJob
	targets []domainReceiverEntry
}

// fanoutCollector 聚合各 writer 分片的写入结果，最后一个完成的分片负责
// 触发 onComplete，使 dispatcher 无需等待所有 writer 即可处理下一帧。
type fanoutCollector struct {
	remaining  atomic.Int64
	onComplete func(fanoutWriteResult)
	mu         sync.Mutex
	result     fanoutWriteResult
}

// finishPartition 累加一个分片的写入结果；当全部分片完成时调用 onComplete。
// remaining 为原子计数，恰好一个分片会在减到 0 时触发回调。
func (c *fanoutCollector) finishPartition(result fanoutWriteResult) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.result.attempted += result.attempted
	c.result.sent += result.sent
	c.result.dropped += result.dropped
	c.result.errors += result.errors
	c.result.noBuffer += result.noBuffer
	c.result.wouldBlock += result.wouldBlock
	c.result.tooLarge += result.tooLarge
	total := c.result
	c.mu.Unlock()
	if c.remaining.Add(-1) == 0 && c.onComplete != nil {
		c.onComplete(total)
	}
}

type fanoutWriteResult struct {
	attempted  int64
	sent       int64
	dropped    int64
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
	running     bool
	maxFrameAge time.Duration
	queueSize   int

	framesAccepted   int64
	framesSent       int64
	framesDropped    int64
	framesStale      int64
	targetsAttempted int64
	sent             int64
	targetsDropped   int64
	writeErrors      int64
	noBufferErrors   int64
	wouldBlockErrors int64
	tooLargeErrors   int64
	writerEvictions  int64
	dispatchNanos    int64
	maxDispatchNanos int64
}

const (
	defaultFanoutFrameQueue  = 64
	defaultFanoutMaxFrameAge = 500 * time.Millisecond
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
	// 【H6】writer 队列改为有界缓冲，dispatcher 无需等 writer 就绪即可投递。
	// 缓冲上限按帧队列容量均摊：在正常负载下整条帧队列都能进入 writer 缓冲
	// 而不丢帧；仅当 writer 实际处理能力落后超过一个帧队列（真实过载）时才
	// 触发丢弃，避免单 dispatcher 被慢 writer 锁死拖垮所有域。
	writerQueueBuffer := queueSize / workers
	if writerQueueBuffer < 2 {
		writerQueueBuffer = 2
	}
	if writerQueueBuffer > 256 {
		writerQueueBuffer = 256
	}
	if conn != nil {
		s.writers = append(s.writers, fanoutWriter{conn: conn, queue: make(chan fanoutWorkerJob, writerQueueBuffer)})
	}
	duplicates, duplicateErr := duplicateUDPConns(conn, workers-len(s.writers))
	for _, dup := range duplicates {
		s.writers = append(s.writers, fanoutWriter{conn: dup, owned: true, queue: make(chan fanoutWorkerJob, writerQueueBuffer)})
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
		generationStale := frame.generation != nil && frame.snapshotGen != frame.generation.Load()
		if s.frameExpired(frame) || generationStale || (frame.validateGen && frame.snapshotGen != atomic.LoadUint64(&domainReceiverGen)) {
			dropped := countFrameTargets(frame)
			atomic.AddInt64(&s.framesStale, 1)
			atomic.AddInt64(&s.framesDropped, 1)
			s.addWriteResult(fanoutWriteResult{dropped: dropped})
			if frame.onComplete != nil {
				frame.onComplete(fanoutWriteResult{dropped: dropped})
			}
			continue
		}
		started := time.Now()
		// 【H6】异步派发：只做非阻塞投递，不等待 writer 完成；onComplete 由
		// fanoutCollector 在最后一个分片完成后触发。
		s.dispatchFrame(frame)
		elapsed := time.Since(started).Nanoseconds()
		atomic.AddInt64(&s.dispatchNanos, elapsed)
		updateMaxInt64(&s.maxDispatchNanos, elapsed)
		atomic.AddInt64(&s.framesSent, 1)
	}
}

func countFrameTargets(frame fanoutFrameJob) int64 {
	var count int64
	for i := range frame.partitions {
		for target := range frame.partitions[i] {
			if !isSourceTarget(&frame.partitions[i][target], frame.sourceID, frame.sourceUser, frame.sourceSSID, frame.sourceSessionID) {
				count++
			}
		}
	}
	return count
}

func (s *FanoutSender) frameExpired(frame fanoutFrameJob) bool {
	return s.maxFrameAge > 0 && time.Since(frame.enqueuedAt) > s.maxFrameAge
}

// dispatchFrame 异步派发一帧：向各 writer 队列非阻塞投递分片，队列满时
// 丢弃该分片（不阻塞 dispatcher、不回退为同步写）。onComplete 帧通过
// fanoutCollector 由最后一个完成的分片触发回调。
func (s *FanoutSender) dispatchFrame(frame fanoutFrameJob) {
	var collect *fanoutCollector
	if frame.onComplete != nil {
		planned := 0
		for index, targets := range frame.partitions {
			if index < len(s.writers) && partitionHasTarget(targets, frame.sourceID, frame.sourceUser, frame.sourceSSID, frame.sourceSessionID) {
				planned++
			}
		}
		if planned == 0 {
			// 无可投递分片：立即完成，避免 onComplete 永不触发
			frame.onComplete(fanoutWriteResult{})
			return
		}
		collect = &fanoutCollector{onComplete: frame.onComplete}
		collect.remaining.Store(int64(planned))
		frame.collect = collect
	}

	for index, targets := range frame.partitions {
		if index >= len(s.writers) || !partitionHasTarget(targets, frame.sourceID, frame.sourceUser, frame.sourceSSID, frame.sourceSessionID) {
			continue
		}
		job := fanoutWorkerJob{frame: frame, targets: targets}
		accepted, evicted := enqueueLatestWorkerJob(s.writers[index].queue, job)
		if evicted != nil {
			s.dropWorkerJob(*evicted)
		}
		if !accepted {
			// The queue can only reject after a concurrent close during shutdown.
			dropped := countPartitionTargets(targets, frame.sourceID, frame.sourceUser, frame.sourceSSID, frame.sourceSessionID)
			s.addWriteResult(fanoutWriteResult{dropped: dropped})
			if collect != nil {
				collect.finishPartition(fanoutWriteResult{dropped: dropped})
			}
		}
	}
}

// enqueueLatestWorkerJob keeps a bounded writer queue fresh under overload.
// It evicts the oldest frame rather than dropping the newest voice frame.
func enqueueLatestWorkerJob(queue chan fanoutWorkerJob, job fanoutWorkerJob) (accepted bool, evicted *fanoutWorkerJob) {
	select {
	case queue <- job:
		return true, nil
	default:
	}
	select {
	case old := <-queue:
		evicted = &old
	default:
		return false, nil
	}
	select {
	case queue <- job:
		return true, evicted
	default:
		return false, evicted
	}
}

func (s *FanoutSender) dropWorkerJob(job fanoutWorkerJob) {
	dropped := countPartitionTargets(job.targets, job.frame.sourceID, job.frame.sourceUser, job.frame.sourceSSID, job.frame.sourceSessionID)
	s.addWriteResult(fanoutWriteResult{dropped: dropped})
	atomic.AddInt64(&s.writerEvictions, 1)
	if job.frame.collect != nil {
		job.frame.collect.finishPartition(fanoutWriteResult{dropped: dropped})
	}
}

// addWriteResult 累加一次写入统计到全局指标。
func (s *FanoutSender) addWriteResult(result fanoutWriteResult) {
	if s == nil {
		return
	}
	atomic.AddInt64(&s.targetsAttempted, result.attempted)
	atomic.AddInt64(&s.sent, result.sent)
	atomic.AddInt64(&s.targetsDropped, result.dropped)
	atomic.AddInt64(&s.writeErrors, result.errors)
	atomic.AddInt64(&s.noBufferErrors, result.noBuffer)
	atomic.AddInt64(&s.wouldBlockErrors, result.wouldBlock)
	atomic.AddInt64(&s.tooLargeErrors, result.tooLarge)
}

// countPartitionTargets 统计一个分片中非源目标的接收者数量。
func countPartitionTargets(targets []domainReceiverEntry, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string) int64 {
	var count int64
	for i := range targets {
		if !isSourceTarget(&targets[i], sourceID, sourceUser, sourceSSID, sourceSessionID) {
			count++
		}
	}
	return count
}

func partitionHasTarget(targets []domainReceiverEntry, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string) bool {
	for i := range targets {
		if !isSourceTarget(&targets[i], sourceID, sourceUser, sourceSSID, sourceSessionID) {
			return true
		}
	}
	return false
}

func isSourceTarget(target *domainReceiverEntry, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string) bool {
	if target == nil {
		return true
	}
	if sourceSessionID != "" {
		return target.sessionID == sourceSessionID
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
			if isSourceTarget(target, job.frame.sourceID, job.frame.sourceUser, job.frame.sourceSSID, job.frame.sourceSessionID) {
				continue
			}
			result.attempted++
			payload := job.frame.data
			if target.sourceGroupV1 && len(job.frame.sourceGroupData) > 0 {
				payload = job.frame.sourceGroupData
			}
			if _, err := writer.conn.WriteToUDPAddrPort(payload, target.addr); err == nil {
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
		// 【H6】writer 直接累加全局指标，并通知（可能的）完成收集器
		s.addWriteResult(result)
		if job.frame.collect != nil {
			job.frame.collect.finishPartition(result)
		}
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

	// Channel send/receive operations are safe for concurrent callers. Do not
	// serialize all domains behind a global mutex; lifecycleMu above keeps the
	// channel from being closed while this non-blocking admission runs.
	accepted, evicted := enqueueLatestFrame(s.frames, job)
	if evicted != nil {
		dropped := countFrameTargets(*evicted)
		atomic.AddInt64(&s.framesDropped, 1)
		s.addWriteResult(fanoutWriteResult{dropped: dropped})
		if evicted.onComplete != nil {
			evicted.onComplete(fanoutWriteResult{dropped: dropped})
		}
	}
	if accepted {
		atomic.AddInt64(&s.framesAccepted, 1)
		return true
	}
	atomic.AddInt64(&s.framesDropped, 1)
	s.addWriteResult(fanoutWriteResult{dropped: countFrameTargets(job)})
	return false
}

func enqueueLatestFrame(queue chan fanoutFrameJob, job fanoutFrameJob) (accepted bool, evicted *fanoutFrameJob) {
	select {
	case queue <- job:
		return true, nil
	default:
	}
	select {
	case old := <-queue:
		evicted = &old
	default:
	}
	select {
	case queue <- job:
		return true, evicted
	default:
		return false, evicted
	}
}

func (s *FanoutSender) enqueueDomainFrame(data []byte, snap *domainReceiverSnap, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string, sourceGroupID int) bool {
	if s == nil || snap == nil || len(data) == 0 || len(snap.entries) == 0 || snap.workers != len(s.writers) {
		return false
	}
	var sourceGroupData []byte
	for i := range snap.entries {
		if snap.entries[i].sourceGroupV1 {
			sourceGroupData, _ = protocol.WithSourceGroupID(data, sourceGroupID)
			break
		}
	}
	return s.enqueue(fanoutFrameJob{
		data:            append([]byte(nil), data...),
		sourceGroupData: sourceGroupData,
		partitions:      snap.partitions,
		sourceID:        sourceID,
		sourceUser:      sourceUser,
		sourceSSID:      sourceSSID,
		sourceSessionID: sourceSessionID,
		enqueuedAt:      time.Now(),
		snapshotGen:     snap.gen,
		validateGen:     true,
	})
}

func (s *FanoutSender) enqueueFixedDomainFrame(
	data []byte,
	snap *domainReceiverSnap,
	sourceGroupID int,
	generation *atomic.Uint64,
	onComplete func(fanoutWriteResult),
) bool {
	if s == nil || snap == nil || len(data) == 0 || len(snap.entries) == 0 || snap.workers != len(s.writers) || generation == nil {
		return false
	}
	var sourceGroupData []byte
	for i := range snap.entries {
		if snap.entries[i].sourceGroupV1 {
			sourceGroupData, _ = protocol.WithSourceGroupID(data, sourceGroupID)
			break
		}
	}
	return s.enqueue(fanoutFrameJob{
		data:            append([]byte(nil), data...),
		sourceGroupData: sourceGroupData,
		partitions:      snap.partitions,
		enqueuedAt:      time.Now(),
		snapshotGen:     generation.Load(),
		validateGen:     false,
		generation:      generation,
		onComplete:      onComplete,
	})
}

func writeUDPDomain(data []byte, snap *domainReceiverSnap, sourceID int, sourceUser string, sourceSSID byte, sourceSessionID string, sourceGroupID int) {
	if len(data) == 0 || snap == nil || len(snap.entries) == 0 {
		return
	}
	if s := getFanoutSender(); s != nil {
		// Once the asynchronous sender is published, a rejected enqueue means
		// shutdown or overload. Never turn that condition into a synchronous
		// UDP write on the ingress worker; dropping the frame preserves latency.
		_ = s.enqueueDomainFrame(data, snap, sourceID, sourceUser, sourceSSID, sourceSessionID, sourceGroupID)
		return
	}
	var sourceGroupData []byte
	for i := range snap.entries {
		target := &snap.entries[i]
		if isSourceTarget(target, sourceID, sourceUser, sourceSSID, sourceSessionID) {
			continue
		}
		payload := data
		if target.sourceGroupV1 {
			if sourceGroupData == nil {
				sourceGroupData, _ = protocol.WithSourceGroupID(data, sourceGroupID)
			}
			if len(sourceGroupData) > 0 {
				payload = sourceGroupData
			}
		}
		_, _ = globalConn.WriteToUDPAddrPort(payload, target.addr)
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
		"targets_dropped":      atomic.LoadInt64(&s.targetsDropped),
		"write_errors":         atomic.LoadInt64(&s.writeErrors),
		"enobufs":              atomic.LoadInt64(&s.noBufferErrors),
		"would_block":          atomic.LoadInt64(&s.wouldBlockErrors),
		"message_too_large":    atomic.LoadInt64(&s.tooLargeErrors),
		"writer_evictions":     atomic.LoadInt64(&s.writerEvictions),
		"dispatch_ns":          atomic.LoadInt64(&s.dispatchNanos),
		"max_dispatch_ns":      atomic.LoadInt64(&s.maxDispatchNanos),
		"queued":               s.queued(),
	}
}
