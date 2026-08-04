package udphub

import (
	"fmt"
	"log"
	"sync"
	"time"

	"draarl/internal/gormdb"
)

// CommRecorder 通信录制管理器（整合所有组件）
type CommRecorder struct {
	buffer   *CommBuffer
	uploader *CommUploader
	syncer   *CommSyncer
	config   *CommSettingsConfig
	running  bool
	mu       sync.RWMutex

	// 定时器
	timeoutTicker *time.Ticker
	uploadTicker  *time.Ticker
	dbSyncTicker  *time.Ticker
	stopChan      chan struct{}
}

// 全局录制器实例
var globalCommRecorder *CommRecorder

// NewCommRecorder 创建录制管理器
func NewCommRecorder(config *CommSettingsConfig) *CommRecorder {
	if config == nil {
		config = &CommSettingsConfig{
			Enabled:        false,
			RetentionDays:  30,
			MinDurationMs:  500,
			MaxDurationSec: 300,
			BatchUploadSec: 10,
		}
	}

	// 创建结果通道
	resultChan := make(chan *UploadResult, 1000)

	// 创建各组件
	buffer := NewCommBuffer(config)
	uploader := NewCommUploader(config, resultChan)
	syncer := NewCommSyncer(resultChan)

	recorder := &CommRecorder{
		buffer:   buffer,
		uploader: uploader,
		syncer:   syncer,
		config:   config,
		stopChan: make(chan struct{}),
	}

	// 设置会话结束回调：将完成的会话加入上传队列
	buffer.SetOnSessionEnd(func(session *AudioSession) {
		recorder.uploader.AddToQueue(session)
	})

	return recorder
}

// Start 启动录制管理器
func (cr *CommRecorder) Start() {
	if cr == nil {
		return
	}

	cr.mu.Lock()
	if cr.running {
		cr.mu.Unlock()
		return
	}
	cr.running = true
	config := *cr.config
	cr.timeoutTicker = time.NewTicker(500 * time.Millisecond)
	uploadInterval := time.Duration(config.BatchUploadSec) * time.Second
	if uploadInterval <= 0 {
		uploadInterval = 10 * time.Second
	}
	cr.uploadTicker = time.NewTicker(uploadInterval)
	cr.dbSyncTicker = time.NewTicker(30 * time.Second)
	cr.mu.Unlock()

	cr.syncer.Start()

	go cr.runTimers()

	log.Printf("[COMM_RECORDER] 通信录制管理器已启动 (启用: %v, 最小阈值: %dms, 最大时长: %ds, 上传间隔: %ds)",
		config.Enabled, config.MinDurationMs, config.MaxDurationSec, config.BatchUploadSec)
}

// runTimers 运行定时器
func (cr *CommRecorder) runTimers() {
	cr.mu.RLock()
	timeoutTicker := cr.timeoutTicker
	uploadTicker := cr.uploadTicker
	dbSyncTicker := cr.dbSyncTicker
	stopChan := cr.stopChan
	cr.mu.RUnlock()

	for {
		select {
		case <-stopChan:
			return
		case <-timeoutTicker.C:
			cr.buffer.CheckTimeout()
		case <-uploadTicker.C:
			cr.uploader.ProcessBatch()
		case <-dbSyncTicker.C:
			cr.syncer.SyncToDatabase()
		}
	}
}

func (cr *CommRecorder) canRecord() bool {
	if cr == nil {
		return false
	}
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	return cr.running && cr.config != nil && cr.config.Enabled
}

// RecordPacket 录制音频包（在转发前调用）
// audioData 是 Opus 编码的数据，直接存储为 .raw 格式
// 注意：由于 CGO 限制，服务端不解码 Opus，直接存储原始数据
// deviceID: 持久化设备ID，正数为普通设备，幽灵设备为0
func (cr *CommRecorder) RecordPacket(
	sourceKey string,
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	audioData []byte,
) {
	if !cr.canRecord() {
		return
	}

	// 直接存储 Opus 数据（标记为 Opus 格式）
	cr.buffer.AppendPacket(sourceKey, deviceID, deviceSSID, groupID, userID, audioData)
}

// Stop 停止录制管理器
func (cr *CommRecorder) Stop() {
	if cr == nil {
		return
	}

	cr.mu.Lock()
	if !cr.running {
		cr.mu.Unlock()
		return
	}
	cr.running = false
	timeoutTicker := cr.timeoutTicker
	uploadTicker := cr.uploadTicker
	dbSyncTicker := cr.dbSyncTicker
	stopChan := cr.stopChan
	cr.mu.Unlock()

	// 停止定时器
	if timeoutTicker != nil {
		timeoutTicker.Stop()
	}
	if uploadTicker != nil {
		uploadTicker.Stop()
	}
	if dbSyncTicker != nil {
		dbSyncTicker.Stop()
	}

	close(stopChan)

	// 处理剩余数据
	cr.buffer.CheckTimeout()
	cr.uploader.ProcessBatch()
	cr.syncer.SyncToDatabase()
	cr.syncer.Stop()

	log.Println("[COMM_RECORDER] 通信录制管理器已停止")
}

// UpdateConfig 更新配置
func (cr *CommRecorder) UpdateConfig(config *CommSettingsConfig) {
	if cr == nil || config == nil {
		return
	}

	cr.mu.Lock()
	cr.config = config
	uploadTicker := cr.uploadTicker
	cr.mu.Unlock()

	cr.buffer.UpdateConfig(config)
	cr.uploader.UpdateConfig(config)

	// 更新上传间隔
	if uploadTicker != nil && config.BatchUploadSec > 0 {
		uploadTicker.Reset(time.Duration(config.BatchUploadSec) * time.Second)
	}

	log.Printf("[COMM_RECORDER] 配置已更新 (启用: %v, 最小阈值: %dms, 最大时长: %ds)",
		config.Enabled, config.MinDurationMs, config.MaxDurationSec)
}

// GetStats 获取统计信息（用于监控）
func (cr *CommRecorder) GetStats() map[string]interface{} {
	if cr == nil {
		return nil
	}
	cr.mu.RLock()
	enabled := cr.config != nil && cr.config.Enabled
	running := cr.running
	cr.mu.RUnlock()

	return map[string]interface{}{
		"enabled":           enabled,
		"running":           running,
		"active_sessions":   cr.buffer.GetActiveSessionCount(),
		"pending_uploads":   cr.uploader.GetPendingCount(),
		"pending_db_writes": cr.syncer.GetPendingCount(),
	}
}

// ==========================================
// 全局函数
// ==========================================

// InitCommRecorder 初始化全局录制器
func InitCommRecorder() {
	config := loadCommSettings()
	globalCommRecorder = NewCommRecorder(config)
	globalCommRecorder.Start()

	// 性能优化：初始化文本消息批量写入缓冲区
	InitTextMessageBuffer()
}

// StopCommRecorder 停止全局录制器
func StopCommRecorder() {
	// 性能优化：先停止文本消息缓冲区
	StopTextMessageBuffer()

	// 停止异步录制 worker 并排空队列
	stopCommRecordWorker()

	if globalCommRecorder != nil {
		globalCommRecorder.Stop()
		globalCommRecorder = nil
	}
}

func PhysicalCommRecordSourceKey(deviceID int) string {
	return fmt.Sprintf("device:%d", deviceID)
}

func GhostCommRecordSourceKey(transport string, ownerID int, ssid uint8, connectionIdentity string) string {
	return fmt.Sprintf("ghost:%s:%d:%d:%s", transport, ownerID, ssid, connectionIdentity)
}

func InterconnectCommRecordSourceKey(sessionID uint64) string {
	return fmt.Sprintf("relay-session:%d", sessionID)
}

// RecordCommPacket 录制通信数据包（全局接口，异步入队，不阻塞转发热路径）
// 传入的 audioData 是 Opus 编码数据，直接存储为 .opus 文件
// sourceKey: 运行时来源会话键；deviceID 仅用于最终持久化，幽灵设备为 0
func RecordCommPacket(
	sourceKey string,
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	audioData []byte,
) {
	if globalCommRecorder == nil || len(audioData) == 0 {
		return
	}
	// 异步录制：拷贝 payload 后投递有界队列，满则丢弃录制不堵转发
	enqueueCommRecord(sourceKey, deviceID, deviceSSID, groupID, userID, audioData)
}

// ReloadCommSettings 重新加载通信设置
func ReloadCommSettings(config *CommSettingsConfig) {
	if globalCommRecorder != nil {
		globalCommRecorder.UpdateConfig(config)
	}
}

// GetCommRecorderStats 获取录制器统计信息
func GetCommRecorderStats() map[string]interface{} {
	if globalCommRecorder != nil {
		return globalCommRecorder.GetStats()
	}
	return nil
}

// ==========================================
// 文本消息记录（直接写入数据库，不经过上传队列）
// ==========================================

// RecordTextMessage 记录文本消息到数据库
// 文本消息不需要上传 MinIO，直接写入 comm_records 表
// 使用 "text:" 前缀存储在 AudioPath 字段中
func RecordTextMessage(
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	textContent string,
) {
	// 限制文本长度（AudioPath 是 varchar(255)，按字节计算，预留 "text:" 前缀 5 字节）
	// UTF-8 编码下中文字符占 3 字节，需要按字节长度限制
	maxBytes := 250 // 255 - 5 ("text:" 前缀)
	if len(textContent) > maxBytes {
		// 截断到最大字节长度，同时确保不截断 UTF-8 字符
		for len(textContent) > maxBytes {
			textContent = textContent[:len(textContent)-1]
		}
	}

	now := time.Now()

	// 解析设备ID（幽灵设备使用负数ID，实际存储为0）
	var actualDeviceID uint
	if deviceID < 0 {
		actualDeviceID = 0
	} else {
		actualDeviceID = uint(deviceID)
	}

	record := &gormdb.CommRecord{
		DeviceID:   actualDeviceID,
		DeviceSSID: deviceSSID,
		GroupID:    groupID,
		UserID:     userID,
		StartTime:  now,
		EndTime:    now,
		DurationMs: 0,
		AudioPath:  "text:" + textContent, // 选用 text: 前缀标识文本消息
		AudioSize:  int64(len(textContent)),
		Status:     2, // 已完成（不需要上传）
	}

	// 性能优化：使用批量写入缓冲区，减少数据库压力
	BufferTextMessage(record)
}

// loadCommSettings 从数据库加载通信设置
func loadCommSettings() *CommSettingsConfig {
	repo := gormdb.GetSiteConfigRepo()
	settings, err := repo.GetCommSettingsConfig()
	if err != nil {
		log.Printf("[COMM_RECORDER] 加载通信设置失败: %v, 使用默认配置", err)
		return &CommSettingsConfig{
			Enabled:        false,
			RetentionDays:  30,
			MinDurationMs:  500,
			MaxDurationSec: 300,
			BatchUploadSec: 10,
		}
	}

	return &CommSettingsConfig{
		Enabled:        settings.Enabled,
		RetentionDays:  settings.RetentionDays,
		MinDurationMs:  settings.MinDurationMs,
		MaxDurationSec: settings.MaxDurationSec,
		BatchUploadSec: settings.BatchUploadSec,
	}
}
