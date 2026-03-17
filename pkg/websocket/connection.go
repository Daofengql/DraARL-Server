package websocket

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"nrllink/internal/models"
)

// ConnectionState 连接状态
type ConnectionState int

const (
	StateDisconnected   ConnectionState = iota // 已断开
	StateConnecting                            // 连接中
	StateAuthenticating                        // 认证中
	StateOnline                                // 在线
	StateDisconnecting                         // 断开中
	StateReconnecting                          // 重连中
)

// String 返回连接状态的字符串表示
func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateAuthenticating:
		return "Authenticating"
	case StateOnline:
		return "Online"
	case StateDisconnecting:
		return "Disconnecting"
	case StateReconnecting:
		return "Reconnecting"
	default:
		return "Unknown"
	}
}

// DeviceType 设备类型
type DeviceType int

const (
	DeviceTypeNormal DeviceType = iota // 普通设备（需要设备密码认证）
	DeviceTypeGhost                    // 幽灵设备（Web 端 JWT 认证）
)

// String 返回设备类型的字符串表示
func (t DeviceType) String() string {
	switch t {
	case DeviceTypeNormal:
		return "Normal"
	case DeviceTypeGhost:
		return "Ghost"
	default:
		return "Unknown"
	}
}

// WSDevice WebSocket 设备信息
type WSDevice struct {
	// 连接信息
	Conn           *websocket.Conn
	ConnState      ConnectionState
	ConnectTime    time.Time
	LastPacketTime time.Time

	// 设备类型
	DeviceType DeviceType

	// 普通设备信息（设备密码认证）
	Username       string
	SSID           byte
	DevicePassword string
	DeviceID       int // 数据库中的设备 ID

	// 幽灵设备信息（JWT 认证）
	UserID   int    // 用户 ID
	CallSign string // 呼号
	Nickname string // 昵称

	// 当前群组
	GroupID int

	// 设备状态
	DevModel    byte
	IsOnline    bool
	DisableSend bool
	DisableRecv bool

	// 统计信息
	Traffic     int64
	VoiceTime   int64
	PacketCount int64

	// 重连相关
	IsReconnecting      bool
	ReconnectCount      int
	LastDisconnectTime  time.Time
	PendingReconnect    bool
	ConnectionStartTime time.Time

	// 语音状态
	IsSendingVoice   bool
	IsReceivingVoice bool
	LastVoiceTime    time.Time
}

// GetIdentifier 获取设备唯一标识
func (d *WSDevice) GetIdentifier() string {
	if d.DeviceType == DeviceTypeGhost {
		return fmt.Sprintf("ghost-%d", d.UserID)
	}
	return fmt.Sprintf("%s-%d", d.Username, d.SSID)
}

// GetCallSignSSID 获取呼号-SSID
func (d *WSDevice) GetCallSignSSID() string {
	return fmt.Sprintf("%s-%d", d.CallSign, d.SSID)
}

// GetGroupID 获取当前群组 ID
func (d *WSDevice) GetGroupID() int {
	return d.GroupID
}

// IsGhost 检查是否是幽灵设备
func (d *WSDevice) IsGhost() bool {
	return d.DeviceType == DeviceTypeGhost
}

// GetUserID 获取用户 ID
func (d *WSDevice) GetUserID() int {
	return d.UserID
}

// GetDeviceID 获取设备 ID
func (d *WSDevice) GetDeviceID() int {
	return d.DeviceID
}

// GetUsername 获取用户名
func (d *WSDevice) GetUsername() string {
	return d.Username
}

// GetCallSign 获取呼号
func (d *WSDevice) GetCallSign() string {
	return d.CallSign
}

// GetSSID 获取 SSID
func (d *WSDevice) GetSSID() byte {
	return d.SSID
}

// GetDevModel 获取设备型号
func (d *WSDevice) GetDevModel() byte {
	return d.DevModel
}

// IsDisabledRecv 检查是否禁收
func (d *WSDevice) IsDisabledRecv() bool {
	return d.DisableRecv
}

// WSConnectionManager WebSocket 连接管理器
type WSConnectionManager struct {
	// 普通设备连接 (key: username-ssid)
	normalDevices map[string]*WSDevice
	// 幽灵设备连接 (key: userID)
	ghostDevices map[int]*WSDevice
	// 连接索引 (key: conn.RemoteAddr().String())
	connMap map[string]*WSDevice

	// ==========================================
	// 性能优化：按群组索引的设备列表
	// 将 GetDevicesByGroup 从 O(n) 优化到 O(1)
	// ==========================================
	groupDevices map[int]map[string]*WSDevice // groupID -> deviceKey -> device

	// 读写锁
	mu sync.RWMutex

	// 配置
	AuthTimeout      time.Duration // 认证超时
	HeartbeatTimeout time.Duration // 心跳超时
	ReconnectGrace   time.Duration // 重连宽限期
	ProxyTimeout     time.Duration // 反向代理超时
	PreReconnectTime time.Duration // 预重连时间（在代理超时前多久开始准备重连）
}

// NewWSConnectionManager 创建新的连接管理器
func NewWSConnectionManager() *WSConnectionManager {
	return &WSConnectionManager{
		normalDevices:    make(map[string]*WSDevice),
		ghostDevices:     make(map[int]*WSDevice),
		connMap:          make(map[string]*WSDevice),
		groupDevices:     make(map[int]map[string]*WSDevice), // 初始化群组索引
		AuthTimeout:      30 * time.Second,                   // 30 秒认证超时
		HeartbeatTimeout: 20 * time.Second,                   // 20 秒心跳超时
		ReconnectGrace:   30 * time.Second,                   // 30 秒重连宽限期
		ProxyTimeout:     300 * time.Second,                  // 300 秒反向代理超时
		PreReconnectTime: 240 * time.Second,                  // 240 秒开始准备重连
	}
}

// ==========================================
// 性能优化：群组索引辅助方法
// ==========================================

// addToGroupIndex 将设备添加到群组索引（调用前必须持有锁）
func (m *WSConnectionManager) addToGroupIndex(groupID int, key string, device *WSDevice) {
	if m.groupDevices[groupID] == nil {
		m.groupDevices[groupID] = make(map[string]*WSDevice)
	}
	m.groupDevices[groupID][key] = device
}

// removeFromGroupIndex 从群组索引中移除设备（调用前必须持有锁）
func (m *WSConnectionManager) removeFromGroupIndex(groupID int, key string) {
	if devices, ok := m.groupDevices[groupID]; ok {
		delete(devices, key)
		// 如果群组为空，清理 map 以节省内存
		if len(devices) == 0 {
			delete(m.groupDevices, groupID)
		}
	}
}

// getDeviceKey 获取设备的唯一键
func getDeviceKey(device *WSDevice) string {
	if device.DeviceType == DeviceTypeGhost {
		return fmt.Sprintf("ghost-%d", device.UserID)
	}
	return fmt.Sprintf("%s-%d", device.Username, device.SSID)
}

// RegisterConnection 注册新连接
func (m *WSConnectionManager) RegisterConnection(conn *websocket.Conn) *WSDevice {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 【前置逻辑说明】
	// 统一跨协议的默认群组 ID。
	// UDP 终端上报 0 时，服务端底层逻辑会将其定向到 models.GroupIDPublicMin (即 999)。
	// 此处必须将 WS 端刚建立连接时的默认 GroupID 也设定为 999，
	// 以确保两端在未进行主动切组操作的初始状态下，处于同一个广播域内。
	device := &WSDevice{
		Conn:                conn,
		ConnState:           StateConnecting,
		ConnectTime:         time.Now(),
		LastPacketTime:      time.Now(),
		ConnectionStartTime: time.Now(),
		GroupID:             models.GroupIDPublicMin, // 【核心修改】与 UDP 默认群组保持一致 (999)
	}

	addr := conn.RemoteAddr().String()
	m.connMap[addr] = device

	log.Printf("[WS] New connection registered: %s", addr)
	return device
}

// SetDeviceAuthenticating 设置设备为认证中状态
func (m *WSConnectionManager) SetDeviceAuthenticating(device *WSDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	device.ConnState = StateAuthenticating
	device.LastPacketTime = time.Now()
}

// RegisterNormalDevice 注册普通设备（设备密码认证成功后调用）
func (m *WSConnectionManager) RegisterNormalDevice(device *WSDevice, username string, ssid byte, deviceID int, callsign string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	device.DeviceType = DeviceTypeNormal
	device.Username = username
	device.SSID = ssid
	device.DeviceID = deviceID
	device.CallSign = callsign
	device.ConnState = StateOnline
	device.IsOnline = true
	device.LastPacketTime = time.Now()

	key := fmt.Sprintf("%s-%d", username, ssid)
	m.normalDevices[key] = device

	// 添加到群组索引
	m.addToGroupIndex(device.GroupID, key, device)

	log.Printf("[WS] Normal device registered: %s (ID: %d, CallSign: %s)", key, deviceID, callsign)
}

// RegisterGhostDevice 注册幽灵设备（JWT 认证成功后调用）
func (m *WSConnectionManager) RegisterGhostDevice(device *WSDevice, userID int, callsign, nickname string, ssid byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	device.DeviceType = DeviceTypeGhost
	device.UserID = userID
	device.CallSign = callsign
	device.Nickname = nickname
	device.SSID = ssid
	device.DevModel = 105 // DraARLDevModelBrowser
	device.ConnState = StateOnline
	device.IsOnline = true
	device.LastPacketTime = time.Now()

	// 幽灵设备的 "Username" 使用用户呼号
	device.Username = callsign

	m.ghostDevices[userID] = device

	// 添加到群组索引
	key := getDeviceKey(device)
	m.addToGroupIndex(device.GroupID, key, device)

	log.Printf("[WS] Ghost device registered: user-%d (%s-%d)", userID, callsign, ssid)
}

// UnregisterDevice 注销设备
func (m *WSConnectionManager) UnregisterDevice(device *WSDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()

	addr := device.Conn.RemoteAddr().String()

	// 【关键修复：防并发覆盖】
	// 只有当 connMap 中的指针与当前尝试注销的设备指针一致时，才执行删除。
	// 防止旧连接在超时退出时，误删了同 IP 刚刚建立的新连接。
	if existing, ok := m.connMap[addr]; ok && existing == device {
		delete(m.connMap, addr)
	}

	// 获取设备键用于群组索引清理
	key := getDeviceKey(device)

	if device.DeviceType == DeviceTypeGhost {
		// 【关键修复：指针比对】防止旧的 Ghost 连接超时清理掉新的 Ghost 连接
		if existing, ok := m.ghostDevices[device.UserID]; ok && existing == device {
			delete(m.ghostDevices, device.UserID)
			// 从群组索引中移除
			m.removeFromGroupIndex(device.GroupID, key)
			log.Printf("[WS] Ghost device unregistered: user-%d", device.UserID)
		}
	} else if device.Username != "" {
		deviceKey := fmt.Sprintf("%s-%d", device.Username, device.SSID)
		// 【关键修复：指针比对】
		if existing, ok := m.normalDevices[deviceKey]; ok && existing == device {
			delete(m.normalDevices, deviceKey)
			// 从群组索引中移除
			m.removeFromGroupIndex(device.GroupID, key)
			log.Printf("[WS] Normal device unregistered: %s", deviceKey)
		}
	}

	device.IsOnline = false
	device.ConnState = StateDisconnected
}

// GetDeviceByConn 通过连接获取设备
func (m *WSConnectionManager) GetDeviceByConn(conn *websocket.Conn) (*WSDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	addr := conn.RemoteAddr().String()
	device, exists := m.connMap[addr]
	return device, exists
}

// GetNormalDevice 获取普通设备
func (m *WSConnectionManager) GetNormalDevice(username string, ssid byte) (*WSDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s-%d", username, ssid)
	device, exists := m.normalDevices[key]
	return device, exists
}

// GetGhostDevice 获取幽灵设备
func (m *WSConnectionManager) GetGhostDevice(userID int) (*WSDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	device, exists := m.ghostDevices[userID]
	return device, exists
}

// GetAllOnlineDevices 获取所有在线设备
func (m *WSConnectionManager) GetAllOnlineDevices() []*WSDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*WSDevice, 0)

	for _, device := range m.normalDevices {
		if device.IsOnline {
			devices = append(devices, device)
		}
	}

	for _, device := range m.ghostDevices {
		if device.IsOnline {
			devices = append(devices, device)
		}
	}

	return devices
}

// GetDevicesByGroup 获取指定群组的在线设备
// 性能优化：使用群组索引，从 O(n) 优化到 O(1)
func (m *WSConnectionManager) GetDevicesByGroup(groupID int) []*WSDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 直接从群组索引获取，O(1) 复杂度
	groupDevs, ok := m.groupDevices[groupID]
	if !ok {
		return []*WSDevice{}
	}

	devices := make([]*WSDevice, 0, len(groupDevs))
	for _, device := range groupDevs {
		if device.IsOnline {
			devices = append(devices, device)
		}
	}

	return devices
}

// GetOnlineCount 获取在线设备数量
func (m *WSConnectionManager) GetOnlineCount() (normalCount, ghostCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, device := range m.normalDevices {
		if device.IsOnline {
			normalCount++
		}
	}

	for _, device := range m.ghostDevices {
		if device.IsOnline {
			ghostCount++
		}
	}

	return
}

// GetTotalCount 获取总连接数
func (m *WSConnectionManager) GetTotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connMap)
}

// UpdateDeviceActivity 更新设备活动时间
func (m *WSConnectionManager) UpdateDeviceActivity(device *WSDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	device.LastPacketTime = time.Now()
}

// SetDeviceGroup 设置设备群组
func (m *WSConnectionManager) SetDeviceGroup(device *WSDevice, groupID int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果群组没有变化，直接返回
	if device.GroupID == groupID {
		return
	}

	// 从旧群组索引中移除
	key := getDeviceKey(device)
	m.removeFromGroupIndex(device.GroupID, key)

	// 更���群组 ID
	device.GroupID = groupID

	// 添加到新群组索引
	m.addToGroupIndex(groupID, key, device)

	log.Printf("[WS] Device %s changed to group %d", device.GetIdentifier(), groupID)
}

// CheckAuthTimeout 检查认证超时
func (m *WSConnectionManager) CheckAuthTimeout(device *WSDevice) bool {
	if device.ConnState != StateAuthenticating && device.ConnState != StateConnecting {
		return false
	}

	return time.Since(device.ConnectTime) > m.AuthTimeout
}

// CheckHeartbeatTimeout 检查心跳超时
func (m *WSConnectionManager) CheckHeartbeatTimeout(device *WSDevice) bool {
	if device.ConnState != StateOnline {
		return false
	}

	return time.Since(device.LastPacketTime) > m.HeartbeatTimeout
}

// ShouldPrepareReconnect 检查是否应该准备重连
func (m *WSConnectionManager) ShouldPrepareReconnect(device *WSDevice) bool {
	if device.ConnState != StateOnline {
		return false
	}

	elapsed := time.Since(device.ConnectionStartTime)
	return elapsed >= m.PreReconnectTime
}

// IsVoiceActive 检查语音是否活跃（200ms 内有语音活动）
func (m *WSConnectionManager) IsVoiceActive(device *WSDevice) bool {
	return time.Since(device.LastVoiceTime) < 200*time.Millisecond
}

// MarkVoiceSending 标记正在发送语音
func (m *WSConnectionManager) MarkVoiceSending(device *WSDevice, sending bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	device.IsSendingVoice = sending
	if sending {
		device.LastVoiceTime = time.Now()
	}
}

// MarkVoiceReceiving 标记正在接收语音
func (m *WSConnectionManager) MarkVoiceReceiving(device *WSDevice, receiving bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	device.IsReceivingVoice = receiving
	if receiving {
		device.LastVoiceTime = time.Now()
	}
}

// BroadcastToGroup 广播消息到群组（排除发送者）
func (m *WSConnectionManager) BroadcastToGroup(groupID int, sender *WSDevice, data []byte, messageType int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error

	for _, device := range m.normalDevices {
		if device.IsOnline && device.GroupID == groupID && device != sender {
			if device.DisableRecv {
				continue
			}
			if err := device.Conn.WriteMessage(messageType, data); err != nil {
				errs = append(errs, err)
			} else {
				device.Traffic += int64(len(data))
			}
		}
	}

	for _, device := range m.ghostDevices {
		if device.IsOnline && device.GroupID == groupID && device != sender {
			if device.DisableRecv {
				continue
			}
			if err := device.Conn.WriteMessage(messageType, data); err != nil {
				errs = append(errs, err)
			} else {
				device.Traffic += int64(len(data))
			}
		}
	}

	if len(errs) > 0 {
		return errors.New("some broadcasts failed")
	}
	return nil
}

// SendToDevice 发送消息到指定设备
func (m *WSConnectionManager) SendToDevice(device *WSDevice, data []byte, messageType int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !device.IsOnline {
		return errors.New("device is offline")
	}

	err := device.Conn.WriteMessage(messageType, data)
	if err == nil {
		device.Traffic += int64(len(data))
	}
	return err
}
