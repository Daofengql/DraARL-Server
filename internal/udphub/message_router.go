package udphub

import (
	"fmt"
	"log"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/models"
	"draarl/internal/protocol"
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

func buildWSSpeakerIdentity(source interfaces.WSDeviceInterface) (speakerID string, speakerLabel string) {
	if source == nil {
		return "", ""
	}

	labelBase := source.GetCallSign()
	if labelBase == "" {
		labelBase = source.GetUsername()
	}
	if labelBase == "" {
		labelBase = source.GetIdentifier()
	}
	if labelBase == "" {
		labelBase = "ws-unknown"
	}
	speakerLabel = fmt.Sprintf("%s-%d", labelBase, source.GetSSID())

	switch {
	case source.GetDeviceID() > 0:
		speakerID = fmt.Sprintf("ws_dev:%d", source.GetDeviceID())
	case source.GetUserID() > 0:
		speakerID = fmt.Sprintf("ws_user:%d:%d", source.GetUserID(), source.GetSSID())
	default:
		speakerID = fmt.Sprintf("ws_id:%s:%d", source.GetIdentifier(), source.GetSSID())
	}

	return speakerID, speakerLabel
}

// RouteVoiceFromUDP 转发 UDP 语音到 WebSocket 设备
// 当 UDP 设备发送语音时，转发到同组的所有 WebSocket 设备
func (r *MessageRouter) RouteVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		log.Println("[ROUTE_ERR] UDP -> WS 转发失败: wsManager 未初始化 (init() 可能未执行)")
		return
	}

	r.wsManager.ForEachDeviceByGroup(groupID, func(device interfaces.WSDeviceInterface) {
		// 不转发给自己（如果是普通设备）
		if !device.IsGhost() && device.GetDeviceID() == source.ID {
			return
		}

		// 检查目标设备是否禁收
		if device.IsDisabledRecv() {
			return
		}

		// 发送语音数据
		_ = r.wsManager.SendToDevice(device, data, 2) // 2 = websocket.BinaryMessage
	})
}

// RouteTextFromUDP 转发 UDP 文本消息到 WebSocket 设备
func (r *MessageRouter) RouteTextFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		return
	}

	r.wsManager.ForEachDeviceByGroup(groupID, func(device interfaces.WSDeviceInterface) {
		// 不转发给自己
		if !device.IsGhost() && device.GetDeviceID() == source.ID {
			return
		}

		if device.IsDisabledRecv() {
			return
		}

		_ = r.wsManager.SendToDevice(device, data, 2)
	})
}

// RouteServerVoiceFromUDP 转发 UDP 服务器互联语音到 WebSocket 设备
func (r *MessageRouter) RouteServerVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if r.wsManager == nil {
		return
	}

	r.wsManager.ForEachDeviceByGroup(groupID, func(device interfaces.WSDeviceInterface) {
		if device.IsDisabledRecv() {
			return
		}

		_ = r.wsManager.SendToDevice(device, data, 2)
	})
}

// RouteVoiceToUDP 转发 WebSocket 语音到 UDP 设备
// 当 WebSocket 设备发送语音时，通过 UDP 发送到同组的所有 UDP 设备
func (r *MessageRouter) RouteVoiceToUDP(source interfaces.WSDeviceInterface, opusData []byte, groupID int) {
	conn := GetGlobalConn()
	if conn == nil {
		log.Println("[ROUTE_ERR] WS -> UDP 转发失败: 全局 UDP 连接尚未初始化")
		return
	}
	if source == nil || source.IsDisabledSend() {
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

	speakerID, speakerLabel := buildWSSpeakerIdentity(source)
	if !tryAcquireHalfDuplex(groupID, speakerID, speakerLabel, time.Now()) {
		return
	}

	// 【前置逻辑说明】
	// 这里是解决 UDP 客户端收不到声音的最关键一步。
	// 我们必须放弃使用 EncodeServerVoice (会打包成 Type 6)，因为普通硬件终端不解析互联包扩展头。
	// 改为调用 EncodeDraARLv1 并指定 Type 为 protocol.DraARLTypeOpus16K (即协议中的 Type 5)，
	// 这样下发的就是 标准、纯净的 16K 语音流包，所有客户端都能正常解码播放。
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

	// 获取群组连接池（存在性已在上方校验）
	if _, ok := group.ConnPool.(*CurrentConnPool); !ok {
		return
	}

	// 1-2. 连通域 UDP fan-out（普通设备 + ghost + 互联）
	// 构造临时 source 设备视图供排除与身份使用
	srcDev := &models.Device{
		ID:       source.GetDeviceID(),
		Username: source.GetUsername(),
		SSID:     source.GetSSID(),
		CallSign: source.GetCallSign(),
		OwnerID:  source.GetUserID(),
	}
	forwardVoiceDomain(srcDev, voicePacket, groupID)

	// 3. 互联组 WS 已包含在 RouteVoiceToWSClients
	// 4. 转发到同组和互联组的其他 WS 客户端
	r.RouteVoiceToWSClients(source, voicePacket, groupID)
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

	// 文本：连通域 UDP fan-out
	srcDev := &models.Device{
		ID:       source.GetDeviceID(),
		Username: source.GetUsername(),
		SSID:     source.GetSSID(),
		CallSign: source.GetCallSign(),
		OwnerID:  source.GetUserID(),
	}
	forwardVoiceDomain(srcDev, textPacket, groupID)

	// 【新增】转发到同组和互联组的其他 WS 客户端
	r.RouteTextToWSClients(source, textPacket, groupID)
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

// BroadcastVoiceFromUDP 广播 UDP 语音到 WebSocket 设备（单群）
func BroadcastVoiceFromUDP(source *models.Device, data []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteVoiceFromUDP(source, data, groupID)
	}
}

// BroadcastTextFromUDP 广播 UDP 文本消息到 WebSocket 设备（单群）
func BroadcastTextFromUDP(source *models.Device, data []byte, groupID int) {
	if GlobalMessageRouter != nil {
		GlobalMessageRouter.RouteTextFromUDP(source, data, groupID)
	}
}

// BroadcastVoiceFromUDPDomain 将 UDP 语音转发到连通域内所有群组的 WS 客户端（一次取域）。
func BroadcastVoiceFromUDPDomain(source *models.Device, data []byte, sourceGroupID int) {
	if GlobalMessageRouter == nil || GlobalMessageRouter.wsManager == nil || source == nil {
		return
	}
	r := GlobalMessageRouter
	domainGroups := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(domainGroups) == 0 {
		domainGroups = []int{sourceGroupID}
	}
	for _, gid := range domainGroups {
		if gp, ok := GetGroupFromCache(gid); ok && gp != nil && gp.Status != 1 {
			continue
		}
		r.wsManager.ForEachDeviceByGroup(gid, func(device interfaces.WSDeviceInterface) {
			if !device.IsGhost() && device.GetDeviceID() == source.ID {
				return
			}
			if device.IsDisabledRecv() {
				return
			}
			_ = r.wsManager.SendToDevice(device, data, 2)
		})
	}
}

// BroadcastTextFromUDPDomain 将 UDP 文本转发到连通域内所有群组的 WS 客户端。
func BroadcastTextFromUDPDomain(source *models.Device, data []byte, sourceGroupID int) {
	if GlobalMessageRouter == nil || GlobalMessageRouter.wsManager == nil || source == nil {
		return
	}
	r := GlobalMessageRouter
	domainGroups := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(domainGroups) == 0 {
		domainGroups = []int{sourceGroupID}
	}
	for _, gid := range domainGroups {
		if gp, ok := GetGroupFromCache(gid); ok && gp != nil && gp.Status != 1 {
			continue
		}
		r.wsManager.ForEachDeviceByGroup(gid, func(device interfaces.WSDeviceInterface) {
			if !device.IsGhost() && device.GetDeviceID() == source.ID {
				return
			}
			if device.IsDisabledRecv() {
				return
			}
			_ = r.wsManager.SendToDevice(device, data, 2)
		})
	}
}

// RouteVoiceToWSClients 转发 WebSocket 语音到同组和互联组的其他 WS 客户端
func (r *MessageRouter) RouteVoiceToWSClients(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	if r.wsManager == nil {
		return
	}
	domainGroups := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(domainGroups) == 0 {
		domainGroups = []int{sourceGroupID}
	}
	for _, targetID := range domainGroups {
		if targetGroup, exists := GetGroupFromCache(targetID); exists && targetGroup.Status != 1 {
			continue
		}
		r.wsManager.ForEachDeviceByGroup(targetID, func(device interfaces.WSDeviceInterface) {
			if device.IsGhost() && device.GetUserID() == source.GetUserID() && device.GetSSID() == source.GetSSID() {
				return
			}
			if device.IsDisabledRecv() {
				return
			}
			_ = r.wsManager.SendToDevice(device, data, 2)
		})
	}
}

// RouteTextToWSClients 转发 WebSocket 文本消息到同组和互联组的其他 WS 客户端
func (r *MessageRouter) RouteTextToWSClients(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	if r.wsManager == nil {
		return
	}
	domainGroups := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(domainGroups) == 0 {
		domainGroups = []int{sourceGroupID}
	}
	for _, targetID := range domainGroups {
		if targetGroup, exists := GetGroupFromCache(targetID); exists && targetGroup.Status != 1 {
			continue
		}
		r.wsManager.ForEachDeviceByGroup(targetID, func(device interfaces.WSDeviceInterface) {
			if device.IsGhost() && device.GetUserID() == source.GetUserID() && device.GetSSID() == source.GetSSID() {
				return
			}
			if device.IsDisabledRecv() {
				return
			}
			_ = r.wsManager.SendToDevice(device, data, 2)
		})
	}
}
