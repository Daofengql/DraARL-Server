package websocket

import (
	"fmt"
	"log"
	"sync"
	"time"

	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

// GhostDevice 幽灵设备（Web 端浏览器客户端）
type GhostDevice struct {
	// 基本信息
	ID           int    // 虚拟 ID（负数，如 -userID）
	UserID       int    // 关联的用户 ID
	CallSign     string // 用户呼号
	Nickname     string // 用户昵称
	Username     string // 用户名
	SSID         byte   // 设备子号（可配置）

	// 连接信息
	Conn           *WSDevice // 关联的 WSDevice
	GroupID        int       // 当前群组
	ISOnline       bool
	LastPacketTime time.Time

	// 状态控制
	DisableSend bool
	DisableRecv bool

	// 统计
	VoiceTime int64
	Traffic   int64
}

// GhostDeviceManager 幽灵设备管理器
type GhostDeviceManager struct {
	devices map[int]*GhostDevice // key: userID
	mu      sync.RWMutex

	// 默认 SSID
	defaultSSID byte
}

// NewGhostDeviceManager 创建幽灵设备管理器
func NewGhostDeviceManager() *GhostDeviceManager {
	return &GhostDeviceManager{
		devices:     make(map[int]*GhostDevice),
		defaultSSID: 10,
	}
}

// CreateGhostDevice 创建幽灵设备
func (m *GhostDeviceManager) CreateGhostDevice(wsDevice *WSDevice, userID int, callsign, nickname string, ssid byte) *GhostDevice {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	if existing, ok := m.devices[userID]; ok {
		// 更新现有设备
		existing.Conn = wsDevice
		existing.ISOnline = true
		existing.LastPacketTime = time.Now()
		existing.CallSign = callsign
		existing.Nickname = nickname
		log.Printf("[GHOST] Updated existing ghost device: user-%d (%s-%d)", userID, callsign, ssid)
		return existing
	}

	// 创建新设备
	ghost := &GhostDevice{
		ID:             -userID, // 负数 ID
		UserID:         userID,
		CallSign:       callsign,
		Nickname:       nickname,
		Username:       callsign, // 使用呼号作为用户名
		SSID:           ssid,
		Conn:           wsDevice,
		GroupID:        999, // 默认公共群组
		ISOnline:       true,
		LastPacketTime: time.Now(),
	}

	m.devices[userID] = ghost
	log.Printf("[GHOST] Created ghost device: user-%d (%s-%d)", userID, callsign, ssid)
	return ghost
}

// RemoveGhostDevice 移除幽灵设备
func (m *GhostDeviceManager) RemoveGhostDevice(userID int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ghost, ok := m.devices[userID]; ok {
		ghost.ISOnline = false
		delete(m.devices, userID)
		log.Printf("[GHOST] Removed ghost device: user-%d (%s-%d)", userID, ghost.CallSign, ghost.SSID)
	}
}

// GetGhostDevice 获取幽灵设备
func (m *GhostDeviceManager) GetGhostDevice(userID int) (*GhostDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ghost, ok := m.devices[userID]
	return ghost, ok
}

// GetGhostDeviceByID 通过虚拟 ID 获取幽灵设备
func (m *GhostDeviceManager) GetGhostDeviceByID(id int) (*GhostDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// ID 是负数，转换为 userID
	userID := -id
	ghost, ok := m.devices[userID]
	return ghost, ok
}

// SetGhostDeviceGroup 设置幽灵设备群组
func (m *GhostDeviceManager) SetGhostDeviceGroup(userID, groupID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ghost, ok := m.devices[userID]
	if !ok {
		return fmt.Errorf("ghost device not found: user-%d", userID)
	}

	oldGroupID := ghost.GroupID
	ghost.GroupID = groupID

	log.Printf("[GHOST] Device group changed: user-%d (%s-%d) from group %d to %d",
		userID, ghost.CallSign, ghost.SSID, oldGroupID, groupID)

	return nil
}

// GetOnlineGhostDevices 获取所有在线的幽灵设备
func (m *GhostDeviceManager) GetOnlineGhostDevices() []*GhostDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*GhostDevice, 0)
	for _, ghost := range m.devices {
		if ghost.ISOnline {
			devices = append(devices, ghost)
		}
	}
	return devices
}

// GetGhostDevicesByGroup 获取指定群组的幽灵设备
func (m *GhostDeviceManager) GetGhostDevicesByGroup(groupID int) []*GhostDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*GhostDevice, 0)
	for _, ghost := range m.devices {
		if ghost.ISOnline && ghost.GroupID == groupID {
			devices = append(devices, ghost)
		}
	}
	return devices
}

// GetOnlineGhostCount 获取在线幽灵设备数量
func (m *GhostDeviceManager) GetOnlineGhostCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, ghost := range m.devices {
		if ghost.ISOnline {
			count++
		}
	}
	return count
}

// ConvertToModelDevice 将幽灵设备转换为 models.Device（用于消息路由）
func (g *GhostDevice) ConvertToModelDevice() *models.Device {
	return &models.Device{
		ID:           g.ID,
		Name:         g.Nickname,
		SSID:         g.SSID,
		OwnerID:      g.UserID,
		CallSign:     g.CallSign,
		Username:     g.Username,
		DevModel:     protocol.DraARLDevModelBrowser,
		GroupID:      g.GroupID,
		ISOnline:     g.ISOnline,
		DisableSend:  g.DisableSend,
		DisableRecv:  g.DisableRecv,
		VoiceTime:    g.VoiceTime,
		Traffic:      g.Traffic,
	}
}

// GetIdentifier 获取设备标识
func (g *GhostDevice) GetIdentifier() string {
	return fmt.Sprintf("ghost-%d", g.UserID)
}

// GetCallSignSSID 获取呼号-SSID
func (g *GhostDevice) GetCallSignSSID() string {
	return fmt.Sprintf("%s-%d", g.CallSign, g.SSID)
}

// UpdateActivity 更新活动时间
func (g *GhostDevice) UpdateActivity() {
	g.LastPacketTime = time.Now()
}

// IsGhostDevice 检查是否是幽灵设备（通过 ID 判断）
func IsGhostDevice(id int) bool {
	return id < 0
}

// GlobalGhostManager 全局幽灵设备管理器
var GlobalGhostManager = NewGhostDeviceManager()
