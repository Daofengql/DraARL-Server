package websocket

import (
	"log"
	"net/http"
	"time"

	"draarl/internal/udphub"

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
)

// 全局连接管理器
var GlobalManager = NewWSConnectionManager()

func init() {
	// 1. 初始化全局消息路由器
	udphub.InitMessageRouter()

	// 2. 实例化适配器，包装全局的 WebSocket 连接管理器
	adapter := &WSManagerAdapter{
		manager: GlobalManager,
	}

	// 3. 将适配器注入到 udphub 的路由器中
	udphub.SetWSManagerForRouter(adapter)

	// 4. 启动后台维护协程
	go startHeartbeatChecker()
	go startStatsReporter()

	log.Println("[WS] WebSocket manager adapter initialized and injected into udphub router")
}

// HandleWebSocket WebSocket 处理器
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// ==========================================
	// 【互斥检查】在 WebSocket 升级之前进行
	// 防止同一用户开多个页面导致多个幽灵设备连接
	// ==========================================
	preAuth := ParsePreAuthData(r)

	// 必须提供 JWT Token
	if preAuth.Token == "" {
		http.Error(w, "token_required", http.StatusUnauthorized)
		return
	}

	authResult := AuthenticateJWT(preAuth.Token)
	if !authResult.Success {
		http.Error(w, authResult.Error, http.StatusUnauthorized)
		return
	}

	// 【核心】互斥检查：该用户是否已有在线的幽灵设备
	if GlobalManager.IsGhostDeviceOnline(authResult.UserID) {
		log.Printf("[WS] Ghost device conflict: user-%d already has an online connection", authResult.UserID)
		http.Error(w, "ghost_device_conflict", http.StatusConflict)
		return
	}

	// ==========================================
	// 互斥检查通过，进行 WebSocket 升级
	// ==========================================
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}
	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[WS] New connection from %s", remoteAddr)
	// 处理认证
	device, authResult := HandleAuthentication(conn, r, GlobalManager)
	if device == nil {
		log.Printf("[WS] Authentication failed from %s: %s", remoteAddr, authResult.Error)
		return
	}
	// 认证成功，启动异步 writer 和 Ping/Pong
	device.StartWriter()
	go startPingPong(device)
	// 处理消息
	handleAuthenticatedConnection(device)
}

// startPingPong 启动 Ping/Pong 保持连接
// 优化：通过异步写通道发送 Ping，避免与音频写入竞争写锁
func startPingPong(device *WSDevice) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !device.WritePing() {
			log.Printf("[WS] Ping failed for %s: write channel closed", device.GetIdentifier())
			device.Conn.Close()
			return
		}
	}
}

// handleAuthenticatedConnection 处理已认证的连接（只支持幽灵设备）
func handleAuthenticatedConnection(device *WSDevice) {
	defer func() {
		device.StopWriter() // 先停止 writer goroutine
		device.Conn.Close()
		GlobalManager.UnregisterDevice(device)
		GlobalGhostManager.RemoveGhostDevice(device.UserID, device)
		log.Printf("[WS] Ghost device disconnected: %s", device.GetIdentifier())
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
