package websocket

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"draarl/internal/ghostsession"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

const fixedWebGhostSSID byte = protocol.SSIDGhostWeb

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
	stateMu sync.RWMutex

	// 连接信息
	Conn           *websocket.Conn
	ConnState      ConnectionState
	ConnectTime    time.Time
	LastPacketTime time.Time

	// 设备类型
	DeviceType DeviceType

	// 幽灵设备信息（JWT 认证）
	UserID           int    // 用户 ID
	Username         string // 用户名
	CallSign         string // 呼号
	Nickname         string // 昵称
	SSID             byte   // 设备子号
	SessionID        string
	SessionTag       uint32
	ClientInstanceID string
	LegacySession    bool
	ProtocolVersion  uint16
	Capabilities     []string

	routingMu   sync.RWMutex
	GroupID     int // 当前发送群组
	RxGroupIDs  []int
	DevModel    byte // 设备型号
	IsOnline    bool
	DisableSend bool // 禁发
	DisableRecv bool // 禁收

	// 统计信息
	Traffic     int64
	VoiceTime   int64
	PacketCount int64

	interconnectSessionID    atomic.Uint64
	interconnectSessionEpoch atomic.Uint64

	// ==========================================
	// 异步写入优化：带缓冲的写通道
	// 解决跨组转发时同步阻塞导致的延迟累积问题
	// ==========================================
	writeCh              chan *writeRequest // 异步写缓冲通道
	closeCh              chan struct{}      // 关闭信号
	writeMu              sync.Mutex         // 保护 writeCh 的访问
	writeOnce            sync.Once          // 确保 writer 只启动一次
	unregistered         atomic.Bool
	connectionRegistered bool
}

func (d *WSDevice) GetInterconnectSession() (uint64, uint64) {
	return d.interconnectSessionID.Load(), d.interconnectSessionEpoch.Load()
}

func (d *WSDevice) SetInterconnectSession(sessionID, sessionEpoch uint64) {
	d.interconnectSessionID.Store(sessionID)
	d.interconnectSessionEpoch.Store(sessionEpoch)
}

// writeRequest 写请求结构
type writeRequest struct {
	messageType int
	payload     *sharedWritePayload
}

type sharedWritePayload struct {
	data []byte
	refs atomic.Int32
}

var (
	wsFramesCopied  atomic.Int64
	wsBytesCopied   atomic.Int64
	wsWritesQueued  atomic.Int64
	wsWritesDropped atomic.Int64
	wsWritesDrained atomic.Int64
)

func newSharedWritePayload(data []byte) *sharedWritePayload {
	payload := &sharedWritePayload{data: append([]byte(nil), data...)}
	payload.refs.Store(1)
	wsFramesCopied.Add(1)
	wsBytesCopied.Add(int64(len(data)))
	return payload
}

func (p *sharedWritePayload) retain() {
	if p != nil {
		p.refs.Add(1)
	}
}

func (p *sharedWritePayload) release() {
	if p != nil && p.refs.Add(-1) == 0 {
		p.data = nil
	}
}

const writeChSize = 64 // 写通道缓冲大小，约 4 秒的音频帧 (63ms * 64 ≈ 4s)

// GetIdentifier 获取设备唯一标识
func (d *WSDevice) GetIdentifier() string {
	if d.DeviceType == DeviceTypeGhost {
		return "ghost-session-" + d.SessionID
	}
	return fmt.Sprintf("%s-%d", d.GetCallSign(), d.SSID)
}

// GetCallSignSSID 获取呼号-SSID
func (d *WSDevice) GetCallSignSSID() string {
	return fmt.Sprintf("%s-%d", d.GetCallSign(), d.SSID)
}

// GetGroupID 获取当前群组 ID
func (d *WSDevice) GetGroupID() int {
	d.routingMu.RLock()
	groupID := d.GroupID
	d.routingMu.RUnlock()
	return groupID
}

func (d *WSDevice) GetRxGroupIDs() []int {
	d.routingMu.RLock()
	groupIDs := append([]int(nil), d.RxGroupIDs...)
	d.routingMu.RUnlock()
	return groupIDs
}

func (d *WSDevice) setRouting(routing ghostsession.Routing) {
	d.routingMu.Lock()
	d.GroupID = routing.TxGroupID
	d.RxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	d.routingMu.Unlock()
}

// IsGhost 检查是否是幽灵设备
func (d *WSDevice) IsGhost() bool {
	return d.DeviceType == DeviceTypeGhost
}

// GetUserID 获取用户 ID
func (d *WSDevice) GetUserID() int {
	return d.UserID
}

func (d *WSDevice) GetSessionID() string {
	return d.SessionID
}

func (d *WSDevice) HasCapability(capability string) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	index := sort.SearchStrings(d.Capabilities, capability)
	return index < len(d.Capabilities) && d.Capabilities[index] == capability
}

// GetUsername 获取用户名
func (d *WSDevice) GetUsername() string {
	d.stateMu.RLock()
	value := d.Username
	d.stateMu.RUnlock()
	return value
}

// GetNickname 获取用户昵称
func (d *WSDevice) GetNickname() string {
	d.stateMu.RLock()
	value := d.Nickname
	d.stateMu.RUnlock()
	return value
}

// GetCallSign 获取呼号
func (d *WSDevice) GetCallSign() string {
	d.stateMu.RLock()
	value := d.CallSign
	d.stateMu.RUnlock()
	return value
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
	d.stateMu.RLock()
	value := d.DisableRecv
	d.stateMu.RUnlock()
	return value
}

// IsDisabledSend 检查是否禁发
func (d *WSDevice) IsDisabledSend() bool {
	d.stateMu.RLock()
	value := d.DisableSend
	d.stateMu.RUnlock()
	return value
}

// GetConnectTime 获取连接时间
func (d *WSDevice) GetConnectTime() time.Time {
	d.stateMu.RLock()
	value := d.ConnectTime
	d.stateMu.RUnlock()
	return value
}

// GetLastPacketTime 获取最后数据包时间
func (d *WSDevice) GetLastPacketTime() time.Time {
	d.stateMu.RLock()
	value := d.LastPacketTime
	d.stateMu.RUnlock()
	return value
}

func (d *WSDevice) isOnline() bool {
	d.stateMu.RLock()
	value := d.IsOnline
	d.stateMu.RUnlock()
	return value
}

func (d *WSDevice) isOnlineState(state ConnectionState) bool {
	d.stateMu.RLock()
	matched := d.IsOnline && d.ConnState == state
	d.stateMu.RUnlock()
	return matched
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
		go d.writerLoop(d.writeCh, d.closeCh)
	})
}

// writerLoop writer goroutine 主循环
// 所有写操作都通过此 goroutine 串行执行，避免写锁竞争
func (d *WSDevice) writerLoop(writeCh chan *writeRequest, closeCh <-chan struct{}) {
	defer d.finishWriter(writeCh)
	for {
		select {
		case req, ok := <-writeCh:
			if !ok {
				return
			}
			if req == nil || req.payload == nil {
				continue
			}
			err := d.Conn.WriteMessage(req.messageType, req.payload.data)
			req.payload.release()
			wsWritesDrained.Add(1)
			if err != nil {
				log.Printf("[WS] Async write failed for %s: %v", d.GetIdentifier(), err)
				return
			}
		case <-closeCh:
			return
		}
	}
}

func (d *WSDevice) finishWriter(writeCh chan *writeRequest) {
	d.writeMu.Lock()
	if d.writeCh == writeCh {
		close(writeCh)
		d.writeCh = nil
	}
	d.writeMu.Unlock()
	drainWriteRequests(writeCh)
}

func drainWriteRequests(writeCh <-chan *writeRequest) {
	for {
		select {
		case req, ok := <-writeCh:
			if !ok {
				return
			}
			if req != nil && req.payload != nil {
				req.payload.release()
				wsWritesDrained.Add(1)
			}
		default:
			return
		}
	}
}

// AsyncWrite 异步写入数据（非阻塞）
// 返回值：true=投递成功，false=通道满（丢帧）
// 入队时拷贝 data，调用方缓冲可立即复用/归还。
func (d *WSDevice) AsyncWrite(messageType int, data []byte) bool {
	payload := newSharedWritePayload(data)
	accepted := d.asyncWriteShared(messageType, payload)
	payload.release()
	return accepted
}

func (d *WSDevice) asyncWriteShared(messageType int, payload *sharedWritePayload) bool {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if d.writeCh == nil || payload == nil {
		wsWritesDropped.Add(1)
		return false
	}
	payload.retain()
	select {
	case d.writeCh <- &writeRequest{messageType: messageType, payload: payload}:
		wsWritesQueued.Add(1)
		return true
	default:
		payload.release()
		wsWritesDropped.Add(1)
		return false
	}
}

func getWSDeliveryStats() map[string]int64 {
	queued := wsWritesQueued.Load()
	drained := wsWritesDrained.Load()
	return map[string]int64{
		"frames_copied":  wsFramesCopied.Load(),
		"bytes_copied":   wsBytesCopied.Load(),
		"writes_queued":  queued,
		"writes_dropped": wsWritesDropped.Load(),
		"writes_drained": drained,
		"writes_pending": queued - drained,
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
	ghostDevices map[string]*WSDevice         // 幽灵设备 (key: sessionID)
	ownerDevices map[int]map[string]*WSDevice // 用户在线幽灵会话
	connMap      map[string]*WSDevice         // 连接索引 (key: conn.RemoteAddr().String())
	groupDevices map[int]map[string]*WSDevice // 群组索引
}

// WSConnectionManager WebSocket 连接管理器（优化版）
// 使用全局群组索引实现 O(1) 的群组查询，解决跨组转发卡顿问题
type WSConnectionManager struct {
	shards [shardCount]*connShard // 分片数组

	// 【优化】全局群组索引：独立锁，避免分片遍历
	// key: groupID, value: map[deviceKey]*WSDevice
	globalGroupIndex struct {
		mu      sync.RWMutex
		devices map[int]map[string]*WSDevice
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
			ghostDevices: make(map[string]*WSDevice),
			ownerDevices: make(map[int]map[string]*WSDevice),
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
		return "ghost-session-" + device.SessionID
	}
	return fmt.Sprintf("%s-%d", device.GetCallSign(), device.SSID)
}

// ==========================================
// 全局群组索引辅助方法
// ==========================================

// addToGlobalGroupIndex 将设备添加到全局群组索引
func (m *WSConnectionManager) addToGlobalGroupIndex(groupID int, key string, device *WSDevice) {
	m.globalGroupIndex.mu.Lock()
	defer m.globalGroupIndex.mu.Unlock()
	m.addToGlobalGroupIndexLocked(groupID, key, device)
}

func (m *WSConnectionManager) addToGlobalGroupIndexLocked(groupID int, key string, device *WSDevice) {
	if m.globalGroupIndex.devices[groupID] == nil {
		m.globalGroupIndex.devices[groupID] = make(map[string]*WSDevice)
	}
	m.globalGroupIndex.devices[groupID][key] = device
}

// removeFromGlobalGroupIndex 从全局群组索引中移除设备
func (m *WSConnectionManager) removeFromGlobalGroupIndex(groupID int, key string) {
	m.globalGroupIndex.mu.Lock()
	defer m.globalGroupIndex.mu.Unlock()
	m.removeFromGlobalGroupIndexLocked(groupID, key)
}

func (m *WSConnectionManager) removeFromGlobalGroupIndexLocked(groupID int, key string) {
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
		Conn:                 conn,
		ConnState:            StateConnecting,
		ConnectTime:          time.Now(),
		LastPacketTime:       time.Now(),
		GroupID:              models.GroupIDPublicMin, // 默认群组
		connectionRegistered: true,
	}
	shard.connMap[addr] = device

	atomic.AddInt64(&m.totalConnections, 1)
	log.Printf("[WS] New connection registered: %s", addr)
	return device
}

// UnregisterDevice 注销设备
func (m *WSConnectionManager) UnregisterDevice(device *WSDevice) {
	if device == nil || !device.unregistered.CompareAndSwap(false, true) {
		return
	}

	var addrShard *connShard
	if device.Conn != nil {
		addr := device.Conn.RemoteAddr().String()
		addrShard = m.getShardByAddr(addr)
		addrShard.mu.Lock()
		if addrShard.connMap[addr] == device {
			delete(addrShard.connMap, addr)
		}
		addrShard.mu.Unlock()
	}
	device.stateMu.Lock()
	device.IsOnline = false
	device.ConnState = StateDisconnected
	device.stateMu.Unlock()

	key := getDeviceKey(device)
	if device.DeviceType == DeviceTypeGhost {
		userShard := m.getShardByUserID(device.UserID)
		rxGroupIDs := device.GetRxGroupIDs()
		userShard.mu.Lock()
		if userShard.ghostDevices[device.SessionID] == device {
			delete(userShard.ghostDevices, device.SessionID)
			if ownerSet := userShard.ownerDevices[device.UserID]; ownerSet != nil {
				delete(ownerSet, device.SessionID)
				if len(ownerSet) == 0 {
					delete(userShard.ownerDevices, device.UserID)
				}
			}
			m.globalGroupIndex.mu.Lock()
			for _, groupID := range rxGroupIDs {
				userShard.removeFromGroupIndexInShard(groupID, key)
				m.removeFromGlobalGroupIndexLocked(groupID, key)
			}
			m.globalGroupIndex.mu.Unlock()
		}
		userShard.mu.Unlock()
		ghostsession.Global.Remove(device.SessionID)
	} else if addrShard != nil {
		addrShard.mu.Lock()
		addrShard.removeFromGroupIndexInShard(device.GroupID, key)
		m.removeFromGlobalGroupIndex(device.GroupID, key)
		addrShard.mu.Unlock()
	}

	if device.connectionRegistered {
		atomic.AddInt64(&m.totalConnections, -1)
	}
	log.Printf("[WS] Device unregistered: %s", key)
}

// DisconnectDevice removes one exact session before closing its transport.
// Deferred cleanup from the reader loop is idempotent and cannot remove a
// replacement session.
func (m *WSConnectionManager) DisconnectDevice(device *WSDevice) {
	if device == nil {
		return
	}
	m.UnregisterDevice(device)
	device.StopWriter()
	if device.Conn != nil {
		_ = device.Conn.Close()
	}
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
	devices := shard.ownerDevices[userID]
	if len(devices) != 1 {
		return nil, false
	}
	for _, device := range devices {
		return device, device != nil
	}
	return nil, false
}

func (m *WSConnectionManager) GetGhostDevicesByUser(userID int) []*WSDevice {
	shard := m.getShardByUserID(userID)
	shard.mu.RLock()
	devices := shard.ownerDevices[userID]
	result := make([]*WSDevice, 0, len(devices))
	for _, device := range devices {
		if device != nil && device.isOnline() {
			result = append(result, device)
		}
	}
	shard.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].ConnectTime.Equal(result[j].ConnectTime) {
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].ConnectTime.Before(result[j].ConnectTime)
	})
	return result
}

func (m *WSConnectionManager) GetGhostSession(sessionID string) (*WSDevice, bool) {
	if sessionID == "" {
		return nil, false
	}
	for i := 0; i < shardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		device := shard.ghostDevices[sessionID]
		shard.mu.RUnlock()
		if device != nil {
			return device, true
		}
	}
	return nil, false
}

func (m *WSConnectionManager) UpdateUserCallSign(userID int, callSign string) {
	shard := m.getShardByUserID(userID)
	shard.mu.Lock()
	for _, device := range shard.ownerDevices[userID] {
		if device != nil {
			device.stateMu.Lock()
			device.CallSign = callSign
			device.stateMu.Unlock()
		}
	}
	shard.mu.Unlock()
	ghostsession.Global.UpdateOwnerCallSign(userID, callSign)
}

// IsGhostDeviceOnline 检查幽灵设备是否在线
func (m *WSConnectionManager) IsGhostDeviceOnline(userID int) bool {
	shard := m.getShardByUserID(userID)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	for _, device := range shard.ownerDevices[userID] {
		if device != nil && device.isOnlineState(StateOnline) {
			return true
		}
	}
	return false
}

func (m *WSConnectionManager) IsLegacyGhostDeviceOnline(userID int) bool {
	shard := m.getShardByUserID(userID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	for _, device := range shard.ownerDevices[userID] {
		if device != nil && device.LegacySession && device.isOnlineState(StateOnline) {
			return true
		}
	}
	return false
}

// GetAllOnlineDevices 获取所有在线设备
func (m *WSConnectionManager) GetAllOnlineDevices() []*WSDevice {
	devices := make([]*WSDevice, 0)

	// 遍历所有分片
	for i := 0; i < shardCount; i++ {
		shard := m.shards[i]
		shard.mu.RLock()
		for _, device := range shard.ghostDevices {
			if device.isOnline() {
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
		if device.isOnline() {
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
			if device.isOnline() {
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
	now := time.Now()
	device.stateMu.Lock()
	device.LastPacketTime = now
	device.stateMu.Unlock()
	ghostsession.Global.UpdateActivity(device.SessionID, "", now)
}

// RegisterGhostDevice 注册幽灵设备
func (m *WSConnectionManager) RegisterGhostDevice(device *WSDevice, userID int, username, callsign, nickname string, ssid byte) error {
	if device == nil || device.SessionID == "" {
		return errors.New("ghost session id is required")
	}
	shard := m.getShardByUserID(userID)

	shard.mu.Lock()
	if existing := shard.ghostDevices[device.SessionID]; existing != nil && existing != device {
		shard.mu.Unlock()
		return errors.New("ghost session id is already registered")
	}
	ssid = fixedWebGhostSSID

	device.stateMu.Lock()
	device.DeviceType = DeviceTypeGhost
	device.UserID = userID
	device.Username = username
	device.CallSign = callsign
	device.Nickname = nickname
	device.SSID = ssid
	device.IsOnline = true
	device.ConnState = StateOnline
	device.stateMu.Unlock()

	shard.ghostDevices[device.SessionID] = device
	if shard.ownerDevices[userID] == nil {
		shard.ownerDevices[userID] = make(map[string]*WSDevice)
	}
	shard.ownerDevices[userID][device.SessionID] = device

	key := getDeviceKey(device)
	m.globalGroupIndex.mu.Lock()
	for _, groupID := range device.GetRxGroupIDs() {
		shard.addToGroupIndexInShard(groupID, key, device)
		m.addToGlobalGroupIndexLocked(groupID, key, device)
	}
	m.globalGroupIndex.mu.Unlock()
	shard.mu.Unlock()

	log.Printf("[WS] Ghost session registered: session=%s user=%d model=%d tx_group=%d rx_groups=%v", device.SessionID, userID, device.DevModel, device.GetGroupID(), device.GetRxGroupIDs())
	return nil
}

func (m *WSConnectionManager) SetDeviceRouting(device *WSDevice, routing ghostsession.Routing) error {
	if device == nil {
		return ErrDeviceNotFound
	}
	routing, err := ghostsession.NormalizeRouting(routing, ghostsession.DefaultMaxSubscriptions)
	if err != nil {
		return err
	}
	// 使用 userID 确定分片（幽灵设备）
	var shard *connShard
	if device.DeviceType == DeviceTypeGhost {
		shard = m.getShardByUserID(device.UserID)
	} else if device.Conn != nil {
		shard = m.getShardByAddr(device.Conn.RemoteAddr().String())
	} else {
		return ErrDeviceNotFound
	}

	shard.mu.Lock()
	if device.DeviceType == DeviceTypeGhost && shard.ghostDevices[device.SessionID] != device {
		shard.mu.Unlock()
		return ErrDeviceNotFound
	}

	key := getDeviceKey(device)
	oldRxGroupIDs := device.GetRxGroupIDs()
	m.globalGroupIndex.mu.Lock()
	for _, groupID := range oldRxGroupIDs {
		shard.removeFromGroupIndexInShard(groupID, key)
		m.removeFromGlobalGroupIndexLocked(groupID, key)
	}
	device.setRouting(routing)
	for _, groupID := range routing.RxGroupIDs {
		shard.addToGroupIndexInShard(groupID, key, device)
		m.addToGlobalGroupIndexLocked(groupID, key, device)
	}
	m.globalGroupIndex.mu.Unlock()
	shard.mu.Unlock()
	log.Printf("[WS] Ghost session routing changed: session=%s tx=%d rx=%v", device.SessionID, routing.TxGroupID, routing.RxGroupIDs)
	sendRoutingUpdated(device)
	return nil
}

// SetDeviceGroup keeps the legacy single-channel projection.
func (m *WSConnectionManager) SetDeviceGroup(device *WSDevice, newGroupID int) {
	_ = m.SetDeviceRouting(device, ghostsession.Routing{TxGroupID: newGroupID, RxGroupIDs: []int{newGroupID}})
}

// ErrDeviceNotFound 设备未找到错误
var ErrDeviceNotFound = errors.New("device not found")
