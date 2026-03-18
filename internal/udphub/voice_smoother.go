package udphub

import (
	"log"
	"net"
	"sync"
	"time"
)

// ==========================================
// 语音平滑发送器
// 解决Web端发送的语音包突发问题，确保UDP客户端接收平稳
// ==========================================

// VoicePacket 语音数据包
type VoicePacket struct {
	Data      []byte
	Addr      *net.UDPAddr
	Timestamp time.Time
}

// VoiceSmoother 语音平滑发送器
// 收集语音包并按固定间隔发送，避免突发
type VoiceSmoother struct {
	mu           sync.Mutex
	queue        []*VoicePacket
	maxQueueSize int
	flushTicker  *time.Ticker
	stopChan     chan struct{}
	conn         *net.UDPConn
	running      bool

	// 统计
	sentPackets int64
	dropped     int64
}

// VoiceSmootherConfig 配置
type VoiceSmootherConfig struct {
	MaxQueueSize int           // 最大队列大小
	FlushRate    time.Duration // 发送间隔（默认20ms，与Opus帧时长匹配）
}

// DefaultVoiceSmootherConfig 默认配置
var DefaultVoiceSmootherConfig = VoiceSmootherConfig{
	MaxQueueSize: 100,        // 最多缓存100帧（约2秒）
	FlushRate:    20 * time.Millisecond,
}

// globalVoiceSmoother 全局语音平滑器实例
var globalVoiceSmoother *VoiceSmoother

// NewVoiceSmoother 创建语音平滑发送器
func NewVoiceSmoother(conn *net.UDPConn, config VoiceSmootherConfig) *VoiceSmoother {
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = DefaultVoiceSmootherConfig.MaxQueueSize
	}
	if config.FlushRate <= 0 {
		config.FlushRate = DefaultVoiceSmootherConfig.FlushRate
	}

	return &VoiceSmoother{
		queue:        make([]*VoicePacket, 0, config.MaxQueueSize),
		maxQueueSize: config.MaxQueueSize,
		flushTicker:  time.NewTicker(config.FlushRate),
		stopChan:     make(chan struct{}),
		conn:         conn,
	}
}

// Start 启动平滑发送器
func (vs *VoiceSmoother) Start() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if vs.running {
		return
	}

	vs.running = true
	go vs.runFlusher()
	log.Printf("[VoiceSmoother] 语音平滑发送器已启动 (发送间隔: %v, 队列大小: %d)",
		DefaultVoiceSmootherConfig.FlushRate, vs.maxQueueSize)
}

// Stop 停止平滑发送器
func (vs *VoiceSmoother) Stop() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if !vs.running {
		return
	}

	vs.running = false
	close(vs.stopChan)
	vs.flushTicker.Stop()

	// 发送剩余数据
	vs.flushQueue()

	log.Printf("[VoiceSmoother] 语音平滑发送器已停止 (发送: %d, 丢弃: %d)",
		vs.sentPackets, vs.dropped)
}

// Enqueue 添加语音包到队列
func (vs *VoiceSmoother) Enqueue(data []byte, addr *net.UDPAddr) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if !vs.running {
		// 未运行时直接发送
		if vs.conn != nil {
			vs.conn.WriteToUDP(data, addr)
		}
		return true
	}

	// 队列满时丢弃最旧的包
	if len(vs.queue) >= vs.maxQueueSize {
		vs.queue = vs.queue[1:]
		vs.dropped++
	}

	// 复制数据
	packetData := make([]byte, len(data))
	copy(packetData, data)

	vs.queue = append(vs.queue, &VoicePacket{
		Data:      packetData,
		Addr:      addr,
		Timestamp: time.Now(),
	})

	return true
}

// runFlusher 定时发送协程
func (vs *VoiceSmoother) runFlusher() {
	for {
		select {
		case <-vs.stopChan:
			return
		case <-vs.flushTicker.C:
			vs.flushOne()
		}
	}
}

// flushOne 发送一个语音包（按固定间隔）
func (vs *VoiceSmoother) flushOne() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if len(vs.queue) == 0 || vs.conn == nil {
		return
	}

	// 取出最早的一个包
	packet := vs.queue[0]
	vs.queue = vs.queue[1:]

	// 发送
	_, err := vs.conn.WriteToUDP(packet.Data, packet.Addr)
	if err == nil {
		vs.sentPackets++
	}
}

// flushQueue 发送队列中所有数据
func (vs *VoiceSmoother) flushQueue() {
	if vs.conn == nil {
		return
	}

	for _, packet := range vs.queue {
		vs.conn.WriteToUDP(packet.Data, packet.Addr)
		vs.sentPackets++
	}
	vs.queue = vs.queue[:0]
}

// GetStats 获取统计信息
func (vs *VoiceSmoother) GetStats() (sentPackets, dropped int64, queueLen int) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.sentPackets, vs.dropped, len(vs.queue)
}

// ==========================================
// 全局函数
// ==========================================

// InitVoiceSmoother 初始化全局语音平滑器
func InitVoiceSmoother(conn *net.UDPConn) {
	if globalVoiceSmoother != nil {
		return
	}
	globalVoiceSmoother = NewVoiceSmoother(conn, DefaultVoiceSmootherConfig)
	globalVoiceSmoother.Start()
}

// StopVoiceSmoother 停止全局语音平滑器
func StopVoiceSmoother() {
	if globalVoiceSmoother != nil {
		globalVoiceSmoother.Stop()
		globalVoiceSmoother = nil
	}
}

// SmoothSendVoice 添加语音包到平滑发送队列
func SmoothSendVoice(data []byte, addr *net.UDPAddr) bool {
	if globalVoiceSmoother != nil {
		return globalVoiceSmoother.Enqueue(data, addr)
	}
	// 降级：直接发送
	if globalConn != nil {
		_, err := globalConn.WriteToUDP(data, addr)
		return err == nil
	}
	return false
}

// GetVoiceSmootherStats 获取语音平滑器统计
func GetVoiceSmootherStats() (sentPackets, dropped int64, queueLen int) {
	if globalVoiceSmoother != nil {
		return globalVoiceSmoother.GetStats()
	}
	return 0, 0, 0
}
