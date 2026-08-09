package udphub

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"draarl/pkg/storage"
)

// PendingUpload 待上传项
type PendingUpload struct {
	Session   *AudioSession
	CreatedAt time.Time
}

// UploadResult 上传结果
type UploadResult struct {
	Session   *AudioSession
	AudioPath string
	AudioSize int64
	Error     error
}

// CommUploader 批量上传器
type CommUploader struct {
	pendingQueue []*PendingUpload
	resultChan   chan *UploadResult
	mu           sync.Mutex
	config       *CommSettingsConfig
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewCommUploader 创建批量上传器
func NewCommUploader(config *CommSettingsConfig, resultChan chan *UploadResult) *CommUploader {
	ctx, cancel := context.WithCancel(context.Background())
	return &CommUploader{
		pendingQueue: make([]*PendingUpload, 0),
		resultChan:   resultChan,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// AddToQueue 添加到上传队列
func (cu *CommUploader) AddToQueue(session *AudioSession) {
	if cu == nil {
		return
	}

	cu.mu.Lock()
	defer cu.mu.Unlock()

	cu.pendingQueue = append(cu.pendingQueue, &PendingUpload{
		Session:   session,
		CreatedAt: time.Now(),
	})
}

// ProcessBatch 处理批量上传（由定时器调用）
func (cu *CommUploader) ProcessBatch() {
	if cu == nil {
		return
	}

	cu.mu.Lock()
	if len(cu.pendingQueue) == 0 {
		cu.mu.Unlock()
		return
	}

	batch := cu.pendingQueue
	cu.pendingQueue = make([]*PendingUpload, 0)
	cu.mu.Unlock()

	log.Printf("[COMM_UPLOADER] 开始批量上传 %d 个音频会话", len(batch))

	successCount := 0
	failCount := 0

	for _, item := range batch {
		audioPath, audioSize, err := cu.uploadAudio(item.Session)

		if cu.resultChan != nil {
			cu.resultChan <- &UploadResult{
				Session:   item.Session,
				AudioPath: audioPath,
				AudioSize: audioSize,
				Error:     err,
			}
		}

		if err != nil {
			log.Printf("[COMM_UPLOADER] 上传失败: %s, 错误: %v", item.Session.SessionID, err)
			failCount++
		} else {
			successCount++
		}
	}

	if successCount > 0 || failCount > 0 {
		log.Printf("[COMM_UPLOADER] 批量上传完成: 成功 %d, 失败 %d", successCount, failCount)
	}
}

// RawOpusHeader 原始 Opus 数据文件头格式
type RawOpusHeader struct {
	Magic      [4]byte
	Version    uint16
	SampleRate uint32
	Channels   uint16
	FrameSize  uint16
	FrameCount uint32
	Reserved   [6]byte
}

// uploadAudio 上传单个音频文件
func (cu *CommUploader) uploadAudio(session *AudioSession) (string, int64, error) {
	if !storage.IsEnabled() {
		return "", 0, fmt.Errorf("存储未初始化")
	}

	session.mu.Lock()
	opusData := session.Buffer.Bytes()
	frameCount := session.PacketCount
	session.mu.Unlock()

	if len(opusData) == 0 {
		return "", 0, fmt.Errorf("音频数据为空")
	}

	now := time.Now()
	objectName := fmt.Sprintf("comm-records/%d/%02d/%d/%s.raw",
		now.Year(), now.Month(), now.Day(), session.SessionID)

	header := RawOpusHeader{
		Version:    1,
		SampleRate: 16000,
		Channels:   1,
		FrameSize:  960,
		FrameCount: uint32(frameCount),
	}
	copy(header.Magic[:], []byte("OPUS"))

	headerBytes := make([]byte, 24)
	copy(headerBytes[0:4], header.Magic[:])
	binary.LittleEndian.PutUint16(headerBytes[4:6], header.Version)
	binary.LittleEndian.PutUint32(headerBytes[6:10], header.SampleRate)
	binary.LittleEndian.PutUint16(headerBytes[10:12], header.Channels)
	binary.LittleEndian.PutUint16(headerBytes[12:14], header.FrameSize)
	binary.LittleEndian.PutUint32(headerBytes[14:18], header.FrameCount)

	rawData := make([]byte, len(headerBytes)+len(opusData))
	copy(rawData[:len(headerBytes)], headerBytes)
	copy(rawData[len(headerBytes):], opusData)

	if err := storage.Put(cu.ctx, objectName, bytes.NewReader(rawData), int64(len(rawData)), "application/octet-stream"); err != nil {
		return "", 0, fmt.Errorf("上传音频失败: %w", err)
	}

	log.Printf("[COMM_UPLOADER] 上传成功: %s (大小: %d 字节, 帧数: %d)",
		objectName, len(rawData), frameCount)

	return objectName, int64(len(rawData)), nil
}

// GetPendingCount 获取待上传数量
func (cu *CommUploader) GetPendingCount() int {
	if cu == nil {
		return 0
	}
	cu.mu.Lock()
	defer cu.mu.Unlock()
	return len(cu.pendingQueue)
}

// UpdateConfig 更新配置
func (cu *CommUploader) UpdateConfig(config *CommSettingsConfig) {
	if cu != nil {
		cu.mu.Lock()
		defer cu.mu.Unlock()
		cu.config = config
	}
}

// Stop 停止上传器
func (cu *CommUploader) Stop() {
	if cu != nil && cu.cancel != nil {
		cu.cancel()
	}
}
