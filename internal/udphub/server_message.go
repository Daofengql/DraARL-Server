package udphub

import (
	"log"
	"net"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

// handleDraARLConfig 处理 DraARLv1 设备配置
func handleDraARLConfig(packet *protocol.DraARLv1Packet, dev *models.Device) {
	if packet == nil || dev == nil {
		return
	}
	// 兼容旧的控制包格式（data[0] == 2 且长度 > 512）
	if len(packet.DATA) > 512 && packet.DATA[0] == 2 {
		decoded := decodeControlPacket(packet.DATA)
		dev.UpdateRuntime(func(current *models.Device) {
			current.DeviceParm = decoded
		})
		return
	}

	// 处理新的 Config 包协议 (TLV 格式)
	if len(packet.DATA) < 1 {
		return
	}

	switch packet.DATA[0] {
	case ConfigTypeSet:
		// 设备上报配置 (DATA[0] = 0x02)
		HandleDeviceConfigReport(dev, packet.DATA)
	case ConfigTypeQuery:
		// 查询请求通常由服务器发起，设备不应发送此类型
		state := dev.RuntimeSnapshot()
		log.Printf("[CONFIG] 设备 %s-%d 发送了意外的查询请求", state.CallSign, state.SSID)
	case ConfigTypeTimeSync:
		// 时间同步响应，通常不需要处理
		state := dev.RuntimeSnapshot()
		log.Printf("[CONFIG] 设备 %s-%d 确认时间同步", state.CallSign, state.SSID)
	}
}

// handleDraARLTextMessage 处理 DraARLv1 文本消息
func handleDraARLTextMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	if gp == nil || !canSendFromDevice(dev, gp.ID) {
		return
	}
	state := dev.RuntimeSnapshot()
	forwardDraARLMessage(packet, data, dev, conn, gp.ConnPool.(*CurrentConnPool), gp)

	// 【文本消息记录】直接写入数据库
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint
		if gp != nil {
			gid := uint(gp.ID)
			groupID = &gid
		}
		// 从设备所有者获取用户ID（快照当时的归属关系）
		if state.OwnerID > 0 {
			uid := uint(state.OwnerID)
			userID = &uid
		}
		recordDeviceID := state.ID
		if protocol.IsGhostSSID(state.SSID) {
			recordDeviceID = 0
		}
		sender := CommSenderSnapshot{Username: state.Username, CallSign: state.CallSign, Nickname: state.Nickname, DevModel: int(state.DevModel)}
		RecordTextMessage(recordDeviceID, uint8(state.SSID), groupID, userID, sender, string(packet.DATA))
	}
}

// forwardDraARLMessage 转发 DraARLv1 文本消息
func forwardDraARLMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, pool *CurrentConnPool, gp *models.Group) {
	state := dev.RuntimeSnapshot()
	refilledData := protocol.PrepareForwardPacket(
		data,
		state.Username,
		state.CallSign,
		state.SSID,
		protocol.DraARLTypeTextMessage,
		state.DevModel,
		state.DMRID,
		packet.DATA,
	)

	// 文本同样走连通域 UDP fan-out
	forwardVoiceDomain(dev, refilledData, gp.ID)
	BroadcastTextFromUDPDomain(dev, refilledData, gp.ID)
	if err := RelayCenterLocalDevice(dev, refilledData); err != nil {
		log.Printf("[INTERCONNECT] relay centre text failed: device=%d err=%v", state.ID, err)
	}
	protocol.ReleaseForwardPacket(refilledData)
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
