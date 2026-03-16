package udphub

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// AudioSession 单次通信会话（精简版，只保留 ID）
type AudioSession struct {
	SessionID      string        // 会话唯一标识 (DeviceID_Timestamp)
	DeviceID       uint          // 设备ID
	DeviceSSID     uint8         // 设备 SSID
	GroupID        *uint         // 群组ID
	UserID         *uint         // 用户ID
	StartTime      time.Time     // 开始时间
	LastPacketTime time.Time     // 最后一个包的时间（用于判断会话结束）
	Buffer         *bytes.Buffer // PCM 音频数据缓冲
	PacketCount    int           // 收到的包数量
	TotalBytes     int           // 总字节数
	mu             sync.Mutex
}

// CommBuffer 通信缓冲管理器
type CommBuffer struct {
	sessions     map[string]*AudioSession // 活跃会话 (key: deviceID)
	mu           sync.RWMutex
	config       *CommSettingsConfig // 通信配置
	onSessionEnd func(*AudioSession) // 会话结束回调
}

// CommSettingsConfig 通信设置配置
type CommSettingsConfig struct {
	Enabled        bool // 是否启用通信记录
	RetentionDays  int  // 数据保留天数
	MinDurationMs  int  // 最小录制阈值（毫秒）
	MaxDurationSec int  // 最大录制时长（秒），0=不限制
	BatchUploadSec int  // 批量上传间隔（秒）
}

// NewCommBuffer 创建通信缓冲管理器
func NewCommBuffer(config *CommSettingsConfig) *CommBuffer {
	return &CommBuffer{
		sessions: make(map[string]*AudioSession),
		config:   config,
	}
}

// generateSessionID 生成会话ID
func generateSessionID(deviceID uint) string {
	return fmt.Sprintf("%d", deviceID)
}

// AppendPacket 追加音频数据包（精简版，只记录 ID）
func (cb *CommBuffer) AppendPacket(
	deviceID uint,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	pcmData []byte,
) {
	if cb == nil || !cb.config.Enabled {
		return
	}

	sessionKey := generateSessionID(deviceID)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	session, exists := cb.sessions[sessionKey]
	now := time.Now()

	// 判断是否是新会话（间隔超过 200ms 视为新会话，与 PTT 检测逻辑一致）
	if !exists || now.Sub(session.LastPacketTime) > 200*time.Millisecond {
		// 关闭旧会话
		if exists {
			cb.finalizeSession(session)
			delete(cb.sessions, sessionKey)
		}
		// 创建新会话
		session = &AudioSession{
			SessionID:      fmt.Sprintf("%d_%s", deviceID, now.Format("20060102_150405.000")),
			DeviceID:       deviceID,
			DeviceSSID:     deviceSSID,
			GroupID:        groupID,
			UserID:         userID,
			StartTime:      now,
			LastPacketTime: now,
			Buffer:         bytes.NewBuffer(nil),
		}
		cb.sessions[sessionKey] = session
	}

	// 追加数据（带帧长度前缀：2字节 little-endian + Opus 数据）
	session.mu.Lock()
	// 写入帧长度前缀（2字节，little-endian）
	lenBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(lenBuf, uint16(len(pcmData)))
	session.Buffer.Write(lenBuf)
	// 写入 Opus 帧数据
	session.Buffer.Write(pcmData)
	session.PacketCount++
	session.TotalBytes += len(pcmData) + 2 // 包含长度前缀
	session.LastPacketTime = now
	session.mu.Unlock()

	// 检查最大时长限制
	if cb.config.MaxDurationSec > 0 {
		elapsed := now.Sub(session.StartTime).Seconds()
		if elapsed >= float64(cb.config.MaxDurationSec) {
			cb.finalizeSession(session)
			delete(cb.sessions, sessionKey)
		}
	}
}

// finalizeSession 完成会话，加入上传队列（调用前必须持有锁）
func (cb *CommBuffer) finalizeSession(session *AudioSession) {
	session.mu.Lock()
	defer session.mu.Unlock()

	// 使用最后一个数据包时间计算时长，与数据库保存逻辑一致
	durationMs := int(session.LastPacketTime.Sub(session.StartTime).Milliseconds())

	// 检查最小时长阈值
	if durationMs < cb.config.MinDurationMs {
		return // 太短，丢弃
	}

	// 回调处理
	if cb.onSessionEnd != nil {
		// 复制会话数据，避免后续修改影响
		sessionCopy := &AudioSession{
			SessionID:      session.SessionID,
			DeviceID:       session.DeviceID,
			DeviceSSID:     session.DeviceSSID,
			GroupID:        session.GroupID,
			UserID:         session.UserID,
			StartTime:      session.StartTime,
			LastPacketTime: session.LastPacketTime,
			Buffer:         bytes.NewBuffer(session.Buffer.Bytes()),
			PacketCount:    session.PacketCount,
			TotalBytes:     session.TotalBytes,
		}
		go cb.onSessionEnd(sessionCopy)
	}
}

// CheckTimeout 检查超时会话（由定时器调用）
func (cb *CommBuffer) CheckTimeout() {
	if cb == nil {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	for key, session := range cb.sessions {
		// 超过 500ms 没有新数据包，视为会话结束
		if now.Sub(session.LastPacketTime) > 500*time.Millisecond {
			cb.finalizeSession(session)
			delete(cb.sessions, key)
		}
	}
}

// SetOnSessionEnd 设置会话结束回调
func (cb *CommBuffer) SetOnSessionEnd(callback func(*AudioSession)) {
	if cb != nil {
		cb.onSessionEnd = callback
	}
}

// GetActiveSessionCount 获取活跃会话数量（用于监控）
func (cb *CommBuffer) GetActiveSessionCount() int {
	if cb == nil {
		return 0
	}
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.sessions)
}

// UpdateConfig 更新配置
func (cb *CommBuffer) UpdateConfig(config *CommSettingsConfig) {
	if cb != nil {
		cb.mu.Lock()
		defer cb.mu.Unlock()
		cb.config = config
	}
}
