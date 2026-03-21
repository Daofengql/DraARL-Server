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
	StateDisconnected ConnectionState = iota // 已断开
	StateConnecting                          // 连接中
	StateAuthenticating                      // 认证中
	StateOnline                              // 在线
	StateDisconnecting                       // 断开中
	StateReconnecting                        // 重连中
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
	DeviceTypeGhost DeviceType = iota // 幽灵设备(Web 端 JWT 认证)
)

// String 返回设备类型的字符串表示
func (t DeviceType) String() string {
	switch t {
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

	// 幽灵设备信息（JWT 认证）
	UserID   int    // 用户 ID
	Username string // 用户名
	CallSign string // 呼号
	Nickname string // 昵称
	SSID     byte   // 设备子号

	GroupID     int  // 当前群组
	DevModel    byte // 设备型号
	IsOnline    bool
	DisableSend bool // 禁发
	DisableRecv bool // 禁收

	// 统计信息
	Traffic     int64
	VoiceTime   int64
	PacketCount int64
}

// GetIdentifier 获取设备唯一标识
func (d *WSDevice) GetIdentifier() string {
	if d.DeviceType == DeviceTypeGhost {
		return fmt.Sprintf("ghost-%d", d.UserID)
	}
	return fmt.Sprintf("%s-%d", d.CallSign, d.SSID)
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

// IsDisabledSend 检查是否禁发
func (d *WSDevice) IsDisabledSend() bool {
	return d.DisableSend
}

// GetConnectTime 获取连接时间
func (d *WSDevice) GetConnectTime() time.Time {
	return d.ConnectTime
}

// GetLastPacketTime 获取最后数据包时间
func (d *WSDevice) GetLastPacketTime() time.Time {
	return d.LastPacketTime
}

// WSConnectionManager WebSocket 连接管理器
type WSConnectionManager struct {
	// 幽灵设备连接 (key: userID)
	ghostDevices map[int]*WSDevice
	// 连接索引 (key: conn.RemoteAddr().String())
	connMap map[string]*WSDevice
	// 群组索引 (key: groupID -> deviceKey -> device)
	groupDevices map[int]map[string]*WSDevice

	// 读写锁
	mu sync.RWMutex
	// 配置
	AuthTimeout      time.Duration // 认证超时
	HeartbeatTimeout time.Duration // 心跳超时
	ReconnectGrace   time.Duration // 重连宽限期
	ProxyTimeout     time.Duration // 反向代理超时
	PreReconnectTime time.Duration // 预重连时间(在代理超时前多久开始准备重连)
}

// NewWSConnectionManager 创建新的连接管理器
func NewWSConnectionManager() *WSConnectionManager {
	return &WSConnectionManager{
		ghostDevices:     make(map[int]*WSDevice),
		connMap:          make(map[string]*WSDevice),
		groupDevices:     make(map[int]map[string]*WSDevice),
		AuthTimeout:      30 * time.Second,  // 30 秒认证超时
		HeartbeatTimeout: 20 * time.Second,  // 20 秒心跳超时
		ReconnectGrace:   30 * time.Second,  // 30 秒重连宽限期
		ProxyTimeout:     300 * time.Second, // 300 秒反向代理超时
		PreReconnectTime: 240 * time.Second, // 240 秒开始准备重连
	}
}

// ==========================================
// 性能优化：群组索引辅助方法
// ==========================================

// addToGroupIndex 将设备添加到群组索引
func (m *WSConnectionManager) addToGroupIndex(groupID int, key string, device *WSDevice) {
	if m.groupDevices[groupID] == nil {
		m.groupDevices[groupID] = make(map[string]*WSDevice)
	}
	m.groupDevices[groupID][key] = device
}

// removeFromGroupIndex 从群组索引中移除设备
func (m *WSConnectionManager) removeFromGroupIndex(groupID int, key string) {
	if devices, ok := m.groupDevices[groupID]; ok {
		delete(devices, key)
		// 如果群组为空，清理map
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
	return fmt.Sprintf("%s-%d", device.CallSign, device.SSID)
}

// RegisterConnection 注册新连接
func (m *WSConnectionManager) RegisterConnection(conn *websocket.Conn) *WSDevice {
	m.mu.Lock()
	defer m.mu.Unlock()

	addr := conn.RemoteAddr().String()
	device := &WSDevice{
		Conn:           conn,
		ConnState:      StateConnecting,
		ConnectTime:    time.Now(),
		LastPacketTime: time.Now(),
		GroupID:        models.GroupIDPublicMin, // 默认群组
	}
	m.connMap[addr] = device

	log.Printf("[WS] New connection registered: %s", addr)
	return device
}

// UnregisterDevice 注销设备
func (m *WSConnectionManager) UnregisterDevice(device *WSDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()

	addr := device.Conn.RemoteAddr().String()
	delete(m.connMap, addr)
	device.IsOnline = false
	device.ConnState = StateDisconnected
	// 从群组索引中移除
	key := getDeviceKey(device)
	m.removeFromGroupIndex(device.GroupID, key)
	log.Printf("[WS] Device unregistered: %s", key)
}

// GetDeviceByConn 通过连接获取设备
func (m *WSConnectionManager) GetDeviceByConn(conn *websocket.Conn) (*WSDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	addr := conn.RemoteAddr().String()
	device, exists := m.connMap[addr]
	return device, exists
}

// GetGhostDevice 获取幽灵设备
func (m *WSConnectionManager) GetGhostDevice(userID int) (*WSDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	device, exists := m.ghostDevices[userID]
	return device, exists
}

// IsGhostDeviceOnline 检查幽灵设备是否在线
func (m *WSConnectionManager) IsGhostDeviceOnline(userID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	device, exists := m.ghostDevices[userID]
	return exists && device != nil && device.IsOnline && device.ConnState == StateOnline
}

// GetAllOnlineDevices 获取所有在线设备
func (m *WSConnectionManager) GetAllOnlineDevices() []*WSDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	devices := make([]*WSDevice, 0)
	for _, device := range m.ghostDevices {
		if device.IsOnline {
			devices = append(devices, device)
		}
	}
	return devices
}

// GetDevicesByGroup 获取指定群组的在线设备
func (m *WSConnectionManager) GetDevicesByGroup(groupID int) []*WSDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// 直接从群组索引获取
	if groupDevs, ok := m.groupDevices[groupID]; ok {
		devices := make([]*WSDevice, 0)
		for _, device := range groupDevs {
			if device.IsOnline {
				devices = append(devices, device)
			}
		}
		return devices
	}
	return []*WSDevice{}
}

// GetOnlineCount 获取在线设备数量
func (m *WSConnectionManager) GetOnlineCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ghostCount := 0
	for _, device := range m.ghostDevices {
		if device.IsOnline {
			ghostCount++
		}
	}
	return ghostCount
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

// RegisterGhostDevice 注册幽灵设备
func (m *WSConnectionManager) RegisterGhostDevice(device *WSDevice, userID int, username, callsign, nickname string, ssid byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	device.DeviceType = DeviceTypeGhost
	device.UserID = userID
	device.Username = username
	device.CallSign = callsign
	device.Nickname = nickname
	device.SSID = ssid
	device.IsOnline = true
	device.ConnState = StateOnline

	m.ghostDevices[userID] = device

	// 添加到群组索引
	key := getDeviceKey(device)
	m.addToGroupIndex(device.GroupID, key, device)

	log.Printf("[WS] Ghost device registered: user-%d (%s-%d) group-%d", userID, callsign, ssid, device.GroupID)
}

// SetDeviceGroup 设置设备群组
func (m *WSConnectionManager) SetDeviceGroup(device *WSDevice, newGroupID int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldGroupID := device.GroupID
	if oldGroupID == newGroupID {
		return
	}

	// 从旧群组索引移除
	oldKey := getDeviceKey(device)
	m.removeFromGroupIndex(oldGroupID, oldKey)

	// 更新群组
	device.GroupID = newGroupID

	// 添加到新群组索引
	newKey := getDeviceKey(device)
	m.addToGroupIndex(newGroupID, newKey, device)

	log.Printf("[WS] Device group changed: %s from group %d to %d", device.GetIdentifier(), oldGroupID, newGroupID)
}

// ErrDeviceNotFound 设备未找到错误
var ErrDeviceNotFound = errors.New("device not found")
