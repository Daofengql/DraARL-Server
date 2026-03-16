package websocket

import (
	"log"
	"net/http"
	"time"

	"nrllink/internal/interfaces"
	"nrllink/internal/protocol"
	"nrllink/internal/udphub"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许所有来源
		},
	}

	// 全局连接管理器
	GlobalManager = NewWSConnectionManager()
)

// init 包初始化函数
// 前置逻辑：Go 语言在启动时会自动执行所有的 init 函数。
// 我们利用这个特性，在系统启动的最早期，就把 UDP 和 WS 之间的消息路由器架设好。
func init() {
	// 1. 初始化全局消息路由器 (解决 GlobalMessageRouter == nil 的问题)
	udphub.InitMessageRouter()

	// 2. 实例化适配器，包装全局的 WebSocket 连接管理器
	adapter := &WSManagerAdapter{
		manager: GlobalManager,
	}

	// 3. 将适配器注入到 udphub 的路由器中 (打通双向通信的桥梁)
	udphub.SetWSManagerForRouter(adapter)

	// 4. 启动后台维护协程
	go startHeartbeatChecker()
	go startStatsReporter()

	log.Println("[WS] WebSocket manager adapter initialized and injected into udphub router")
}

// HandleWebSocket WebSocket 处理器
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[WS] New connection from %s", remoteAddr)

	// 启动 Ping/Pong
	go startPingPong(conn)

	// 处理认证
	device, authResult := HandleAuthentication(conn, r, GlobalManager)
	if device == nil {
		log.Printf("[WS] Authentication failed from %s: %s", remoteAddr, authResult.Error)
		return
	}

	// 认证成功，处理消息
	handleAuthenticatedConnection(device)
}

// startPingPong 启动 Ping/Pong 保持连接
func startPingPong(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
			log.Printf("[WS] Ping failed: %v", err)
			conn.Close()
			return
		}
	}
}

// handleAuthenticatedConnection 处理已认证的连接
func handleAuthenticatedConnection(device *WSDevice) {
	defer func() {
		device.Conn.Close()
		GlobalManager.UnregisterDevice(device)

		// 如果是幽灵设备，从幽灵设备管理器中移除
		if device.DeviceType == DeviceTypeGhost {
			// 【修改】传入 device 本身的指针用于二次身份验证
			// 防止旧连接超时时误删新连接
			GlobalGhostManager.RemoveGhostDevice(device.UserID, device)
		}

		log.Printf("[WS] Device disconnected: %s", device.GetIdentifier())
	}()

	// 重置读取超时（认证完成后不再需要超时）
	device.Conn.SetReadDeadline(time.Time{})

	for {
		messageType, data, err := device.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Read error from %s: %v", device.GetIdentifier(), err)
			}
			break
		}

		// 只处理二进制消息（DraARLv1 协议）
		if messageType != websocket.BinaryMessage {
			continue
		}

		// 解析数据包
		packet, err := DecodeWSPacket(data)
		if err != nil {
			log.Printf("[WS] Packet decode error from %s: %v", device.GetIdentifier(), err)
			continue
		}

		// 更新活动时间
		GlobalManager.UpdateDeviceActivity(device)
		device.PacketCount++
		device.Traffic += int64(len(data))

		// 处理数据包
		handlePacket(device, packet, data)
	}
}

// handlePacket 处理数据包
func handlePacket(device *WSDevice, packet *WSPacket, rawData []byte) {
	switch packet.Type {
	case protocol.DraARLTypeHeartbeat:
		handleHeartbeat(device, packet)

	case protocol.DraARLTypeOpus16K:
		handleVoice(device, packet, rawData)

	case protocol.DraARLTypeTextMessage:
		handleTextMessage(device, packet, rawData)

	case protocol.DraARLTypeConfig:
		handleConfig(device, packet)

	case protocol.DraARLTypeServerVoice:
		handleServerVoice(device, packet, rawData)

	default:
		log.Printf("[WS] Unknown packet type %d from %s", packet.Type, device.GetIdentifier())
	}
}

// handleHeartbeat 处理心跳包
func handleHeartbeat(device *WSDevice, packet *WSPacket) {
	// 发送心跳响应（填充 CallSign）
	response := EncodeHeartbeatResponse(packet, device.CallSign)
	if err := GlobalManager.SendToDevice(device, response, websocket.BinaryMessage); err != nil {
		log.Printf("[WS] Heartbeat response failed to %s: %v", device.GetIdentifier(), err)
	}

	// 检查群组切换请求
	if len(packet.DATA) >= 4 {
		// DATA 区域可能包含群组 ID（4 字节 big-endian）
		// 暂时忽略，使用 Config 包进行群组切换
	}
}

// handleVoice 处理语音包
func handleVoice(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 检查设备是否被禁发
	if device.DisableSend {
		return
	}

	// 标记正在发送语音
	GlobalManager.MarkVoiceSending(device, true)

	// 【通信录制】录制幽灵设备的语音
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint
		gid := uint(device.GroupID)
		groupID = &gid
		uid := uint(device.UserID)
		userID = &uid

		// 使用负数 ID 表示幽灵设备
		ghostDeviceID := -device.UserID
		udphub.RecordCommPacket(ghostDeviceID, device.SSID, groupID, userID, packet.DATA)
	}

	// 转发语音到 UDP 设备（通过 udphub）
	routeVoiceToUDP(device, packet)

	// 转发语音到同组的其他 WS 设备
	routeVoiceToWS(device, rawData)

	// 更新语音统计
	device.VoiceTime += 63 // Opus 帧时长
}

// handleTextMessage 处理文本消息
func handleTextMessage(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 检查设备是否被禁发
	if device.DisableSend {
		return
	}

	// 转发消息到 UDP 设备
	routeTextToUDP(device, packet)

	// 转发消息到同组的其他 WS 设备
	GlobalManager.BroadcastToGroup(device.GroupID, device, rawData, websocket.BinaryMessage)
}

// handleConfig 处理配置包
// 注意：Config 包是服务器下发给设备的下行数据包，客户端不应上传此类型
// 群组切换现在通过 HTTP API PUT /api/radio/group 实现
func handleConfig(device *WSDevice, packet *WSPacket) {
	// Config 包是下行包，客户端上传此包应被忽略
	log.Printf("[WS] 收到非预期的 Config 上行包，忽略。设备: %s", device.GetIdentifier())
}

// handleServerVoice 处理服务器互联语音包
func handleServerVoice(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 检查设备是否被禁发
	if device.DisableSend {
		return
	}

	// 转发到同组设备
	GlobalManager.BroadcastToGroup(device.GroupID, device, rawData, websocket.BinaryMessage)
}

// routeVoiceToUDP 转发语音到 UDP 设备
func routeVoiceToUDP(device *WSDevice, packet *WSPacket) {
	// 获取语音数据
	voiceData := packet.DATA
	if len(voiceData) == 0 {
		return
	}

	// 使用 udphub 的全局消息路由器转发语音到 UDP 设备
	udphub.BroadcastVoiceToUDP(device, voiceData, device.GroupID)
}

// routeVoiceToWS 转发语音到同组的 WS 设备
func routeVoiceToWS(device *WSDevice, rawData []byte) {
	GlobalManager.BroadcastToGroup(device.GroupID, device, rawData, websocket.BinaryMessage)
}

// routeTextToUDP 转发文本消息到 UDP 设备
func routeTextToUDP(device *WSDevice, packet *WSPacket) {
	// 获取文本数据
	textData := packet.DATA
	if len(textData) == 0 {
		return
	}

	// 使用 udphub 的全局消息路由器转发文本消息到 UDP 设备
	udphub.BroadcastTextToUDP(device, textData, device.GroupID)
}

// startHeartbeatChecker 启动心跳检查器
func startHeartbeatChecker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		checkAllDevices()
	}
}

// checkAllDevices 检查所有设备的状态
func checkAllDevices() {
	devices := GlobalManager.GetAllOnlineDevices()
	for _, device := range devices {
		// 检查心跳超时
		if GlobalManager.CheckHeartbeatTimeout(device) {
			log.Printf("[WS] Device heartbeat timeout: %s", device.GetIdentifier())
			device.Conn.Close()
			continue
		}

		// 检查是否需要准备重连
		if GlobalManager.ShouldPrepareReconnect(device) {
			// 如果语音不活跃，标记为待重连
			if !GlobalManager.IsVoiceActive(device) {
				device.PendingReconnect = true
			}
		}
	}
}

// startStatsReporter 启动统计报告器
func startStatsReporter() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		reportStats()
	}
}

// reportStats 报告统计信息
func reportStats() {
	normalCount := 0
	ghostCount := 0

	devices := GlobalManager.GetAllOnlineDevices()
	for _, device := range devices {
		if device.DeviceType == DeviceTypeGhost {
			ghostCount++
		} else {
			normalCount++
		}
	}

	if normalCount > 0 || ghostCount > 0 {
		log.Printf("[WS] Stats: Normal devices=%d, Ghost devices=%d, Total=%d",
			normalCount, ghostCount, normalCount+ghostCount)
	}
}

// Broadcast 广播消息到所有连接（兼容旧接口）
func Broadcast(message []byte) {
	GlobalManager.BroadcastToGroup(999, nil, message, websocket.TextMessage)
}

// SendToClient 发送消息到指定客户端（兼容旧接口）
func SendToClient(remoteAddr string, message []byte) error {
	// 这个方法在新的架构中不再推荐使用
	// 保留用于向后兼容
	return nil
}

// GetConnectedClients 获取已连接的客户端列表（兼容旧接口）
func GetConnectedClients() []string {
	devices := GlobalManager.GetAllOnlineDevices()
	clients := make([]string, 0, len(devices))
	for _, device := range devices {
		clients = append(clients, device.GetIdentifier())
	}
	return clients
}

// GetConnectionCount 获取连接数（兼容旧接口）
func GetConnectionCount() int {
	normalCount, ghostCount := GlobalManager.GetOnlineCount()
	return normalCount + ghostCount
}

// ==========================================
// 接口适配器 (Adapter Pattern)
// 前置逻辑：解决 Go 语言中 []*WSDevice 无法直接转换为 []interfaces.WSDeviceInterface 的切片协变问题
// ==========================================

// WSManagerAdapter 充当 websocket 包和 udphub 包之间的桥梁
type WSManagerAdapter struct {
	manager *WSConnectionManager
}

// GetDevicesByGroup 获取指定群组的设备并转换为接口切片
func (a *WSManagerAdapter) GetDevicesByGroup(groupID int) []interfaces.WSDeviceInterface {
	// 1. 获取原始的 []*WSDevice 切片
	devs := a.manager.GetDevicesByGroup(groupID)

	// 2. 创建一个同等容量的接口类型切片
	result := make([]interfaces.WSDeviceInterface, len(devs))

	// 3. 逐个赋值，触发 Go 的隐式接口转换
	for i, d := range devs {
		result[i] = d
	}
	return result
}

// SendToDevice 将数据通过接口方法发送到具体的 WebSocket 设备
func (a *WSManagerAdapter) SendToDevice(device interfaces.WSDeviceInterface, data []byte, messageType int) error {
	// 使用类型断言 (Type Assertion)，将接口还原为具体的 *WSDevice 指针
	if wsDev, ok := device.(*WSDevice); ok {
		return a.manager.SendToDevice(wsDev, data, messageType)
	}
	return nil
}

// GetOnlineCount 获取当前在线的普通设备和幽灵设备数量
func (a *WSManagerAdapter) GetOnlineCount() (int, int) {
	return a.manager.GetOnlineCount()
}
