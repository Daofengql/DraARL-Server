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
		log.Println("[ROUTE_ERR] UDP -> WS 转发失败: wsManager 未初始化 (init() 可能未执行)")
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
		log.Println("[ROUTE_ERR] WS -> UDP 转发失败: 全局 UDP 连接尚未初始化")
		return
	}

	// 获取群组信息
	group, exists := GetGroupFromCache(groupID)
	if !exists {
		log.Printf("[ROUTE_ERR] WS -> UDP 转发丢弃: 请求的目标群组 %d 不存在", groupID)
		return
	}

	// 检查群组是否已禁用
	if group.Status != 1 {
		log.Printf("[ROUTE_WARN] WS -> UDP 转发丢弃: 目标群组 %d 已被禁用", groupID)
		return
	}

	// 【前置逻辑说明】
	// 这里是解决 UDP 客户端收不到声音的最关键一步。
	// 我们必须放弃使用 EncodeServerVoice (会打包成 Type 6)，因为普通硬件终端不解析互联包扩展头。
	// 改为调用 EncodeDraARLv1 并指定 Type 为 protocol.DraARLTypeOpus16K (即协议中的 Type 5)，
	// 这样下发的就是��标准、纯净的 16K 语音流包，所有客户端都能正常解码播放。
	voicePacket := protocol.EncodeDraARLv1(
		source.GetUsername(),
		"", // 准入密码转发为空
		source.GetSSID(),
		protocol.DraARLTypeOpus16K, // 【核心修改】使用 Type 5：标准 Opus 16K 语音
		source.GetDevModel(),
		0, // DMRID
		source.GetCallSign(),
		opusData,
	)

	// 获取群组连接池
	pool, ok := group.ConnPool.(*CurrentConnPool)
	if !ok {
		return
	}

	// 【前置逻辑说明：WS → UDP 转发不跳过"自己"】
	// WS 设备和 UDP 设备是不同的协议栈，不存在"回声"问题。
	// 即使同一个用户通过 WS 和 UDP 同时在线，也应该正常转发。
	// 只有当 WS 幽灵设备和 UDP 幽灵设备同用户时才需要跳过（已在 forwardToGhostDevices 中处理）
	skipSelf := false
	sourceID := 0 // 不再需要排除自己

	// 1. 发送给普通 UDP 设备
	// 【前置逻辑说明：剥离批量缓冲，保障大包实时性】
	// 在 60ms-180ms 巨型音频帧架构下，批量发送器反而会造成延迟。
	// 直接使用原生 UDP 发送，将 Jitter 交给接收端的 Opus 解码器处理。
	for _, targetDev := range pool.DevConnList {
		if canForwardToDevice(targetDev, sourceID, groupID, skipSelf) {
			conn.WriteToUDP(voicePacket, targetDev.UDPAddr)
		}
	}

	// 2. 【核心修复：补全 WS 到 UDP Ghost 的桥接】
	// 前置逻辑说明：之前的代码由于缺少这行，导致 WS 网页端说话，UDP JWT 幽灵客户端永远听不见。
	forwardToGhostDevices(source.GetUsername(), source.GetSSID(), groupID, voicePacket)

	// 3. 转发到互联组 (复用这个标准语音包)
	r.routeServerVoiceToLinkedGroups(source, voicePacket, groupID)
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

	// 【前置逻辑说明：WS → UDP 转发不跳过"自己"】
	// 同 RouteVoiceToUDP，WS 和 UDP 是不同协议栈，不需要跳过
	skipSelf := false
	sourceID := 0
	for _, targetDev := range pool.DevConnList {
		if canForwardToDevice(targetDev, sourceID, groupID, skipSelf) {
			conn.WriteToUDP(textPacket, targetDev.UDPAddr)
		}
	}

	// 【核心修复：补全 WS 到 UDP Ghost 的文本消息桥接】
	forwardToGhostDevices(source.GetUsername(), source.GetSSID(), groupID, textPacket)

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

			// 转发到目标组的 UDP 设备
			for _, targetDev := range pool.DevConnList {
				if canForwardToDevice(targetDev, 0, targetID, false) {
					conn.WriteToUDP(data, targetDev.UDPAddr)
				}
			}

			// 同时转发到该群组的 WebSocket 设备
			if r.wsManager != nil {
				wsDevices := r.wsManager.GetDevicesByGroup(targetID)
				for _, device := range wsDevices {
					if !device.IsDisabledRecv() {
						r.wsManager.SendToDevice(device, data, 2)
					}
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

			// 转发到目标组的 UDP 设备
			for _, targetDev := range pool.DevConnList {
				if canForwardToDevice(targetDev, 0, targetID, false) {
					conn.WriteToUDP(data, targetDev.UDPAddr)
				}
			}

			// 同时转发到该群组的 WebSocket 设备
			if r.wsManager != nil {
				wsDevices := r.wsManager.GetDevicesByGroup(targetID)
				for _, device := range wsDevices {
					if !device.IsDisabledRecv() {
						r.wsManager.SendToDevice(device, data, 2)
					}
				}
			}
		}
	}
}

// GlobalMessageRouter 全局消息路由器
var GlobalMessageRouter *MessageRouter

// InitMessageRouter 初始化全局消息路由器
func InitMessageRouter() {
	// 【前置逻辑说明】
	// 此处增加单例防重写保护。
	// 因为 websocket/server.go 的 init() 阶段已经通过 SetWSManagerForRouter 注入了适配器，
	// 如果直接覆盖赋值为 NewMessageRouter(nil)，会导致 wsManager 丢失，从而切断 UDP 到 WS 的下行链路。
	if GlobalMessageRouter == nil {
		GlobalMessageRouter = NewMessageRouter(nil)
		log.Println("[ROUTE] Message router initialized")
	} else {
		// 如果已经被初始化过（通常是带上了 wsManager），则保留现有实例，避免破坏依赖
		log.Println("[ROUTE] Message router already initialized, preserving injected dependencies")
	}
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
