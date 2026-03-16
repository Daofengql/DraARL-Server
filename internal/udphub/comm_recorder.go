package udphub

import (
	"log"
	"sync"
	"time"

	"nrllink/internal/gormdb"
)

// CommRecorder 通信录制管理器（整合所有组件）
type CommRecorder struct {
	buffer   *CommBuffer
	uploader *CommUploader
	syncer   *CommSyncer
	config   *CommSettingsConfig
	running  bool
	mu       sync.Mutex

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
	if cr == nil || !cr.config.Enabled {
		log.Println("[COMM_RECORDER] 通信录制功能未启用")
		return
	}

	cr.running = true
	cr.syncer.Start()

	// 启动定时器
	go cr.runTimers()

	log.Printf("[COMM_RECORDER] 通信录制管理器已启动 (最小阈值: %dms, 最大时长: %ds, 上传间隔: %ds)",
		cr.config.MinDurationMs, cr.config.MaxDurationSec, cr.config.BatchUploadSec)
}

// runTimers 运行定时器
func (cr *CommRecorder) runTimers() {
	// 会话超时检查：每 500ms
	cr.timeoutTicker = time.NewTicker(500 * time.Millisecond)

	// 批量上传间隔（可配置）
	uploadInterval := time.Duration(cr.config.BatchUploadSec) * time.Second
	if uploadInterval <= 0 {
		uploadInterval = 10 * time.Second
	}
	cr.uploadTicker = time.NewTicker(uploadInterval)

	// 数据库同步：每 30 秒
	cr.dbSyncTicker = time.NewTicker(30 * time.Second)

	for {
		select {
		case <-cr.stopChan:
			return
		case <-cr.timeoutTicker.C:
			cr.buffer.CheckTimeout()
		case <-cr.uploadTicker.C:
			cr.uploader.ProcessBatch()
		case <-cr.dbSyncTicker.C:
			cr.syncer.SyncToDatabase()
		}
	}
}

// RecordPacket 录制音频包（在转发前调用）
// audioData 是 Opus 编码的数据，直接存储为 .raw 格式
// 注意：由于 CGO 限制，服务端不解码 Opus，直接存储原始数据
// deviceID: 设备ID，正数为普通设备，负数为幽灵设备
func (cr *CommRecorder) RecordPacket(
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	audioData []byte,
) {
	if cr == nil || !cr.running || !cr.config.Enabled {
		return
	}

	// 直接存储 Opus 数据（标记为 Opus 格式）
	cr.buffer.AppendPacket(deviceID, deviceSSID, groupID, userID, audioData)
}

// Stop 停止录制管理器
func (cr *CommRecorder) Stop() {
	if cr == nil {
		return
	}

	cr.running = false

	// 停止定时器
	if cr.timeoutTicker != nil {
		cr.timeoutTicker.Stop()
	}
	if cr.uploadTicker != nil {
		cr.uploadTicker.Stop()
	}
	if cr.dbSyncTicker != nil {
		cr.dbSyncTicker.Stop()
	}

	close(cr.stopChan)

	// 处理剩余数据
	cr.buffer.CheckTimeout()
	cr.uploader.ProcessBatch()
	cr.syncer.SyncToDatabase()
	cr.syncer.Stop()

	log.Println("[COMM_RECORDER] 通信录制管理器已停止")
}

// UpdateConfig 更新配置
func (cr *CommRecorder) UpdateConfig(config *CommSettingsConfig) {
	if cr == nil {
		return
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()

	cr.config = config
	cr.buffer.UpdateConfig(config)
	cr.uploader.UpdateConfig(config)

	// 更新上传间隔
	if cr.uploadTicker != nil && config.BatchUploadSec > 0 {
		cr.uploadTicker.Reset(time.Duration(config.BatchUploadSec) * time.Second)
	}

	log.Printf("[COMM_RECORDER] 配置已更新 (启用: %v, 最小阈值: %dms, 最大时长: %ds)",
		config.Enabled, config.MinDurationMs, config.MaxDurationSec)
}

// GetStats 获取统计信息（用于监控）
func (cr *CommRecorder) GetStats() map[string]interface{} {
	if cr == nil {
		return nil
	}

	return map[string]interface{}{
		"enabled":           cr.config.Enabled,
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
}

// StopCommRecorder 停止全局录制器
func StopCommRecorder() {
	if globalCommRecorder != nil {
		globalCommRecorder.Stop()
		globalCommRecorder = nil
	}
}

// RecordCommPacket 录制通信数据包（全局接口）
// 传入的 audioData 是 Opus 编码数据，直接存储为 .opus 文件
// deviceID: 设备ID，正数为普通设备，负数为幽灵设备
func RecordCommPacket(
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	audioData []byte,
) {
	if globalCommRecorder != nil {
		globalCommRecorder.RecordPacket(deviceID, deviceSSID, groupID, userID, audioData)
	}
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
