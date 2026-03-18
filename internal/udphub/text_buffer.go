package udphub

import (
	"log"
	"sync"
	"time"

	"nrllink/internal/gormdb"
)

// ==========================================
// 性能优化：文本消息批量写入
// 避免每条消息单独写入数据库，减少数据库压力
// ==========================================

// TextMessageBuffer 文本消息缓冲区
type TextMessageBuffer struct {
	mu       sync.Mutex
	pending  []*gormdb.CommRecord
	maxBatch int           // 最大批量大小
	interval time.Duration // 刷新间隔

	// 控制
	flushChan chan struct{}
	stopChan  chan struct{}
	running   bool
}

// 全局文本消息缓冲区实例
var globalTextBuffer *TextMessageBuffer

// NewTextMessageBuffer 创建文本消息缓冲区
func NewTextMessageBuffer(maxBatch int, interval time.Duration) *TextMessageBuffer {
	if maxBatch <= 0 {
		maxBatch = 100
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}

	return &TextMessageBuffer{
		pending:   make([]*gormdb.CommRecord, 0, maxBatch),
		maxBatch:  maxBatch,
		interval:  interval,
		flushChan: make(chan struct{}, 1),
		stopChan:  make(chan struct{}),
	}
}

// Start 启动缓冲区后台刷新
func (tb *TextMessageBuffer) Start() {
	if tb == nil {
		return
	}

	tb.running = true
	go tb.runFlusher()
	log.Printf("[TEXT_BUFFER] 文本消息缓冲区已启动 (批量大小: %d, 刷新间隔: %v)", tb.maxBatch, tb.interval)
}

// Stop 停止缓冲区
func (tb *TextMessageBuffer) Stop() {
	if tb == nil || !tb.running {
		return
	}

	tb.running = false
	close(tb.stopChan)

	// 最后一次刷新
	tb.flush()
	log.Println("[TEXT_BUFFER] 文本消息缓冲区已停止")
}

// runFlusher 后台刷新协程
func (tb *TextMessageBuffer) runFlusher() {
	ticker := time.NewTicker(tb.interval)
	defer ticker.Stop()

	for {
		select {
		case <-tb.stopChan:
			return
		case <-ticker.C:
			tb.flush()
		case <-tb.flushChan:
			tb.flush()
		}
	}
}

// Add 添加文本消息到缓冲区
func (tb *TextMessageBuffer) Add(record *gormdb.CommRecord) {
	if tb == nil || !tb.running {
		// 缓冲区未启动，直接写入数据库（降级处理）
		if err := gormdb.Get().Create(record).Error; err != nil {
			log.Printf("[TEXT_BUFFER] 直接写入文本消息失败: %v", err)
		}
		return
	}

	tb.mu.Lock()
	tb.pending = append(tb.pending, record)
	shouldFlush := len(tb.pending) >= tb.maxBatch
	tb.mu.Unlock()

	// 达到批量大小，触发刷新
	if shouldFlush {
		select {
		case tb.flushChan <- struct{}{}:
		default:
			// 已有刷新请求在等待
		}
	}
}

// flush 批量写入数据库
func (tb *TextMessageBuffer) flush() {
	tb.mu.Lock()
	if len(tb.pending) == 0 {
		tb.mu.Unlock()
		return
	}

	// 取出待写入记录
	batch := tb.pending
	tb.pending = make([]*gormdb.CommRecord, 0, tb.maxBatch)
	tb.mu.Unlock()

	// 批量插入（每批 100 条）
	if err := gormdb.Get().CreateInBatches(batch, 100).Error; err != nil {
		log.Printf("[TEXT_BUFFER] 批量写入文本消息失败: %v, 数量: %d", err, len(batch))
		// 失败时不重试，丢弃消息（可根据需求改为重试逻辑）
	} else {
		log.Printf("[TEXT_BUFFER] 批量写入文本消息成功，数量: %d", len(batch))
	}
}

// GetPendingCount 获取待写入数量（用于监控）
func (tb *TextMessageBuffer) GetPendingCount() int {
	if tb == nil {
		return 0
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return len(tb.pending)
}

// ==========================================
// 全局函数
// ==========================================

// InitTextMessageBuffer 初始化全局文本消息缓冲区
func InitTextMessageBuffer() {
	if globalTextBuffer != nil {
		return
	}

	globalTextBuffer = NewTextMessageBuffer(100, 30*time.Second)
	globalTextBuffer.Start()
}

// StopTextMessageBuffer 停止全局文本消息缓冲区
func StopTextMessageBuffer() {
	if globalTextBuffer != nil {
		globalTextBuffer.Stop()
		globalTextBuffer = nil
	}
}

// BufferTextMessage 将文本消息添加到缓冲区
func BufferTextMessage(record *gormdb.CommRecord) {
	if globalTextBuffer != nil {
		globalTextBuffer.Add(record)
	} else {
		// 缓冲区未初始化，直接写入
		if err := gormdb.Get().Create(record).Error; err != nil {
			log.Printf("[TEXT_BUFFER] 直接写入文本消息失败: %v", err)
		}
	}
}

// GetTextBufferPendingCount 获取待写入数量
func GetTextBufferPendingCount() int {
	if globalTextBuffer != nil {
		return globalTextBuffer.GetPendingCount()
	}
	return 0
}
