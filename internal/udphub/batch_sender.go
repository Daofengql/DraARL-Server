package udphub

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"nrllink/internal/models"
)

// ==========================================
// 性能优化：UDP 批量发送
// 减少系统调用次数，提高高并发场景下的吞吐量
// ==========================================

// BatchSender UDP 批量发送器
type BatchSender struct {
	conn       *net.UDPConn
	batchSize  int           // 批量大小
	queueSize  int           // 队列大小
	queue      chan *udpPacket
	wg         sync.WaitGroup
	running    int32
	stats      BatchSenderStats
	flushChan  chan struct{}
}

// udpPacket UDP 数据包
type udpPacket struct {
	data []byte
	addr *net.UDPAddr
}

// BatchSenderStats 批量发送器统计
type BatchSenderStats struct {
	SentPackets   int64 // 已发送包数
	SentBytes     int64 // 已发送字节数
	QueueDrops    int64 // 队列丢弃数
	BatchCount    int64 // 批量发送次数
	FlushCount    int64 // 刷新次数
}

// BatchSenderConfig 批量发送器配置
type BatchSenderConfig struct {
	BatchSize    int           // 批量大小（默认 50）
	QueueSize    int           // 队列大小（默认 10000）
	FlushTimeout time.Duration // 刷新超时（默认 5ms）
}

// DefaultBatchSenderConfig 默认配置
var DefaultBatchSenderConfig = BatchSenderConfig{
	BatchSize:    50,
	QueueSize:    10000,
	FlushTimeout: 5 * time.Millisecond,
}

// globalBatchSender 全局批量发送器实例
var globalBatchSender *BatchSender

// NewBatchSender 创建批量发送器
func NewBatchSender(conn *net.UDPConn, config BatchSenderConfig) *BatchSender {
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultBatchSenderConfig.BatchSize
	}
	if config.QueueSize <= 0 {
		config.QueueSize = DefaultBatchSenderConfig.QueueSize
	}
	if config.FlushTimeout <= 0 {
		config.FlushTimeout = DefaultBatchSenderConfig.FlushTimeout
	}

	return &BatchSender{
		conn:      conn,
		batchSize: config.BatchSize,
		queueSize: config.QueueSize,
		queue:     make(chan *udpPacket, config.QueueSize),
		flushChan: make(chan struct{}, 1),
	}
}

// Start 启动批量发送器
func (bs *BatchSender) Start() {
	if !atomic.CompareAndSwapInt32(&bs.running, 0, 1) {
		return // 已经在运行
	}

	bs.wg.Add(1)
	go bs.runBatchSender()

	bs.wg.Add(1)
	go bs.runFlusher()

	log.Printf("[BATCH_SENDER] UDP 批量发送器已启动 (批量大小: %d, 队列大小: %d)",
		bs.batchSize, bs.queueSize)
}

// Stop 停止批量发送器
func (bs *BatchSender) Stop() {
	if !atomic.CompareAndSwapInt32(&bs.running, 1, 0) {
		return // 已经停止
	}

	// 触发最后一次刷新
	select {
	case bs.flushChan <- struct{}{}:
	default:
	}

	// 等待处理完成
	bs.wg.Wait()

	log.Printf("[BATCH_SENDER] UDP 批量发送器已停止 (发送: %d 包, %d 字节, 批量: %d 次)",
		atomic.LoadInt64(&bs.stats.SentPackets),
		atomic.LoadInt64(&bs.stats.SentBytes),
		atomic.LoadInt64(&bs.stats.BatchCount))
}

// Send 添加到发送队列（非阻塞）
func (bs *BatchSender) Send(data []byte, addr *net.UDPAddr) bool {
	if atomic.LoadInt32(&bs.running) == 0 {
		// 未运行，直接发送
		bs.conn.WriteToUDP(data, addr)
		return true
	}

	// 复制数据（因为调用者可能重用缓冲区）
	packetData := make([]byte, len(data))
	copy(packetData, data)

	select {
	case bs.queue <- &udpPacket{data: packetData, addr: addr}:
		return true
	default:
		// 队列满，丢弃
		atomic.AddInt64(&bs.stats.QueueDrops, 1)
		return false
	}
}

// SendImmediate 立即发送（不经过队列）
func (bs *BatchSender) SendImmediate(data []byte, addr *net.UDPAddr) error {
	_, err := bs.conn.WriteToUDP(data, addr)
	if err == nil {
		atomic.AddInt64(&bs.stats.SentPackets, 1)
		atomic.AddInt64(&bs.stats.SentBytes, int64(len(data)))
	}
	return err
}

// runBatchSender 批量发送协程
func (bs *BatchSender) runBatchSender() {
	defer bs.wg.Done()

	// 批量缓冲区
	batch := make([]*udpPacket, 0, bs.batchSize)
	timeout := time.NewTimer(DefaultBatchSenderConfig.FlushTimeout)
	defer timeout.Stop()

	for {
		select {
		case packet, ok := <-bs.queue:
			if !ok {
				// 队列关闭，发送剩余数据
				if len(batch) > 0 {
					bs.sendBatch(batch)
				}
				return
			}

			batch = append(batch, packet)

			// 达到批量大小，立即发送
			if len(batch) >= bs.batchSize {
				bs.sendBatch(batch)
				batch = make([]*udpPacket, 0, bs.batchSize)
				timeout.Reset(DefaultBatchSenderConfig.FlushTimeout)
			}

		case <-timeout.C:
			// 超时，发送当前批次
			if len(batch) > 0 {
				bs.sendBatch(batch)
				batch = make([]*udpPacket, 0, bs.batchSize)
			}
			timeout.Reset(DefaultBatchSenderConfig.FlushTimeout)

		case <-bs.flushChan:
			// 刷新请求，发送当前批次
			if len(batch) > 0 {
				bs.sendBatch(batch)
				batch = make([]*udpPacket, 0, bs.batchSize)
			}
			atomic.AddInt64(&bs.stats.FlushCount, 1)
		}
	}
}

// runFlusher 定时刷新协程
func (bs *BatchSender) runFlusher() {
	defer bs.wg.Done()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for atomic.LoadInt32(&bs.running) == 1 {
		<-ticker.C
		select {
		case bs.flushChan <- struct{}{}:
		default:
			// 已有刷新请求在等待
		}
	}
}

// sendBatch 发送一批数据包
func (bs *BatchSender) sendBatch(batch []*udpPacket) {
	if len(batch) == 0 {
		return
	}

	for _, packet := range batch {
		if _, err := bs.conn.WriteToUDP(packet.data, packet.addr); err == nil {
			atomic.AddInt64(&bs.stats.SentPackets, 1)
			atomic.AddInt64(&bs.stats.SentBytes, int64(len(packet.data)))
		}
	}

	atomic.AddInt64(&bs.stats.BatchCount, 1)
}

// GetStats 获取统计信息
func (bs *BatchSender) GetStats() BatchSenderStats {
	return BatchSenderStats{
		SentPackets: atomic.LoadInt64(&bs.stats.SentPackets),
		SentBytes:   atomic.LoadInt64(&bs.stats.SentBytes),
		QueueDrops:  atomic.LoadInt64(&bs.stats.QueueDrops),
		BatchCount:  atomic.LoadInt64(&bs.stats.BatchCount),
		FlushCount:  atomic.LoadInt64(&bs.stats.FlushCount),
	}
}

// ==========================================
// 全局函数
// ==========================================

// InitBatchSender 初始化全局批量发送器
func InitBatchSender(conn *net.UDPConn) {
	if globalBatchSender != nil {
		return
	}

	globalBatchSender = NewBatchSender(conn, DefaultBatchSenderConfig)
	globalBatchSender.Start()
}

// StopBatchSender 停止全局批量发送器
func StopBatchSender() {
	if globalBatchSender != nil {
		globalBatchSender.Stop()
		globalBatchSender = nil
	}
}

// BatchSendUDP 添加到批量发送队列
func BatchSendUDP(data []byte, addr *net.UDPAddr) bool {
	if globalBatchSender != nil {
		return globalBatchSender.Send(data, addr)
	}
	// 降级：直接发送
	if globalConn != nil {
		_, err := globalConn.WriteToUDP(data, addr)
		return err == nil
	}
	return false
}

// GetBatchSenderStats 获取批量发送器统计
func GetBatchSenderStats() *BatchSenderStats {
	if globalBatchSender != nil {
		stats := globalBatchSender.GetStats()
		return &stats
	}
	return nil
}

// ==========================================
// 性能优化：批量转发函数
// ==========================================

// forwardToUDPDevicesBatch 批量转发到 UDP 设备
func forwardToUDPDevicesBatch(devices []*models.Device, sourceID int, expectedGroupID int, skipSelf bool, data []byte) {
	if globalBatchSender != nil {
		// 使用批量发送器
		for _, target := range devices {
			if canForwardToDevice(target, sourceID, expectedGroupID, skipSelf) {
				globalBatchSender.Send(data, target.UDPAddr)
			}
		}
	} else {
		// 降级：直接发送
		for _, target := range devices {
			if canForwardToDevice(target, sourceID, expectedGroupID, skipSelf) {
				globalConn.WriteToUDP(data, target.UDPAddr)
			}
		}
	}
}
