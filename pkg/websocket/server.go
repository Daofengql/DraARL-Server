package websocket

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
		return true // 允���所有来源
		},
	}

	connections = make(map[string]*websocket.Conn, 20)
	connMutex   sync.RWMutex
)

// HandleWebSocket WebSocket 处理器
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("WebSocket connected: %s", remoteAddr)

	// 保存连接
	connMutex.Lock()
	connections[remoteAddr] = conn
	connMutex.Unlock()

	// 启动 ping/pong
	go startPingPong(conn)

	// 处理消息
	go handleConnection(conn, remoteAddr)
}

// startPingPong 启动 ping/pong 保持连接
func startPingPong(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
			log.Printf("WebSocket ping failed: %v", err)
			conn.Close()
			return
		}
	}
}

// handleConnection 处理连接
func handleConnection(conn *websocket.Conn, remoteAddr string) {
	defer func() {
		conn.Close()
		connMutex.Lock()
		delete(connections, remoteAddr)
		connMutex.Unlock()
		log.Printf("WebSocket disconnected: %s", remoteAddr)
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		// 处理接收到的消息
		handleMessage(remoteAddr, messageType, message)
	}
}

// handleMessage 处理消息
func handleMessage(remoteAddr string, messageType int, message []byte) {
	// 这里可以根据业务需求处理消息
	log.Printf("WebSocket message from %s: %s", remoteAddr, string(message))

	// 示例：将消息转换为大写后返回
	if messageType == websocket.TextMessage {
		response := fmt.Sprintf("Echo: %s", string(message))
		broadcast([]byte(response))
	}
}

// Broadcast 广播消息到所有连接
func Broadcast(message []byte) {
	broadcast(message)
}

// broadcast 广播消息到所有连接
func broadcast(message []byte) {
	connMutex.RLock()
	defer connMutex.RUnlock()

	for addr, conn := range connections {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("WebSocket send to %s failed: %v", addr, err)
		}
	}
}

// SendToClient 发送消息到指定客户端
func SendToClient(remoteAddr string, message []byte) error {
	connMutex.RLock()
	defer connMutex.RUnlock()

	conn, ok := connections[remoteAddr]
	if !ok {
		return fmt.Errorf("connection not found: %s", remoteAddr)
	}

	return conn.WriteMessage(websocket.TextMessage, message)
}

// GetConnectedClients 获取已连接的客户端列表
func GetConnectedClients() []string {
	connMutex.RLock()
	defer connMutex.RUnlock()

	clients := make([]string, 0, len(connections))
	for addr := range connections {
		clients = append(clients, addr)
	}
	return clients
}

// GetConnectionCount 获取连接数
func GetConnectionCount() int {
	connMutex.RLock()
	defer connMutex.RUnlock()
	return len(connections)
}
