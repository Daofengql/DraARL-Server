package udphub

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// PendingDeviceConfig 待绑定设备配置（用于动态码绑定）
type PendingDeviceConfig struct {
	Username       string
	DevicePassword string // 明文密码（AES解密后）
	SSID           int
	DMRID          int
}

// PendingDevice 待绑定设备
type PendingDevice struct {
	MAC         string               // 设备MAC地址
	IP          string               // 设备IP
	Code        string               // 6位动态码
	CodeExpires time.Time            // 动态码过期时间
	Bound       bool                 // 是否已绑定
	BoundUserID uint                 // 绑定的用户ID
	BoundAt     time.Time            // 绑定时间
	ConfigReady bool                 // 配置是否就绪
	Config      *PendingDeviceConfig // 设备配置
	CreatedAt   time.Time            // 创建时间
}

// PendingDeviceManager 待绑定设备管理器
type PendingDeviceManager struct {
	mu            sync.RWMutex
	byCode        map[string]*PendingDevice // 动态码 -> 设备
	byMAC         map[string]*PendingDevice // MAC -> 设备
	codeDuration  time.Duration             // 动态码有效期
	bindDuration  time.Duration             // 绑定状态有效期
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
	stopOnce      sync.Once
}

// 全局待绑定设备管理器
var pendingDeviceManager *PendingDeviceManager

// newPendingDeviceManager 创建待绑定设备管理器。
func newPendingDeviceManager() *PendingDeviceManager {
	return &PendingDeviceManager{
		byCode:        make(map[string]*PendingDevice),
		byMAC:         make(map[string]*PendingDevice),
		codeDuration:  5 * time.Minute,  // 动态码 5 分钟有效期
		bindDuration:  10 * time.Minute, // 绑定状态 10 分钟有效期
		cleanupTicker: time.NewTicker(30 * time.Second),
		stopCh:        make(chan struct{}),
	}
}

// InitPendingDeviceManager 初始化待绑定设备管理器
func InitPendingDeviceManager() {
	if pendingDeviceManager != nil {
		pendingDeviceManager.Stop()
	}
	pendingDeviceManager = newPendingDeviceManager()
	go pendingDeviceManager.cleanup()
}

// GetPendingDeviceManager 获取全局待绑定设备管理器
func GetPendingDeviceManager() *PendingDeviceManager {
	return pendingDeviceManager
}

// generateDynamicCode 生成6位数字动态码
// 【模偏差修复】改用 crypto/rand.Int（内部拒绝采样，均匀无偏差），
// 避免 24-bit 取模导致部分 6 位码出现概率偏高。
func generateDynamicCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// RequestCode 请求生成动态码
// 如果设备已存在且动态码未过期，返回现有动态码
// 已绑定会话在有效期内返回原会话，支持丢包后的重复请求。
func (m *PendingDeviceManager) RequestCode(mac, ip string) (*PendingDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	if device, exists := m.byMAC[mac]; exists {
		if device.Bound && time.Since(device.BoundAt) <= m.bindDuration {
			return snapshotPendingDevice(device), nil
		}
		// 如果动态码未过期，返回现有动态码
		if !device.Bound && time.Now().Before(device.CodeExpires) {
			return snapshotPendingDevice(device), nil
		}
		// 动态码已过期，生成新的
		delete(m.byCode, device.Code)
	}

	// 生成新的动态码（确保唯一）
	var code string
	for {
		var err error
		code, err = generateDynamicCode()
		if err != nil {
			return nil, fmt.Errorf("生成动态码失败: %w", err)
		}
		// 检查动态码是否已被使用
		if _, exists := m.byCode[code]; !exists {
			break
		}
	}

	// 创建待绑定设备
	device := &PendingDevice{
		MAC:         mac,
		IP:          ip,
		Code:        code,
		CodeExpires: time.Now().Add(m.codeDuration),
		CreatedAt:   time.Now(),
	}

	m.byCode[code] = device
	m.byMAC[mac] = device

	log.Printf("[PENDING] 生成动态码: MAC=%s, IP=%s", mac, ip)
	return snapshotPendingDevice(device), nil
}

// BindDevice 通过动态码绑定设备
func (m *PendingDeviceManager) BindDevice(code string, userID uint) (*PendingDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	device, exists := m.byCode[code]
	if !exists {
		return nil, fmt.Errorf("动态码无效或已过期")
	}

	// The same user can retry after a lost HTTP response or reopen the dialog.
	// Retaining this mapping never lets a different user claim a consumed code.
	if device.Bound {
		if device.BoundUserID == userID && time.Since(device.BoundAt) <= m.bindDuration {
			return snapshotPendingDevice(device), nil
		}
		return nil, fmt.Errorf("该设备已被绑定或绑定已过期")
	}
	// 检查动态码是否过期
	if time.Now().After(device.CodeExpires) {
		delete(m.byCode, code)
		delete(m.byMAC, device.MAC)
		return nil, fmt.Errorf("动态码已过期")
	}

	// 标记为已绑定
	device.Bound = true
	device.BoundUserID = userID
	device.BoundAt = time.Now()

	log.Printf("[PENDING] 设备绑定成功: MAC=%s, UserID=%d", device.MAC, userID)
	return snapshotPendingDevice(device), nil
}

// SetDeviceConfig 设置设备配置
func (m *PendingDeviceManager) SetDeviceConfig(mac string, userID uint, config *PendingDeviceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	device, exists := m.byMAC[mac]
	if !exists {
		return fmt.Errorf("设备未找到")
	}

	if !device.Bound || device.BoundUserID != userID || time.Since(device.BoundAt) > m.bindDuration {
		return fmt.Errorf("设备未绑定到当前用户或绑定已过期")
	}
	if config == nil {
		return fmt.Errorf("设备配置为空")
	}
	copy := *config
	device.Config = &copy
	device.ConfigReady = true

	log.Printf("[PENDING] 设备配置已设置: MAC=%s, SSID=%d, DMRID=%d", mac, config.SSID, config.DMRID)
	return nil
}

// ConfirmBind 确认绑定状态并获取配置
// 设备端轮询此接口
func (m *PendingDeviceManager) ConfirmBind(mac string) (*PendingDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	device, exists := m.byMAC[mac]
	if !exists {
		return nil, fmt.Errorf("设备未请求绑定或已过期")
	}

	// 检查绑定状态是否过期
	if device.Bound && time.Since(device.BoundAt) > m.bindDuration {
		return nil, fmt.Errorf("绑定已过期")
	}
	if !device.Bound && !time.Now().Before(device.CodeExpires) {
		return nil, fmt.Errorf("动态码已过期")
	}
	return snapshotPendingDevice(device), nil
}

// GetByMAC 通过MAC地址获取待绑定设备
func (m *PendingDeviceManager) GetByMAC(mac string) (*PendingDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	device, exists := m.byMAC[mac]
	return snapshotPendingDevice(device), exists
}

// GetByCode 通过动态码获取待绑定设备
func (m *PendingDeviceManager) GetByCode(code string) (*PendingDevice, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	device, exists := m.byCode[code]
	return snapshotPendingDevice(device), exists
}

func snapshotPendingDevice(device *PendingDevice) *PendingDevice {
	if device == nil {
		return nil
	}
	copy := *device
	if device.Config != nil {
		config := *device.Config
		copy.Config = &config
	}
	return &copy
}

// ListConfiguredSSIDsByUser 返回指定用户当前在待绑定队列中已占用的 SSID。
func (m *PendingDeviceManager) ListConfiguredSSIDsByUser(userID uint, excludeMAC string) map[int]struct{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	used := make(map[int]struct{})
	for mac, device := range m.byMAC {
		if device == nil || !device.Bound || device.BoundUserID != userID || device.Config == nil {
			continue
		}
		if excludeMAC != "" && mac == excludeMAC {
			continue
		}
		used[device.Config.SSID] = struct{}{}
	}

	return used
}

// Remove 移除待绑定设备
func (m *PendingDeviceManager) Remove(mac string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if device, exists := m.byMAC[mac]; exists {
		delete(m.byCode, device.Code)
		delete(m.byMAC, mac)
		log.Printf("[PENDING] 移除设备: MAC=%s", mac)
	}
}

// cleanup 定期清理过期的待绑定设备
func (m *PendingDeviceManager) cleanup() {
	for {
		select {
		case <-m.stopCh:
			return
		case now := <-m.cleanupTicker.C:
			m.cleanupExpired(now)
		}
	}
}

func (m *PendingDeviceManager) cleanupExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for mac, device := range m.byMAC {
		// 清理条件：
		// 1. 动态码已过期且未绑定
		// 2. 绑定已过期
		// ConfigReady means queued, not delivered. Keep it until expiry so
		// a delayed poll or lost response can retrieve it again.
		if (!device.Bound && now.After(device.CodeExpires)) ||
			(device.Bound && now.Sub(device.BoundAt) > m.bindDuration) {
			delete(m.byCode, device.Code)
			delete(m.byMAC, mac)
			log.Printf("[PENDING] 清理过期设备: MAC=%s", mac)
		}
	}
}

// Stop 停止清理器
func (m *PendingDeviceManager) Stop() {
	m.stopOnce.Do(func() {
		if m.cleanupTicker != nil {
			m.cleanupTicker.Stop()
		}
		if m.stopCh != nil {
			close(m.stopCh)
		}
	})
}

// Stats 获取统计信息
func (m *PendingDeviceManager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending := 0
	bound := 0
	configured := 0

	for _, device := range m.byMAC {
		if device.ConfigReady {
			configured++
		} else if device.Bound {
			bound++
		} else {
			pending++
		}
	}

	return map[string]interface{}{
		"total":       len(m.byMAC),
		"pending":     pending,
		"bound":       bound,
		"configured":  configured,
		"code_expiry": m.codeDuration.Seconds(),
		"bind_expiry": m.bindDuration.Seconds(),
	}
}

// ValidateMAC 验证 MAC 地址格式
func ValidateMAC(mac string) bool {
	if len(mac) != 17 {
		return false
	}
	for i := 0; i < 17; i++ {
		if i%3 == 2 {
			if mac[i] != ':' {
				return false
			}
		} else {
			c := mac[i]
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
	}
	return true
}

// NormalizeMAC 标准化 MAC 地址为大写
func NormalizeMAC(mac string) string {
	result := make([]byte, 17)
	for i := 0; i < 17; i++ {
		c := mac[i]
		if c >= 'a' && c <= 'f' {
			c -= 32
		}
		result[i] = c
	}
	return string(result)
}

// GenerateRandomHex 生成随机十六进制字符串
func GenerateRandomHex(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes)[:length], nil
}
