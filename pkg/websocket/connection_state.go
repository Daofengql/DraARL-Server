package websocket

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// VoiceState 语音状态
type VoiceState int

const (
	VoiceStateIdle      VoiceState = iota // 空闲
	VoiceStateSending                     // 正在发送语音
	VoiceStateReceiving                   // 正在接收语音
)

// ConnectionMonitor 连接监控器
type ConnectionMonitor struct {
	mu sync.RWMutex

	// 连接状态
	State           ConnectionState
	VoiceState      VoiceState

	// 时间记录
	ConnectTime         time.Time
	LastPacketTime      time.Time
	LastVoiceTime       time.Time
	ConnectionStartTime time.Time
	LastDisconnectTime  time.Time

	// 重连相关
	IsReconnecting   bool
	ReconnectCount   int
	PendingReconnect bool

	// 配置
	AuthTimeout       time.Duration
	HeartbeatTimeout  time.Duration
	ReconnectGrace    time.Duration
	ProxyTimeout      time.Duration
	PreReconnectTime  time.Duration
	VoiceEndTimeout   time.Duration // 语音结束检测超时（200ms）

	// 事件回调
	OnDisconnect       func()
	OnReconnect        func()
	OnVoiceStart       func()
	OnVoiceEnd         func()
	OnHeartbeatTimeout func()
}

// NewConnectionMonitor 创建新的连接监控器
func NewConnectionMonitor() *ConnectionMonitor {
	return &ConnectionMonitor{
		State:             StateDisconnected,
		VoiceState:        VoiceStateIdle,
		AuthTimeout:       30 * time.Second,
		HeartbeatTimeout:  20 * time.Second,
		ReconnectGrace:    30 * time.Second,
		ProxyTimeout:      300 * time.Second,
		PreReconnectTime:  240 * time.Second,
		VoiceEndTimeout:   200 * time.Millisecond,
	}
}

// SetConnecting 设置为连接中状态
func (m *ConnectionMonitor) SetConnecting() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = StateConnecting
	m.ConnectTime = time.Now()
	m.ConnectionStartTime = time.Now()
	m.LastPacketTime = time.Now()
}

// SetAuthenticating 设置为认证中状态
func (m *ConnectionMonitor) SetAuthenticating() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = StateAuthenticating
	m.LastPacketTime = time.Now()
}

// SetOnline 设置为在线状态
func (m *ConnectionMonitor) SetOnline() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = StateOnline
	m.ConnectionStartTime = time.Now()
	m.LastPacketTime = time.Now()
	m.IsReconnecting = false
	m.PendingReconnect = false
}

// SetDisconnecting 设置为断开中状态
func (m *ConnectionMonitor) SetDisconnecting() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = StateDisconnecting
	m.LastDisconnectTime = time.Now()
}

// SetDisconnected 设置为已断开状态
func (m *ConnectionMonitor) SetDisconnected() {
	m.mu.Lock()
	defer m.mu.Unlock()

	wasOnline := m.State == StateOnline
	m.State = StateDisconnected
	m.LastDisconnectTime = time.Now()

	if wasOnline && m.OnDisconnect != nil {
		go m.OnDisconnect()
	}
}

// SetReconnecting 设置为重连中状态
func (m *ConnectionMonitor) SetReconnecting() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.State = StateReconnecting
	m.IsReconnecting = true
	m.ReconnectCount++

	if m.OnReconnect != nil {
		go m.OnReconnect()
	}
}

// UpdateActivity 更新活动时间
func (m *ConnectionMonitor) UpdateActivity() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastPacketTime = time.Now()
}

// StartVoice 开始语音状态
func (m *ConnectionMonitor) StartVoice(sending bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldState := m.VoiceState
	m.VoiceState = VoiceStateSending
	if !sending {
		m.VoiceState = VoiceStateReceiving
	}
	m.LastVoiceTime = time.Now()

	if oldState == VoiceStateIdle && m.OnVoiceStart != nil {
		go m.OnVoiceStart()
	}
}

// EndVoice 结束语音状态
func (m *ConnectionMonitor) EndVoice() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.VoiceState != VoiceStateIdle {
		m.VoiceState = VoiceStateIdle
		if m.OnVoiceEnd != nil {
			go m.OnVoiceEnd()
		}
	}
}

// IsVoiceActive 检查语音是否活跃
func (m *ConnectionMonitor) IsVoiceActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.LastVoiceTime) < m.VoiceEndTimeout
}

// CheckAuthTimeout 检查认证超时
func (m *ConnectionMonitor) CheckAuthTimeout() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.State != StateAuthenticating && m.State != StateConnecting {
		return false
	}
	return time.Since(m.ConnectTime) > m.AuthTimeout
}

// CheckHeartbeatTimeout 检查心跳超时
func (m *ConnectionMonitor) CheckHeartbeatTimeout() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.State != StateOnline {
		return false
	}
	return time.Since(m.LastPacketTime) > m.HeartbeatTimeout
}

// ShouldPrepareReconnect 检查是否应该准备重连
func (m *ConnectionMonitor) ShouldPrepareReconnect() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.State != StateOnline {
		return false
	}
	elapsed := time.Since(m.ConnectionStartTime)
	return elapsed >= m.PreReconnectTime
}

// IsInReconnectGrace 检查是否在重连宽限期内
func (m *ConnectionMonitor) IsInReconnectGrace() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.LastDisconnectTime.IsZero() {
		return false
	}
	return time.Since(m.LastDisconnectTime) < m.ReconnectGrace
}

// CanReconnect 检查是否可以重连
func (m *ConnectionMonitor) CanReconnect() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.IsInReconnectGrace() && m.ReconnectCount < 5
}

// HandleVoiceEndForReconnect 处理语音结束后的重连
func (m *ConnectionMonitor) HandleVoiceEndForReconnect() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.PendingReconnect && m.VoiceState == VoiceStateIdle
}

// GetState 获取当前状态
func (m *ConnectionMonitor) GetState() ConnectionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.State
}

// GetVoiceState 获取语音状态
func (m *ConnectionMonitor) GetVoiceState() VoiceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.VoiceState
}

// ReconnectManager 重连管理器
type ReconnectManager struct {
	mu sync.RWMutex

	// 连接信息
	conn   *websocket.Conn
	device *WSDevice
	manager *WSConnectionManager

	// 监控器
	monitor *ConnectionMonitor

	// 重连状态
	isReconnecting       bool
	reconnectAttempts    int
	maxReconnectAttempts int

	// 会话数据
	sessionData map[string]interface{}
}

// NewReconnectManager 创建重连管理器
func NewReconnectManager(conn *websocket.Conn, device *WSDevice, manager *WSConnectionManager) *ReconnectManager {
	return &ReconnectManager{
		conn:                 conn,
		device:               device,
		manager:              manager,
		monitor:              NewConnectionMonitor(),
		maxReconnectAttempts: 5,
		sessionData:          make(map[string]interface{}),
	}
}

// GetMonitor 获取监控器
func (r *ReconnectManager) GetMonitor() *ConnectionMonitor {
	return r.monitor
}

// StartMonitoring 启动监控
func (r *ReconnectManager) StartMonitoring() {
	go r.monitorLoop()
}

// monitorLoop 监控循环
func (r *ReconnectManager) monitorLoop() {
	checkTicker := time.NewTicker(5 * time.Second)
	defer checkTicker.Stop()

	for {
		<-checkTicker.C

		r.mu.Lock()
		if r.monitor.GetState() == StateDisconnected {
			r.mu.Unlock()
			return
		}

		// 检查心跳超时
		if r.monitor.CheckHeartbeatTimeout() {
			log.Printf("[WS-RECONNECT] Heartbeat timeout for %s", r.device.GetIdentifier())
			if r.monitor.OnHeartbeatTimeout != nil {
				go r.monitor.OnHeartbeatTimeout()
			}
		}

		// 检查是否需要准备重连
		if r.monitor.ShouldPrepareReconnect() {
			if !r.monitor.IsVoiceActive() {
				r.monitor.PendingReconnect = true
				log.Printf("[WS-RECONNECT] Marked for reconnect: %s", r.device.GetIdentifier())
			}
		}

		// 检查语音结束后是否需要重连
		if r.monitor.HandleVoiceEndForReconnect() && !r.isReconnecting {
			go r.initiateReconnect()
		}

		r.mu.Unlock()
	}
}

// initiateReconnect 发起重连
func (r *ReconnectManager) initiateReconnect() {
	r.mu.Lock()
	if r.isReconnecting {
		r.mu.Unlock()
		return
	}
	r.isReconnecting = true
	r.reconnectAttempts++
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.isReconnecting = false
		r.mu.Unlock()
	}()

	log.Printf("[WS-RECONNECT] Initiating reconnect for %s (attempt %d)",
		r.device.GetIdentifier(), r.reconnectAttempts)

	// 保存会话数据
	r.saveSessionData()

	// 设置重连状态
	r.monitor.SetReconnecting()

	// 关闭旧连接
	if r.conn != nil {
		r.conn.Close()
	}
}

// saveSessionData 保存会话数据
func (r *ReconnectManager) saveSessionData() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessionData["group_id"] = r.device.GroupID
	r.sessionData["ssid"] = r.device.SSID
	r.sessionData["username"] = r.device.Username
	r.sessionData["callsign"] = r.device.CallSign
}

// OnVoicePacket 收到语音包时调用
func (r *ReconnectManager) OnVoicePacket(sending bool) {
	r.monitor.StartVoice(sending)
	r.monitor.UpdateActivity()
}

// OnVoiceEnd 语音结束时调用
func (r *ReconnectManager) OnVoiceEnd() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.monitor.EndVoice()

	// 检查是否需要重连
	if r.monitor.PendingReconnect && !r.isReconnecting {
		go r.initiateReconnect()
	}
}

// OnPacket 收到任意数据包时调用
func (r *ReconnectManager) OnPacket() {
	r.monitor.UpdateActivity()
}

// IsOnline 是否在线
func (r *ReconnectManager) IsOnline() bool {
	return r.monitor.GetState() == StateOnline
}
