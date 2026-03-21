package websocket

import (
	"log"
	"time"

	"nrllink/internal/interfaces"
	"nrllink/internal/protocol"
	"nrllink/internal/udphub"

	"github.com/gorilla/websocket"
)

// WSManagerAdapter WebSocket 管理器适配器
// 实现 interfaces.WSManagerInterface 接口
type WSManagerAdapter struct {
	manager *WSConnectionManager
}

// GetDevicesByGroup 获取指定群组的设备列表
func (a *WSManagerAdapter) GetDevicesByGroup(groupID int) []interfaces.WSDeviceInterface {
	devices := a.manager.GetDevicesByGroup(groupID)
	result := make([]interfaces.WSDeviceInterface, len(devices))
	for i, d := range devices {
		result[i] = d
	}
	return result
}

// SendToDevice 向设备发送数据
func (a *WSManagerAdapter) SendToDevice(device interfaces.WSDeviceInterface, data []byte, messageType int) error {
	// 通过 userID 查找 WSDevice
	if device.IsGhost() {
		userID := device.GetUserID()
		wsDevice, ok := a.manager.GetGhostDevice(userID)
		if !ok {
			return nil // 设备不在线，静默忽略
		}
		return wsDevice.Conn.WriteMessage(messageType, data)
	}
	return nil
}

// GetOnlineCount 获取在线设备数量
func (a *WSManagerAdapter) GetOnlineCount() (normalCount, ghostCount int) {
	ghostCount = a.manager.GetOnlineCount()
	return 0, ghostCount
}

// 确保 WSManagerAdapter 实现 WSManagerInterface
var _ interfaces.WSManagerInterface = (*WSManagerAdapter)(nil)

// 确保 WSDevice 实现 WSDeviceInterface
var _ interfaces.WSDeviceInterface = (*WSDevice)(nil)

// GetDeviceID 获取设备 ID（实现 WSDeviceInterface）
func (d *WSDevice) GetDeviceID() int {
	return -d.UserID // 幽灵设备使用负数 ID
}

// startHeartbeatChecker 启动心跳检查器
func startHeartbeatChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// 检查所有幽灵设备的心跳超时
		devices := GlobalManager.GetAllOnlineDevices()
		for _, device := range devices {
			if time.Since(device.LastPacketTime) > GlobalManager.HeartbeatTimeout {
				log.Printf("[WS] Ghost device heartbeat timeout: %s", device.GetIdentifier())
				device.Conn.Close()
			}
		}
	}
}

// startStatsReporter 启动统计报告器
func startStatsReporter() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ghostCount := GlobalManager.GetOnlineCount()
		totalConns := GlobalManager.GetTotalCount()
		log.Printf("[WS-STATS] Ghost devices: %d, Total connections: %d", ghostCount, totalConns)
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
		handleTextMessage(device, packet)
	case protocol.DraARLTypeConfig:
		handleConfig(device, packet)
	default:
		log.Printf("[WS] Unknown packet type %d from %s", packet.Type, device.GetIdentifier())
	}
}

// handleHeartbeat 处理心跳包
func handleHeartbeat(device *WSDevice, packet *WSPacket) {
	// 回填呼号
	response := EncodeHeartbeatResponse(packet, device.CallSign)
	if err := device.Conn.WriteMessage(websocket.BinaryMessage, response); err != nil {
		log.Printf("[WS] Heartbeat response failed: %v", err)
	}
}

// handleVoice 处理语音包
func handleVoice(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 路由语音到 UDP 设备
	udphub.BroadcastVoiceToUDP(device, packet.DATA, device.GroupID)
}

// handleTextMessage 处理文本消息
func handleTextMessage(device *WSDevice, packet *WSPacket) {
	// 路由文本消息到 UDP 设备
	udphub.BroadcastTextToUDP(device, packet.DATA, device.GroupID)
}

// handleConfig 处理配置包（群组切换）
func handleConfig(device *WSDevice, packet *WSPacket) {
	if len(packet.DATA) >= 4 {
		newGroupID := int(uint32(packet.DATA[0])<<24 | uint32(packet.DATA[1])<<16 | uint32(packet.DATA[2])<<8 | uint32(packet.DATA[3]))

		oldGroupID := device.GroupID
		if oldGroupID != newGroupID {
			// 更新 WSDevice 的群组
			GlobalManager.SetDeviceGroup(device, newGroupID)

			// 同步更新 GhostDevice 的群组
			GlobalGhostManager.SetGhostDeviceGroup(device.UserID, newGroupID)

			log.Printf("[WS] Ghost device %s switched group: %d -> %d", device.GetIdentifier(), oldGroupID, newGroupID)
		}
	}
}
