package udphub

import (
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

func activeDomainGroupIDs(sourceGroupID int) []int {
	domainGroups := GetHalfDuplexDomainGroupIDs(sourceGroupID)
	if len(domainGroups) == 0 {
		domainGroups = []int{sourceGroupID}
	}
	active := make([]int, 0, len(domainGroups))
	for _, groupID := range domainGroups {
		if group, ok := GetGroupFromCache(groupID); ok && group != nil && group.Status != 1 {
			continue
		}
		active = append(active, groupID)
	}
	return active
}

func buildWSSpeaker(source interfaces.WSDeviceInterface) halfDuplexSpeaker {
	if source == nil {
		return halfDuplexSpeaker{}
	}

	labelBase := source.GetCallSign()
	if labelBase == "" {
		labelBase = source.GetUsername()
	}
	if labelBase == "" {
		labelBase = "ws-unknown"
	}

	var key uint64
	switch {
	case source.GetDeviceID() > 0:
		key = 0x1000000000000000 | uint64(uint32(source.GetDeviceID()))
	case source.GetUserID() > 0:
		key = 0x2000000000000000 | uint64(uint32(source.GetUserID()))<<8 | uint64(source.GetSSID())
	default:
		key = 0x3000000000000000 | uint64(fnv32String(source.GetIdentifier()))<<8 | uint64(source.GetSSID())
	}
	return halfDuplexSpeaker{key: key, labelBase: labelBase, ssid: source.GetSSID()}
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

	if !tryAcquireHalfDuplex(groupID, buildWSSpeaker(source), time.Now()) {
		return
	}

	// WebSocket 与 UDP 共用 Type 5 标准 Opus 16K 语音包。
	voicePacket := protocol.EncodeDraARLv1(
		source.GetUsername(),
		"", // 准入密码转发为空
		source.GetSSID(),
		protocol.DraARLTypeOpus16K,
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
	if source == nil || source.IsDisabledSend() {
		return
	}
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

// BroadcastVoiceFromUDPDomain 将 UDP 语音转发到连通域内所有群组的 WS 客户端（一次取域）。
func BroadcastVoiceFromUDPDomain(source *models.Device, data []byte, sourceGroupID int) {
	if GlobalMessageRouter == nil || GlobalMessageRouter.wsManager == nil || source == nil {
		return
	}
	GlobalMessageRouter.wsManager.BroadcastToGroups(
		activeDomainGroupIDs(sourceGroupID), data, 2,
		interfaces.WSBroadcastFilter{ExcludeDeviceID: source.ID},
	)
}

// BroadcastTextFromUDPDomain 将 UDP 文本转发到连通域内所有群组的 WS 客户端。
func BroadcastTextFromUDPDomain(source *models.Device, data []byte, sourceGroupID int) {
	if GlobalMessageRouter == nil || GlobalMessageRouter.wsManager == nil || source == nil {
		return
	}
	GlobalMessageRouter.wsManager.BroadcastToGroups(
		activeDomainGroupIDs(sourceGroupID), data, 2,
		interfaces.WSBroadcastFilter{ExcludeDeviceID: source.ID},
	)
}

// RouteVoiceToWSClients 转发 WebSocket 语音到同组和互联组的其他 WS 客户端
func (r *MessageRouter) RouteVoiceToWSClients(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	if r.wsManager == nil {
		return
	}
	r.wsManager.BroadcastToGroups(
		activeDomainGroupIDs(sourceGroupID), data, 2,
		interfaces.WSBroadcastFilter{ExcludeUserID: source.GetUserID(), ExcludeSSID: source.GetSSID()},
	)
}

// RouteTextToWSClients 转发 WebSocket 文本消息到同组和互联组的其他 WS 客户端
func (r *MessageRouter) RouteTextToWSClients(source interfaces.WSDeviceInterface, data []byte, sourceGroupID int) {
	if r.wsManager == nil {
		return
	}
	r.wsManager.BroadcastToGroups(
		activeDomainGroupIDs(sourceGroupID), data, 2,
		interfaces.WSBroadcastFilter{ExcludeUserID: source.GetUserID(), ExcludeSSID: source.GetSSID()},
	)
}
