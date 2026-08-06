package udphub

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type CommSenderSnapshot struct {
	Username string
	CallSign string
	Nickname string
	DevModel int
}

func (s CommSenderSnapshot) normalized() CommSenderSnapshot {
	if s.Nickname == "" {
		s.Nickname = s.CallSign
	}
	if s.Nickname == "" {
		s.Nickname = s.Username
	}
	return s
}

// AudioSession 单次通信会话及其发送时投递范围快照。
type AudioSession struct {
	SessionID        string // 文件安全的会话唯一标识
	SourceKey        string // 运行时录音来源，不持久化
	DeviceID         int    // 持久化设备ID（0表示幽灵设备）
	DeviceSSID       uint8  // 设备 SSID
	GroupID          *uint  // 群组ID
	UserID           *uint  // 用户ID
	DeliveryGroupIDs []uint // 发送时实际投递到的群组
	Sender           CommSenderSnapshot
	StartTime        time.Time     // 开始时间
	LastPacketTime   time.Time     // 最后一个包的时间（用于判断会话结束）
	Buffer           *bytes.Buffer // PCM 音频数据缓冲
	PacketCount      int           // 收到的包数量
	TotalBytes       int           // 总字节数
	mu               sync.Mutex
}

const (
	// 当前链路已切到 60ms Opus 帧，这里统一用帧数来推导录音时长。
	opusFrameDurationMs = 60
	// 与主语音链路的 PTT 结束判定保持一致，避免录音会话被过早切碎。
	commSessionGapThreshold = 600 * time.Millisecond
)

// CommBuffer 通信缓冲管理器
type CommBuffer struct {
	sessions     map[string]*AudioSession // 活跃会话 (key: runtime source key)
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

func normalizeCommRecordSourceKey(sourceKey string, deviceID int) string {
	if sourceKey != "" {
		return sourceKey
	}
	return PhysicalCommRecordSourceKey(deviceID)
}

// generateAudioSessionID hashes the runtime source so object names never leak
// network endpoints, account IDs, or client instance identifiers.
func generateAudioSessionID(sourceKey string, now time.Time) string {
	digest := sha256.Sum256([]byte(sourceKey))
	return fmt.Sprintf("%s_%s", hex.EncodeToString(digest[:12]), now.UTC().Format("20060102T150405.000000000Z"))
}

// parseMergedFrames 解析合并帧格式
// 格式：[Frame1 Length(2B, 大端序)][Frame1 Data][Frame2 Length(2B)][Frame2 Data]
// 兼容单帧格式（无长度前缀，直接返回原始数据）
func parseMergedFrames(data []byte) [][]byte {
	// 如果数据太短，不可能是合并帧格式
	if len(data) < 3 {
		return [][]byte{data}
	}

	var frames [][]byte
	offset := 0

	for offset+2 <= len(data) {
		// 读取帧长度（大端序）
		frameLength := int(data[offset])<<8 | int(data[offset+1])

		// 安全检查：帧长度必须合理
		// Opus 帧通常不超过 1000 字节
		if frameLength == 0 || frameLength > 1000 || offset+2+frameLength > len(data) {
			// 不是合并帧格式，当作单帧处理
			if offset == 0 {
				return [][]byte{data}
			}
			break
		}

		// 提取帧数据
		frames = append(frames, data[offset+2:offset+2+frameLength])
		offset += 2 + frameLength
	}

	// 如果没有解析出任何帧，返回原始数据作为单帧
	if len(frames) == 0 {
		return [][]byte{data}
	}

	return frames
}

// AppendPacket 追加音频数据包。
// pcmData 可能是合并帧格式（包含多个 Opus 子帧），需要先解析再存储
// sourceKey: 运行时来源会话键；deviceID: 持久化设备ID，幽灵设备为0
func (cb *CommBuffer) AppendPacket(
	sourceKey string,
	deviceID int,
	deviceSSID uint8,
	groupID *uint,
	userID *uint,
	sender CommSenderSnapshot,
	deliveryGroupIDs []uint,
	pcmData []byte,
) {
	if cb == nil || !cb.config.Enabled {
		return
	}

	sessionKey := normalizeCommRecordSourceKey(sourceKey, deviceID)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	session, exists := cb.sessions[sessionKey]
	now := time.Now()

	// 判断是否是新会话。60ms/120ms/240ms 大包模式下，200ms 容错太低，会把一次发言切成很多段。
	if !exists || now.Sub(session.LastPacketTime) > commSessionGapThreshold ||
		!sameDeliveryGroupSnapshot(session.DeliveryGroupIDs, deliveryGroupIDs) {
		// 关闭旧会话
		if exists {
			cb.finalizeSession(session)
			delete(cb.sessions, sessionKey)
		}
		// 创建新会话
		session = &AudioSession{
			SessionID:        generateAudioSessionID(sessionKey, now),
			SourceKey:        sessionKey,
			DeviceID:         deviceID,
			DeviceSSID:       deviceSSID,
			GroupID:          groupID,
			UserID:           userID,
			DeliveryGroupIDs: append([]uint(nil), deliveryGroupIDs...),
			Sender:           sender,
			StartTime:        now,
			LastPacketTime:   now,
			Buffer:           bytes.NewBuffer(nil),
		}
		cb.sessions[sessionKey] = session
	}

	// 解析合并帧格式，提取所有子帧
	subFrames := parseMergedFrames(pcmData)

	// 逐个写入子帧（带帧长度前缀：2字节 little-endian + Opus 数据）
	session.mu.Lock()
	for _, frame := range subFrames {
		// 写入帧长度前缀（2字节，little-endian）
		lenBuf := make([]byte, 2)
		binary.LittleEndian.PutUint16(lenBuf, uint16(len(frame)))
		session.Buffer.Write(lenBuf)
		// 写入 Opus 帧数据
		session.Buffer.Write(frame)
		session.PacketCount++
		session.TotalBytes += len(frame) + 2 // 包含长度前缀
	}
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

	durationMs := estimateSessionDurationMs(session)

	// 检查最小时长阈值
	if durationMs < cb.config.MinDurationMs {
		return // 太短，丢弃
	}

	// 回调处理
	if cb.onSessionEnd != nil {
		// 复制会话数据，避免后续修改影响
		sessionCopy := &AudioSession{
			SessionID:        session.SessionID,
			SourceKey:        session.SourceKey,
			DeviceID:         session.DeviceID,
			DeviceSSID:       session.DeviceSSID,
			GroupID:          session.GroupID,
			UserID:           session.UserID,
			DeliveryGroupIDs: append([]uint(nil), session.DeliveryGroupIDs...),
			Sender:           session.Sender,
			StartTime:        session.StartTime,
			LastPacketTime:   session.LastPacketTime,
			Buffer:           bytes.NewBuffer(session.Buffer.Bytes()),
			PacketCount:      session.PacketCount,
			TotalBytes:       session.TotalBytes,
		}
		go cb.onSessionEnd(sessionCopy)
	}
}

func sameDeliveryGroupSnapshot(left, right []uint) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
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
		// 与新会话判定阈值保持一致，避免录音器先于主链路截断。
		if now.Sub(session.LastPacketTime) > commSessionGapThreshold {
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

func estimateSessionDurationMs(session *AudioSession) int {
	if session == nil {
		return 0
	}
	if session.PacketCount > 0 {
		return session.PacketCount * opusFrameDurationMs
	}
	if session.LastPacketTime.After(session.StartTime) {
		return int(session.LastPacketTime.Sub(session.StartTime).Milliseconds())
	}
	return 0
}
