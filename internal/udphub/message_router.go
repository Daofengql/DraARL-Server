package udphub

import (
	"log"

	"nrllink/internal/interfaces"
	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

// 消息路由器使用 interfaces.WSDeviceInterface 和 interfaces.WSManagerInterface
// 来解耦 udphub 和 websocket 包

// MessageRouter 消息路由器
// 负责 UDP 和 WebSocket 之间的消息转发
type MessageRouter struct {
	wsManager interfaces.WSManagerInterface
}

// NewMessageRouter 创建消息路由器
func NewMessageRouter(wsManager interfaces.WSManagerInterface) *MessageRouter {
	return &MessageRouter{
		wsManager: wsManager,
	}
}

// SetWSManager 设置 WebSocket 管理器
func (r *MessageRouter) SetWSManager(wsManager interfaces.WSManagerInterface) {
	r.wsManager = wsManager
}

// RouteVoiceFromUDP 转发 UDP 语音到 WebSocket 设备
// 当 UDP 设备发送语音时，转发到同组的所有 WebSocket 设备
func (r *MessageRouter) RouteVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		return
	}

	// 获取群组内的所有在线 WebSocket 设备
	devices := r.wsManager.GetDevicesByGroup(groupID)

	for _, device := range devices {
		// 不转发给自己（如果是普通设备）
		if !device.IsGhost() && device.GetDeviceID() == source.ID {
			continue
		}

		// 检查目标设备是否禁收
		if device.IsDisabledRecv() {
			continue
		}

		// 发送语音数据
		if err := r.wsManager.SendToDevice(device, data, 2); err != nil { // 2 = websocket.BinaryMessage
			log.Printf("[ROUTE] Failed to send voice to WS device %s: %v", device.GetIdentifier(), err)
		}
	}
}

// RouteTextFromUDP 转发 UDP 文本消息到 WebSocket 设备
func (r *MessageRouter) RouteTextFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		return
	}

	devices := r.wsManager.GetDevicesByGroup(groupID)

	for _, device := range devices {
		// 不转发给自己
		if !device.IsGhost() && device.GetDeviceID() == source.ID {
			continue
		}

		if device.IsDisabledRecv() {
			continue
		}

		if err := r.wsManager.SendToDevice(device, data, 2); err != nil {
			log.Printf("[ROUTE] Failed to send text to WS device %s: %v", device.GetIdentifier(), err)
		}
	}
}

// RouteServerVoiceFromUDP 转发 UDP 服务器互联语音到 WebSocket 设备
func (r *MessageRouter) RouteServerVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		return
	}

	devices := r.wsManager.GetDevicesByGroup(groupID)

	for _, device := range devices {
		if device.IsDisabledRecv() {
			continue
		}

		if err := r.wsManager.SendToDevice(device, data, 2); err != nil {
			log.Printf("[ROUTE] Failed to send server voice to WS device %s: %v", device.GetIdentifier(), err)
		}
	}
}

// RouteVoiceToUDP 转发 WebSocket 语音到 UDP 设备
// 当 WebSocket 设备发送语音时，通过 UDP 发送到同组的所有 UDP 设备
func (r *MessageRouter) RouteVoiceToUDP(source interfaces.WSDeviceInterface, opusData []byte, groupID int) {
	conn := GetGlobalConn()
	if conn == nil {
		return
	}

	// 获取群组信息
	group, exists := GetGroupFromCache(groupID)
	if !exists {
		return
	}

	// 检查群组是否已禁用
	if group.Status != 1 {
		return
	}

	// 编码服务器互联语音包
	serverVoiceData := protocol.EncodeServerVoice(
		source.GetUsername(),
		source.GetCallSign(),
		source.GetSSID(),
		source.GetDevModel(),
		0, // DMRID
		source.GetUsername(),
		source.GetCallSign(),
		nil, // OriginalIP
		opusData,
	)

	// 获取群组连接池
	pool, ok := group.ConnPool.(*CurrentConnPool)
	if !ok {
		return
	}

	// 遍历 UDP 设备并发送
	for _, targetDev := range pool.DevConnList {
		// 不转发给自己（如果是普通设备）
		if !source.IsGhost() && targetDev.ID == source.GetDeviceID() {
			continue
		}

		// 检查目标设备是否禁收
		if targetDev.DisableRecv {
			continue
		}

		// 懒剔除：检查目标设备是否还属于本群组
		if targetDev.GroupID != groupID {
			continue
		}

		if targetDev.UDPAddr != nil && targetDev.ISOnline {
			conn.WriteToUDP(serverVoiceData, targetDev.UDPAddr)
		}
	}

	// 转发到互联组
	r.routeServerVoiceToLinkedGroups(source, serverVoiceData, groupID)
}

// RouteTextToUDP 转发 WebSocket 文本消息到 UDP 设备
func (r *MessageRouter) RouteTextToUDP(source interfaces.WSDeviceInterface, textData []byte, groupID int) {
	conn := GetGlobalConn()
	if conn == nil {
		return
	}

	group, exists := GetGroupFromCache(groupID)
	if !exists || group.Status != 1 {
		return
	}

	// 编码文本消息包
	textPacket := protocol.EncodeDraARLv1(
		source.GetUsername(),
		"",
		source.GetSSID(),
		protocol.DraARLTypeTextMessage,
		source.GetDevModel(),
		0,
		source.GetCallSign(),
		textData,
	)

	pool, ok := group.ConnPool.(*CurrentConnPool)
	if !ok {
		return
	}

	for _, targetDev := range pool.DevConnList {
		if !source.IsGhost() && targetDev.ID == source.GetDeviceID() {
			continue
		}

		if targetDev.DisableRecv || targetDev.GroupID != groupID {
			continue
		}

		if targetDev.UDPAddr != nil && targetDev.ISOnline {
			conn.WriteToUDP(textPacket, targetDev.UDPAddr)
		}
	}

	// 转发到互联组
	r.routeTextToLinkedGroups(source, textPacket, groupID)
}

// routeServerVoiceToLinkedGroups 转发服务器互联语音到关联群组
func (r *MessageRouter) routeServerVoiceToLinkedGroups(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	conn := GetGlobalConn()
	if conn == nil {
		return
	}

	// 获取该群组所属的所有互联组
	linkGroupIDs := GetLinkGroupsForTarget(sourceGroupID)
	if len(linkGroupIDs) == 0 {
		return
	}

	for _, linkGroupID := range linkGroupIDs {
		linkGroup, exists := GetGroupFromCache(linkGroupID)
		if !exists || linkGroup.Status != 1 {
			continue
		}

		targetGroupIDs := GetTargetGroupsForLink(linkGroupID)
		for _, targetID := range targetGroupIDs {
			if targetID == sourceGroupID {
				continue
			}

			targetGroup, exists := GetGroupFromCache(targetID)
			if !exists {
				continue
			}

			pool, ok := targetGroup.ConnPool.(*CurrentConnPool)
			if !ok {
				continue
			}

			for _, targetDev := range pool.DevConnList {
				if targetDev.GroupID != targetID || targetDev.DisableRecv {
					continue
				}

				if targetDev.UDPAddr != nil && targetDev.ISOnline {
					conn.WriteToUDP(data, targetDev.UDPAddr)
				}
			}

			// 同时转发到该群组的 WebSocket 设备
			if r.wsManager != nil {
				wsDevices := r.wsManager.GetDevicesByGroup(targetID)
				for _, device := range wsDevices {
					if device.IsDisabledRecv() {
						continue
					}
					r.wsManager.SendToDevice(device, data, 2)
				}
			}
		}
	}
}

// routeTextToLinkedGroups 转发文本消息到关联群组
func (r *MessageRouter) routeTextToLinkedGroups(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	conn := GetGlobalConn()
	if conn == nil {
		return
	}

	linkGroupIDs := GetLinkGroupsForTarget(sourceGroupID)
	if len(linkGroupIDs) == 0 {
		return
	}

	for _, linkGroupID := range linkGroupIDs {
		linkGroup, exists := GetGroupFromCache(linkGroupID)
		if !exists || linkGroup.Status != 1 {
			continue
		}

		targetGroupIDs := GetTargetGroupsForLink(linkGroupID)
		for _, targetID := range targetGroupIDs {
			if targetID == sourceGroupID {
				continue
			}

			targetGroup, exists := GetGroupFromCache(targetID)
			if !exists {
				continue
			}

			pool, ok := targetGroup.ConnPool.(*CurrentConnPool)
			if !ok {
				continue
			}

			for _, targetDev := range pool.DevConnList {
				if targetDev.GroupID != targetID || targetDev.DisableRecv {
					continue
				}

				if targetDev.UDPAddr != nil && targetDev.ISOnline {
					conn.WriteToUDP(data, targetDev.UDPAddr)
				}
			}

			// 同时转发到该群组的 WebSocket 设备
			if r.wsManager != nil {
				wsDevices := r.wsManager.GetDevicesByGroup(targetID)
				for _, device := range wsDevices {
					if device.IsDisabledRecv() {
						continue
					}
					r.wsManager.SendToDevice(device, data, 2)
				}
			}
		}
	}
}

// GlobalMessageRouter 全局消息路由器
var GlobalMessageRouter *MessageRouter

// InitMessageRouter 初始化全局消息路由器
func InitMessageRouter() {
	GlobalMessageRouter = NewMessageRouter(nil)
	log.Println("[ROUTE] Message router initialized")
}

// SetWSManagerForRouter 设置 WebSocket 管理器
func SetWSManagerForRouter(wsManager interfaces.WSManagerInterface) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.SetWSManager(wsManager)
		log.Println("[ROUTE] WebSocket manager set for message router")
	}
}

// BroadcastVoiceToUDP 广播语音到 UDP 设备（便捷函数）
func BroadcastVoiceToUDP(source interfaces.WSDeviceInterface, opusData []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteVoiceToUDP(source, opusData, groupID)
	}
}

// BroadcastTextToUDP 广播文本消息到 UDP 设备（便捷函数）
func BroadcastTextToUDP(source interfaces.WSDeviceInterface, textData []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteTextToUDP(source, textData, groupID)
	}
}

// BroadcastVoiceFromUDP 广播 UDP 语音到 WebSocket 设备（便捷函数）
func BroadcastVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteVoiceFromUDP(source, data, groupID)
	}
}

// BroadcastTextFromUDP 广播 UDP 文本消息到 WebSocket 设备（便捷函数）
func BroadcastTextFromUDP(source *models.Device, data []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteTextFromUDP(source, data, groupID)
	}
}
