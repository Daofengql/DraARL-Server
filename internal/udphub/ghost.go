package udphub

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

// ==========================================
// UDP 幽灵设备管理器
// 用于管理通过 UDP + JWT 认证的幽灵设备 (DevModel 101-104)
// 这些设备不存储在数据库中，仅存在于内存
// ==========================================

// UDPGhostManager UDP 幽灵设备管理器
type UDPGhostManager struct {
	// 主索引：通过 key (username-ssid) 查找设备，用于快速单播、心跳更新和在线状态判断
	devices map[string]*models.Device

	// 二级索引：按群组划分设备，用于 O(1) 复杂度的群组广播
	// 外层 Key: groupID, 内层 Key: username-ssid, Value: 设备指针
	groupDevices map[int]map[string]*models.Device

	mu sync.RWMutex
}

// GlobalUDPGhostManager 全局 UDP 幽灵设备管理器实例
var GlobalUDPGhostManager = &UDPGhostManager{
	devices:      make(map[string]*models.Device),
	groupDevices: make(map[int]map[string]*models.Device),
}

// getDeviceKey 生成设备唯一键
func getDeviceKey(username string, ssid byte) string {
	return fmt.Sprintf("%s-%d", username, ssid)
}

// Register 注册或刷新 UDP 幽灵设备。
// 当前策略是不再踢旧设备；调用方在入参前负责完成冲突判断。
func (m *UDPGhostManager) Register(device *models.Device) *models.Device {
	key := getDeviceKey(device.Username, device.SSID)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.devices[key]; exists {
		oldGroupID := existing.GroupID
		if oldGroupID != device.GroupID {
			if groupMap, ok := m.groupDevices[oldGroupID]; ok {
				delete(groupMap, key)
				if len(groupMap) == 0 {
					delete(m.groupDevices, oldGroupID)
				}
			}
		}

		existing.Username = device.Username
		existing.CallSign = device.CallSign
		existing.OwnerID = device.OwnerID
		existing.SSID = device.SSID
		existing.CallSignSSID = device.CallSignSSID
		existing.DevModel = device.DevModel
		existing.GroupID = device.GroupID
		existing.Priority = device.Priority
		existing.Status = device.Status
		existing.ISOnline = device.ISOnline
		existing.UDPAddr = device.UDPAddr
		existing.LastPacketTime = device.LastPacketTime
		existing.OnlineTime = device.OnlineTime
		device = existing
	} else {
		m.devices[key] = device
	}

	if m.groupDevices[device.GroupID] == nil {
		m.groupDevices[device.GroupID] = make(map[string]*models.Device)
	}
	m.groupDevices[device.GroupID][key] = device

	log.Printf("[UDP-GHOST] 设备注册: %s (用户: %s, 呼号: %s, 群组: %d)",
		key, device.Username, device.CallSign, device.GroupID)

	return device
}

// Get 获取指定的 UDP 幽灵设备
func (m *UDPGhostManager) Get(username string, ssid byte) *models.Device {
	key := getDeviceKey(username, ssid)

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.devices[key]
}

// GetByUsername 获取用户的所有 UDP 幽灵设备
func (m *UDPGhostManager) GetByUsername(username string) []*models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var devices []*models.Device
	for key, dev := range m.devices {
		// 检查 key 是否以 username- 开头
		prefix := fmt.Sprintf("%s-", username)
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			devices = append(devices, dev)
		}
	}
	return devices
}

// GetByGroup 获取指定群组的 UDP 幽灵设备 (优化为 O(1) 复杂度)
func (m *UDPGhostManager) GetByGroup(groupID int) []*models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 查不到群组直接返回空切片，避免遍历整个设备表
	groupMap, exists := m.groupDevices[groupID]
	if !exists || len(groupMap) == 0 {
		return nil
	}

	// 预分配切片容量，避免 append 时的内存重分配
	devices := make([]*models.Device, 0, len(groupMap))
	for _, dev := range groupMap {
		if dev.ISOnline {
			devices = append(devices, dev)
		}
	}
	return devices
}

// Remove 移除指定的 UDP 幽灵设备
func (m *UDPGhostManager) Remove(username string, ssid byte) {
	key := getDeviceKey(username, ssid)

	m.mu.Lock()
	defer m.mu.Unlock()

	if dev, exists := m.devices[key]; exists {
		// 从主索引中移除
		delete(m.devices, key)

		// 从群组二级索引中移除
		if groupMap, ok := m.groupDevices[dev.GroupID]; ok {
			delete(groupMap, key)
			// 如果群组为空，清理 map 防止内存泄漏
			if len(groupMap) == 0 {
				delete(m.groupDevices, dev.GroupID)
			}
		}

		log.Printf("[UDP-GHOST] 设备移除: %s (用户: %s, 呼号: %s)",
			key, dev.Username, dev.CallSign)
	}
}

// RemoveByUDPAddr 通过 UDP 地址移除设备（用于断开连接时清理）
func (m *UDPGhostManager) RemoveByUDPAddr(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, dev := range m.devices {
		if dev.UDPAddr != nil && dev.UDPAddr.String() == addr {
			// 从主索引中移除
			delete(m.devices, key)

			// 从群组二级索引中移除
			if groupMap, ok := m.groupDevices[dev.GroupID]; ok {
				delete(groupMap, key)
				if len(groupMap) == 0 {
					delete(m.groupDevices, dev.GroupID)
				}
			}

			log.Printf("[UDP-GHOST] 设备断开: %s (地址: %s)", key, addr)
		}
	}
}

// GetAll 获取所有 UDP 幽灵设备
func (m *UDPGhostManager) GetAll() []*models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	devices := make([]*models.Device, 0, len(m.devices))
	for _, dev := range m.devices {
		devices = append(devices, dev)
	}
	return devices
}

// FindBySSIDAndAddr 通过 SSID + UDP 地址查找幽灵设备
// 性能优化：避免通过 GetAll 先构建切片再遍历
func (m *UDPGhostManager) FindBySSIDAndAddr(ssid byte, addr *net.UDPAddr) *models.Device {
	if addr == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dev := range m.devices {
		if dev.SSID != ssid || dev.UDPAddr == nil {
			continue
		}
		if dev.UDPAddr.Port == addr.Port && dev.UDPAddr.IP.Equal(addr.IP) {
			return dev
		}
	}
	return nil
}

// GetOnlineCount 获取在线的 UDP 幽灵设备数量
func (m *UDPGhostManager) GetOnlineCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, dev := range m.devices {
		if dev.ISOnline {
			count++
		}
	}
	return count
}

// CheckTimeout 检查并移除超时的 UDP 幽灵设备
// timeout: 超时时间（建议 20-30 秒）
func (m *UDPGhostManager) CheckTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for key, dev := range m.devices {
		if now.Sub(dev.LastPacketTime) > timeout {
			log.Printf("[UDP-GHOST] 设备超时下线: %s (用户: %s, 超时: %v)",
				key, dev.Username, now.Sub(dev.LastPacketTime))

			// 从主索引中移除
			delete(m.devices, key)

			// 从群组二级索引中移除
			if groupMap, ok := m.groupDevices[dev.GroupID]; ok {
				delete(groupMap, key)
				if len(groupMap) == 0 {
					delete(m.groupDevices, dev.GroupID)
				}
			}
		}
	}
}

// UpdateActivity 更新设备活动时间
func (m *UDPGhostManager) UpdateActivity(username string, ssid byte, addr *net.UDPAddr) {
	key := getDeviceKey(username, ssid)

	m.mu.Lock()
	defer m.mu.Unlock()

	if dev, exists := m.devices[key]; exists {
		dev.LastPacketTime = time.Now()
		if addr != nil {
			dev.UDPAddr = addr
		}
	}
}

// SetDeviceGroup 设置设备群组（在跨组时非常重要，必须同步维护二级索引）
func (m *UDPGhostManager) SetDeviceGroup(username string, ssid byte, groupID int) error {
	key := getDeviceKey(username, ssid)

	m.mu.Lock()
	defer m.mu.Unlock()

	dev, exists := m.devices[key]
	if !exists {
		return fmt.Errorf("device not found: %s", key)
	}

	oldGroupID := dev.GroupID
	if oldGroupID == groupID {
		return nil // 群组未变，直接返回
	}

	// 1. 从旧群组索引中剔除
	if groupMap, ok := m.groupDevices[oldGroupID]; ok {
		delete(groupMap, key)
		if len(groupMap) == 0 {
			delete(m.groupDevices, oldGroupID)
		}
	}

	// 2. 更新设备的群组属性
	dev.GroupID = groupID

	// 3. 加入新群组索引
	if m.groupDevices[groupID] == nil {
		m.groupDevices[groupID] = make(map[string]*models.Device)
	}
	m.groupDevices[groupID][key] = dev

	log.Printf("[UDP-GHOST] 设备群组变更: %s (%d -> %d)", key, oldGroupID, groupID)
	return nil
}

// IsGhostDevice 判断是否为 UDP 幽灵设备
// 通过 DevModel 判断
func IsGhostDevice(dev *models.Device) bool {
	if dev == nil {
		return false
	}
	return protocol.IsGhostDevModel(dev.DevModel)
}

// GetDeviceByKey 通过 key 获取设备（用于内部快速查找）
func (m *UDPGhostManager) GetDeviceByKey(key string) *models.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.devices[key]
}

// GetStats 获取管理器统计信息
func (m *UDPGhostManager) GetStats() (total int, online int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total = len(m.devices)
	for _, dev := range m.devices {
		if dev.ISOnline {
			online++
		}
	}
	return
}

// UpdateUserCallSign 在管理员审批通过后同步在线 UDP 幽灵设备的呼号。
func (m *UDPGhostManager) UpdateUserCallSign(ownerID int, username, newCallSign string) {
	if ownerID <= 0 && username == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, dev := range m.devices {
		if dev == nil {
			continue
		}
		if ownerID > 0 && dev.OwnerID == ownerID {
			dev.CallSign = newCallSign
			dev.CallSignSSID = protocol.GetCallSignSSID(newCallSign, dev.SSID)
			continue
		}
		if ownerID <= 0 && username != "" && dev.Username == username {
			dev.CallSign = newCallSign
			dev.CallSignSSID = protocol.GetCallSignSSID(newCallSign, dev.SSID)
		}
	}
}
