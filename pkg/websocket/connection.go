package websocket

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"draarl/internal/models"
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

	// ==========================================
	// 异步写入优化：带缓冲的写通道
	// 解决跨组转发时同步阻塞导致的延迟累积问题
	// ==========================================
	writeCh   chan *writeRequest // 异步写缓冲通道
	closeCh   chan struct{}      // 关闭信号
	writeMu   sync.Mutex         // 保护 writeCh 的访问
	writeOnce sync.Once          // 确保 writer 只启动一次
}

// writeRequest 写请求结构
type writeRequest struct {
	messageType int
	data        []byte
}

const writeChSize = 64 // 写通道缓冲大小，约 4 秒的音频帧 (63ms * 64 ≈ 4s)

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

// ==========================================
// 异步写入优化：独立 writer goroutine
// 解决跨组转发时同步阻塞导致的延迟累积问题
// ==========================================

// StartWriter 启动独立的 writer goroutine
// 使用 sync.Once 确保只启动一次
func (d *WSDevice) StartWriter() {
	d.writeOnce.Do(func() {
		d.writeCh = make(chan *writeRequest, writeChSize)
		d.closeCh = make(chan struct{})
		go d.writerLoop()
	})
}

// writerLoop writer goroutine 主循环
// 所有写操作都通过此 goroutine 串行执行，避免写锁竞争
func (d *WSDevice) writerLoop() {
	for {
		select {
		case req := <-d.writeCh:
			if err := d.Conn.WriteMessage(req.messageType, req.data); err != nil {
				log.Printf("[WS] Async write failed for %s: %v", d.GetIdentifier(), err)
				return
			}
		case <-d.closeCh:
			return
		}
	}
}

// AsyncWrite 异步写入数据（非阻塞）
// 返回值：true=投递成功，false=通道满（丢帧）
func (d *WSDevice) AsyncWrite(messageType int, data []byte) bool {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if d.writeCh == nil {
		return false
	}

	select {
	case d.writeCh <- &writeRequest{messageType: messageType, data: data}:
		return true
	default:
		// 通道满，丢帧而不是阻塞
		log.Printf("[WS] Write channel full for %s, dropping frame", d.GetIdentifier())
		return false
	}
}

// StopWriter 停止 writer goroutine
func (d *WSDevice) StopWriter() {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if d.closeCh != nil {
		close(d.closeCh)
		d.closeCh = nil
	}
	if d.writeCh != nil {
		close(d.writeCh)
		d.writeCh = nil
	}
}

// WritePing 通过异步通道发送 Ping 消息
func (d *WSDevice) WritePing() bool {
	return d.AsyncWrite(websocket.PingMessage, []byte{})
}

// ==========================================
// 性能优化：分片锁实现
// ==========================================

const shardCount = 32 // 分片数量，应为 2 的幂次方

// connShard 连接分片，每个分片有独立的锁
type connShard struct {
	mu           sync.RWMutex
	ghostDevices map[int]*WSDevice           // 幽灵设备 (key: userID)
	connMap      map[string]*WSDevice        // 连接索引 (key: conn.RemoteAddr().String())
	groupDevices map[int]map[string]*WSDevice // 群组索引
}

// WSConnectionManager WebSocket 连接管理器（优化版）
// 使用全局群组索引实现 O(1) 的群组查询，解决跨组转发卡顿问题
type WSConnectionManager struct {
	shards [shardCount]*connShard // 分片数组

	// 【优化】全局群组索引：独立锁，避免分片遍历
	// key: groupID, value: map[deviceKey]*WSDevice
	globalGroupIndex struct {
		mu        sync.RWMutex
		devices   map[int]map[string]*WSDevice
	}

	// 配置
	AuthTimeout      time.Duration
	HeartbeatTimeout time.Duration
	ReconnectGrace   time.Duration
	ProxyTimeout     time.Duration
	PreReconnectTime time.Duration

	// 统计信息（原子操作）
	totalConnections int64
}

// hashUserID 根据 userID 计算分片索引
func hashUserID(userID int) int {
	return userID % shardCount
}

// hashAddr 根据连接地址计算分片索引
func hashAddr(addr string) int {
	hash := 0
	for i, c := range addr {
		hash += int(c) * (i + 1)
	}
	return hash % shardCount
}

// getShardByUserID 根据 userID 获取分片
func (m *WSConnectionManager) getShardByUserID(userID int) *connShard {
	return m.shards[hashUserID(userID)]
}

// getShardByAddr 根据连接地址获取分片
func (m *WSConnectionManager) getShardByAddr(addr string) *connShard {
	return m.shards[hashAddr(addr)]
}

// NewWSConnectionManager 创建新的连接管理器
func NewWSConnectionManager() *WSConnectionManager {
	m := &WSConnectionManager{
		AuthTimeout:      30 * time.Second,  // 30 秒认证超时
		HeartbeatTimeout: 20 * time.Second,  // 20 秒心跳超时
		ReconnectGrace:   30 * time.Second,  // 30 秒重连宽限期
		ProxyTimeout:     300 * time.Second, // 300 秒反向代理超时
		PreReconnectTime: 240 * time.Second, // 240 秒开始准备重连
	}

	// 初始化全局群组索引
	m.globalGroupIndex.devices = make(map[int]map[string]*WSDevice)

	// 初始化所有分片
	for i := 0; i < shardCount; i++ {
		m.shards[i] = &connShard{
			ghostDevices: make(map[int]*WSDevice),
			connMap:      make(map[string]*WSDevice),
			groupDevices: make(map[int]map[string]*WSDevice),
		}
	}

	return m
}

// ==========================================
// 分片内部辅助方法（调用前必须持有分片锁）
// ==========================================

// addToGroupIndexInShard 将设备添加到群组索引（分片内）
func (s *connShard) addToGroupIndexInShard(groupID int, key string, device *WSDevice) {
	if s.groupDevices[groupID] == nil {
		s.groupDevices[groupID] = make(map[string]*WSDevice)
	}
	s.groupDevices[groupID][key] = device
}

// removeFromGroupIndexInShard 从群组索引中移除设备（分片内）
func (s *connShard) removeFromGroupIndexInShard(groupID int, key string) {
	if devices, ok := s.groupDevices[groupID]; ok {
		delete(devices, key)
		// 如果群组为空，清理map
		if len(devices) == 0 {
			delete(s.groupDevices, groupID)
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

// ==========================================
// 全局群组索引辅助方法
// ==========================================

// addToGlobalGroupIndex 将设备添加到全局群组索引
func (m *WSConnectionManager) addToGlobalGroupIndex(groupID int, key string, device *WSDevice) {
	m.globalGroupIndex.mu.Lock()
	defer m.globalGroupIndex.mu.Unlock()

	if m.globalGroupIndex.devices[groupID] == nil {
		m.globalGroupIndex.devices[groupID] = make(map[string]*WSDevice)
	}
	m.globalGroupIndex.devices[groupID][key] = device
}

// removeFromGlobalGroupIndex 从全局群组索引中移除设备
func (m *WSConnectionManager) removeFromGlobalGroupIndex(groupID int, key string) {
	m.globalGroupIndex.mu.Lock()
	defer m.globalGroupIndex.mu.Unlock()

	if devices, ok := m.globalGroupIndex.devices[groupID]; ok {
		delete(devices, key)
		// 如果群组为空，清理 map
		if len(devices) == 0 {
			delete(m.globalGroupIndex.devices, groupID)
		}
	}
}

// ==========================================
// 公共 API 方法
// ==========================================

// RegisterConnection 注册新连接
func (m *WSConnectionManager) RegisterConnection(conn *websocket.Conn) *WSDevice {
	addr := conn.RemoteAddr().String()
	shard := m.getShardByAddr(addr)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	device := &WSDevice{
		Conn:           conn,
		ConnState:      StateConnecting,
		ConnectTime:    time.Now(),
		LastPacketTime: time.Now(),
		GroupID:        models.GroupIDPublicMin, // 默认群组
	}
	shard.connMap[addr] = device

	atomic.AddInt64(&m.totalConnections, 1)
	log.Printf("[WS] New connection registered: %s", addr)
	return device
}

// UnregisterDevice 注销设备
func (m *WSConnectionManager) UnregisterDevice(device *WSDevice) {
	if device.Conn == nil {
		return
	}

	addr := device.Conn.RemoteAddr().String()
	shard := m.getShardByAddr(addr)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	delete(shard.connMap, addr)
	device.IsOnline = false
	device.ConnState = StateDisconnected

	// 从分片群组索引中移除
	key := getDeviceKey(device)
	shard.removeFromGroupIndexInShard(device.GroupID, key)

	// 【优化】从全局群组索引中移除
	m.removeFromGlobalGroupIndex(device.GroupID, key)

	// 从幽灵设备索引移除
	if device.DeviceType == DeviceTypeGhost {
		delete(shard.ghostDevices, device.UserID)
	}

	atomic.AddInt64(&m.totalConnections, -1)
	log.Printf("[WS] Device unregistered: %s", key)
}

// GetDeviceByConn 通过连接获取设备
func (m *WSConnectionManager) GetDeviceByConn(conn *websocket.Conn) (*WSDevice, bool) {
	addr := conn.RemoteAddr().String()
	shard := m.getShardByAddr(addr)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	device, exists := shard.connMap[addr]
	return device, exists
}

// GetGhostDevice 获取幽灵设备
func (m *WSConnectionManager) GetGhostDevice(userID int) (*WSDevice, bool) {
	shard := m.getShardByUserID(userID)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	device, exists := shard.ghostDevices[userID]
	return device, exists
}

// IsGhostDeviceOnline 检查幽灵设备是否在线
func (m *WSConnectionManager) IsGhostDeviceOnline(userID int) bool {
	shard := m.getShardByUserID(userID)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	device, exists := shard.ghostDevices[userID]
	return exists && device != nil && device.IsOnline && device.ConnState == StateOnline
}

// GetAllOnlineDevices 获取所有在线设备
func (m *WSConnectionManager) GetAllOnlineDevices() []*WSDevice {
	devices := make([]*WSDevice, 0)

	// 遍历所有分片
	for i := 0; i < shardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		for _, device := range shard.ghostDevices {
			if device.IsOnline {
				devices = append(devices, device)
			}
		}
		shard.mu.RUnlock()
	}

	return devices
}

// GetDevicesByGroup 获取指定群组的在线设备
// 【优化】使用全局群组索引，O(1) 复杂度，解决跨组转发卡顿问题
func (m *WSConnectionManager) GetDevicesByGroup(groupID int) []*WSDevice {
	m.globalGroupIndex.mu.RLock()
	defer m.globalGroupIndex.mu.RUnlock()

	groupDevs, ok := m.globalGroupIndex.devices[groupID]
	if !ok || len(groupDevs) == 0 {
		return nil
	}

	// 预分配切片容量
	devices := make([]*WSDevice, 0, len(groupDevs))
	for _, device := range groupDevs {
		if device.IsOnline {
			devices = append(devices, device)
		}
	}

	return devices
}

// GetOnlineCount 获取在线设备数量
func (m *WSConnectionManager) GetOnlineCount() int {
	count := 0

	// 遍历所有分片
	for i := 0; i < shardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		for _, device := range shard.ghostDevices {
			if device.IsOnline {
				count++
			}
		}
		shard.mu.RUnlock()
	}

	return count
}

// GetTotalCount 获取总连接数
func (m *WSConnectionManager) GetTotalCount() int {
	return int(atomic.LoadInt64(&m.totalConnections))
}

// UpdateDeviceActivity 更新设备活动时间
// 注意：此方法不需要锁，因为 LastPacketTime 是单个 goroutine 访问
func (m *WSConnectionManager) UpdateDeviceActivity(device *WSDevice) {
	device.LastPacketTime = time.Now()
}

// RegisterGhostDevice 注册幽灵设备
func (m *WSConnectionManager) RegisterGhostDevice(device *WSDevice, userID int, username, callsign, nickname string, ssid byte) {
	shard := m.getShardByUserID(userID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	ssid = fixedWebGhostSSID

	device.DeviceType = DeviceTypeGhost
	device.UserID = userID
	device.Username = username
	device.CallSign = callsign
	device.Nickname = nickname
	device.SSID = ssid
	device.IsOnline = true
	device.ConnState = StateOnline

	shard.ghostDevices[userID] = device

	// 添加到分片群组索引
	key := getDeviceKey(device)
	shard.addToGroupIndexInShard(device.GroupID, key, device)

	// 【优化】添加到全局群组索引
	m.addToGlobalGroupIndex(device.GroupID, key, device)

	log.Printf("[WS] Ghost device registered: user-%d (%s-%d) group-%d", userID, callsign, ssid, device.GroupID)
}

// SetDeviceGroup 设置设备群组
func (m *WSConnectionManager) SetDeviceGroup(device *WSDevice, newGroupID int) {
	// 使用 userID 确定分片（幽灵设备）
	var shard *connShard
	if device.DeviceType == DeviceTypeGhost {
		shard = m.getShardByUserID(device.UserID)
	} else if device.Conn != nil {
		shard = m.getShardByAddr(device.Conn.RemoteAddr().String())
	} else {
		return
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	oldGroupID := device.GroupID
	if oldGroupID == newGroupID {
		return
	}

	// 从旧群组索引移除
	key := getDeviceKey(device)
	shard.removeFromGroupIndexInShard(oldGroupID, key)

	// 【优化】从全局群组索引中移除
	m.removeFromGlobalGroupIndex(oldGroupID, key)

	// 更新群组
	device.GroupID = newGroupID

	// 添加到新群组索引
	shard.addToGroupIndexInShard(newGroupID, key, device)

	// 【优化】添加到全局群组索引
	m.addToGlobalGroupIndex(newGroupID, key, device)

	log.Printf("[WS] Device group changed: %s from group %d to %d", device.GetIdentifier(), oldGroupID, newGroupID)
}

// ErrDeviceNotFound 设备未找到错误
var ErrDeviceNotFound = errors.New("device not found")
