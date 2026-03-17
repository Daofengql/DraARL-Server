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
// 负责接收 WebSocket 客户端上行的 Opus 语音数据，进行录制拦截，并向下游广播
func handleVoice(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 1. 权限检查：如果设备当前被服务器禁发，则直接丢弃语音包
	if device.DisableSend {
		return
	}

	// 2. 状态刷新：标记该设备正在发送语音，用于心跳状态维护和前端 UI 展示
	GlobalManager.MarkVoiceSending(device, true)

	// ==========================================
	// 3. 通信录制核心逻辑：拦截并记录 WebSocket 客户端的上行语音数据
	// 无论设备是通过 UDP 还是 WebSocket 接入，录制系统底层都依赖 DeviceID 来区分连续的语音会话(Session)。
	// 此处对接入的 WS 设备进行严格的模式分流，确保录音精准归档。
	// ==========================================
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint

		// 安全提取群组 ID：排除无效的 0 值，防止底层产生空指针或无效的数据库外键
		if device.GroupID > 0 {
			gid := uint(device.GroupID)
			groupID = &gid
		}

		// 安全提取用户 ID：确保归属人信息准确
		if device.UserID > 0 {
			uid := uint(device.UserID)
			userID = &uid
		}

		var recordDevID int
		var recordSSID uint8

		// 严格区分认证模式：JWT 幽灵设备 vs 普通硬件设备
		if device.DeviceType == DeviceTypeGhost {
			// 【模式 A】 JWT 认证的幽灵设备（网页端）
			// 传入负数 UserID 作为录制缓冲池的 Session Key，防止与数据库中真实的 DeviceID 冲突
			// 底层 syncer 在落库时会自动将其识别为 Ghost 并提取出正确的 UserID
			recordDevID = -device.UserID

			// 强制锁死 Web 客户端的标准 SSID 为 105 (对应 DraARLDevModelBrowser)
			// 无视前端传入的 SSID，防止数据包伪造
			recordSSID = 105
		} else {
			// 【模式 B】 非 JWT 认证的普通设备（物理硬件终端）
			// 享受与 UDP 设���完全相同的待遇，传入其在数据库中映射的真实物理设备 ID
			recordDevID = device.DeviceID

			// 提取该物理终端自身配置的真实 SSID
			recordSSID = device.SSID
		}

		// 将精准构建的身份信息与纯净的 Opus 语音载荷推入全局录制缓冲管理器
		udphub.RecordCommPacket(recordDevID, recordSSID, groupID, userID, packet.DATA)
	}

	// ==========================================
	// 4. 语音数据路由转发
	// ==========================================

	// 转发语音到 UDP 设备网络（通过 udphub 的全局消息路由器下发给传统对讲机等设备）
	routeVoiceToUDP(device, packet)

	// 【安全防御】转发语音到同组的其他 WebSocket 设备
	// 放弃直接传递前端上报的 rawData，强制使用服务端鉴权后的权威身份 (Username, CallSign, SSID)
	// 对 packet 重新进行编码，杜绝恶意伪造身份透传
	routeVoiceToWS(device, packet)

	// 5. 统计信息更新：每一帧标准的 Opus 16K 数据视为 63ms 的理论时长，累加到该设备的总语音时长中
	device.VoiceTime += 63
}

// handleTextMessage 处理文本消息
func handleTextMessage(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 检查设备是否被禁发
	if device.DisableSend {
		return
	}

	// 转发消息到 UDP 设备
	routeTextToUDP(device, packet)

	// 【核心修复】：文本消息同样需要覆写权威身份，防止盲目透传
	packet.Username = device.Username
	packet.CallSign = device.CallSign
	packet.SSID = device.SSID
	authoritativeData := EncodeWSPacket(packet)

	// 转发消息到同组的其他 WS 设备
	GlobalManager.BroadcastToGroup(device.GroupID, device, authoritativeData, websocket.BinaryMessage)
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
// 【修复】：接收 *WSPacket 而不是 rawData，使用服务端权威身份重新编码
func routeVoiceToWS(device *WSDevice, packet *WSPacket) {
	// 【核心修复】：强制使用服务端鉴权后的权威身份信息覆盖前端的数据
	// 防止前端发送空身份包导致接收端显示错误的发言人
	packet.Username = device.Username
	packet.CallSign = device.CallSign
	packet.SSID = device.SSID

	// 重新编码为标准的 WebSocket 二进制数据包
	authoritativeData := EncodeWSPacket(packet)

	// 发送干净、权威的数据包
	GlobalManager.BroadcastToGroup(device.GroupID, device, authoritativeData, websocket.BinaryMessage)
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
