package websocket

import (
	"fmt"
	"log"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/protocol"
	"draarl/internal/udphub"

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

// BroadcastToGroups 为整次广播只复制一份 payload，各连接队列共享只读引用。
func (a *WSManagerAdapter) BroadcastToGroups(groupIDs []int, data []byte, messageType int, filter interfaces.WSBroadcastFilter) (sent, dropped int) {
	if a == nil || a.manager == nil || len(groupIDs) == 0 || len(data) == 0 {
		return 0, 0
	}
	var legacyPayload *sharedWritePayload
	var sourcePayload *sharedWritePayload
	defer func() {
		if legacyPayload != nil {
			legacyPayload.release()
		}
		if sourcePayload != nil {
			sourcePayload.release()
		}
	}()
	seenSessions := make(map[string]struct{})
	for _, groupID := range groupIDs {
		for _, device := range a.manager.GetDevicesByGroup(groupID) {
			if device == nil || device.IsDisabledRecv() {
				continue
			}
			if udphub.CenterIdentityOwnedByRemote(device.UserID, device.SSID) {
				continue
			}
			if filter.ExcludeDeviceID != 0 && !device.IsGhost() && device.GetDeviceID() == filter.ExcludeDeviceID {
				continue
			}
			if filter.ExcludeSessionID != "" && device.SessionID == filter.ExcludeSessionID {
				continue
			}
			if filter.ExcludeUserID != 0 && device.IsGhost() &&
				device.UserID == filter.ExcludeUserID && device.SSID == filter.ExcludeSSID {
				continue
			}
			wsFanoutCandidates.Add(1)
			sessionKey := device.SessionID
			if sessionKey == "" {
				sessionKey = fmt.Sprintf("%p", device)
			}
			if _, duplicate := seenSessions[sessionKey]; duplicate {
				wsFanoutDeduplicated.Add(1)
				continue
			}
			seenSessions[sessionKey] = struct{}{}
			payload := legacyPayload
			if filter.SourceGroupID > 0 && device.HasCapability("source_group_v1") {
				if sourcePayload == nil {
					if enriched, ok := protocol.WithSourceGroupID(data, filter.SourceGroupID); ok {
						sourcePayload = newSharedWritePayload(enriched)
					}
				}
				if sourcePayload != nil {
					payload = sourcePayload
				}
			}
			if payload == nil {
				legacyPayload = newSharedWritePayload(data)
				payload = legacyPayload
			}
			if device.asyncWriteShared(messageType, payload) {
				sent++
				wsFanoutSent.Add(1)
			} else {
				dropped++
				wsFanoutDropped.Add(1)
			}
		}
	}
	return sent, dropped
}

func (a *WSManagerAdapter) RevokeInterconnectSession(ownerID int, ssid byte, sessionID, sessionEpoch uint64) bool {
	if a == nil || a.manager == nil || ownerID <= 0 || sessionID == 0 {
		return false
	}
	for _, device := range a.manager.GetAllOnlineDevices() {
		if device == nil || device.UserID != ownerID || device.SSID != ssid {
			continue
		}
		currentID, currentEpoch := device.GetInterconnectSession()
		if currentID != sessionID || currentEpoch != sessionEpoch {
			continue
		}
		device.SetInterconnectSession(0, 0)
		if device.Conn != nil {
			_ = device.Conn.Close()
		}
		return true
	}
	return false
}

func (a *WSManagerAdapter) GetDeliveryStats() map[string]int64 {
	return getWSDeliveryStats()
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
			if time.Since(device.GetLastPacketTime()) > GlobalManager.HeartbeatTimeout {
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
	default:
		log.Printf("[WS] Unknown packet type %d from %s", packet.Type, device.GetIdentifier())
	}
}

// handleHeartbeat 处理心跳包
func handleHeartbeat(device *WSDevice, packet *WSPacket) {
	// 回填呼号（通过异步通道发送，避免写锁竞争）
	response := EncodeHeartbeatResponse(packet, device.GetCallSign())
	if !device.AsyncWrite(websocket.BinaryMessage, response) {
		log.Printf("[WS] Heartbeat response failed for %s: write channel full or closed", device.GetIdentifier())
	}
}

// handleVoice 处理语音包
func handleVoice(device *WSDevice, packet *WSPacket, rawData []byte) {
	// 1. 权限检查：如果设备当前被服务器禁发，则直接丢弃语音包
	if device.IsDisabledSend() {
		return
	}
	txGroupID := device.GetGroupID()
	if !udphub.AuthorizeCenterLocalWS(device, txGroupID) {
		return
	}
	if !udphub.AcquireCenterLocalWSVoice(device, txGroupID) {
		return
	}

	// 2. 通信录制：记录 WebSocket 客户端的上行语音数据
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint

		// 安全提取群组 ID
		if txGroupID > 0 {
			gid := uint(txGroupID)
			groupID = &gid
		}

		// 安全提取用户 ID
		if device.UserID > 0 {
			uid := uint(device.UserID)
			userID = &uid
		}

		// 使用实际的设备型号（100-105）作为 SSID
		recordSSID := device.DevModel
		sourceKey := udphub.GhostCommRecordSourceKey("ws", device.UserID, recordSSID, device.SessionID)
		sender := udphub.CommSenderSnapshot{
			Username: device.GetUsername(), CallSign: device.GetCallSign(), Nickname: device.GetNickname(), DevModel: int(device.DevModel),
		}

		udphub.RecordCommPacket(sourceKey, 0, recordSSID, groupID, userID, sender, packet.DATA)
	}

	// 3. 路由语音到 UDP 设备
	udphub.BroadcastVoiceToUDP(device, packet.DATA, txGroupID)

	// 4. 统计信息更新：每一帧标准的 Opus 16K 数据视为 63ms 的理论时长
	device.VoiceTime += 63
}

// handleTextMessage 处理文本消息
func handleTextMessage(device *WSDevice, packet *WSPacket) {
	// 1. 权限检查
	if device.IsDisabledSend() {
		return
	}
	txGroupID := device.GetGroupID()
	if !udphub.AuthorizeCenterLocalWS(device, txGroupID) {
		return
	}

	// 2. 文本消息记录：直接写入数据库
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint

		if txGroupID > 0 {
			gid := uint(txGroupID)
			groupID = &gid
		}
		if device.UserID > 0 {
			uid := uint(device.UserID)
			userID = &uid
		}

		// 使用实际的设备型号（100-105）作为 SSID
		recordSSID := device.DevModel
		sender := udphub.CommSenderSnapshot{
			Username: device.GetUsername(), CallSign: device.GetCallSign(), Nickname: device.GetNickname(), DevModel: int(device.DevModel),
		}

		udphub.RecordTextMessage(0, recordSSID, groupID, userID, sender, string(packet.DATA))
	}

	// 3. 路由文本消息到 UDP 设备
	udphub.BroadcastTextToUDP(device, packet.DATA, txGroupID)
}
