package udphub

import (
	"log"
	"net"

	"draarl/internal/ghostsession"
	"draarl/internal/groupaccess"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

// buildUDPSpeaker 构造无分配的半双工仲裁键；标签只在限频日志实际输出时格式化。
func buildUDPSpeaker(dev *models.Device, packet *protocol.DraARLv1Packet) halfDuplexSpeaker {
	if dev == nil {
		return halfDuplexSpeaker{}
	}

	ssid := dev.SSID
	if ssid == 0 && packet != nil {
		ssid = packet.SSID
	}

	labelBase := dev.CallSign
	if labelBase == "" {
		labelBase = dev.Username
	}
	if labelBase == "" && packet != nil {
		if packet.CallSign != "" {
			labelBase = packet.CallSign
		} else {
			labelBase = packet.Username
		}
	}
	if labelBase == "" {
		labelBase = "unknown"
	}

	var key uint64
	switch {
	case dev.ID > 0:
		key = 0x4000000000000000 | uint64(uint32(dev.ID))
	case dev.GhostSessionID != "":
		key = 0x5000000000000000 | (fnv64String(dev.GhostSessionID) & 0x0fffffffffffffff)
	case dev.OwnerID > 0:
		key = 0x6000000000000000 | uint64(uint32(dev.OwnerID))<<8 | uint64(ssid)
	case dev.Username != "":
		key = 0x7000000000000000 | uint64(fnv32String(dev.Username))<<8 | uint64(ssid)
	case packet != nil && packet.Username != "":
		key = 0x7000000000000000 | uint64(fnv32String(packet.Username))<<8 | uint64(ssid)
	case dev.UDPAddr != nil:
		if addr, ok := udpAddrPort(dev.UDPAddr); ok {
			key = 0x8000000000000000 | (hashAddrPort(addr) & 0x0fffffffffffffff)
		}
	default:
		key = 0x8000000000000000 | uint64(ssid)
	}
	return halfDuplexSpeaker{key: key, labelBase: labelBase, ssid: ssid}
}

func canSendFromDevice(dev *models.Device, groupID int) bool {
	return dev != nil && groupaccess.CanTransmitRoute(dev.DisableSend, dev.GroupID, groupID)
}

// handleDraARLVoice 处理 DraARLv1 语音消息
func handleDraARLVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if gp == nil || !canSendFromDevice(dev, gp.ID) {
		return
	}

	if CenterInterconnectActive() {
		if !AcquireCenterLocalDeviceVoice(dev) {
			return
		}
	} else if !tryAcquireHalfDuplex(gp.ID, buildUDPSpeaker(dev, packet), packet.TimeStamp) {
		return
	}
	if dev.GhostSessionID != "" {
		ghostsession.Global.MarkPTTActive(dev.GhostSessionID, packet.TimeStamp)
	}

	// 【前置逻辑说明】
	// 针对 60ms/帧 (动态1-3帧) 架构的优化：
	// 一个数据包最大承载 180ms 音频，自然发包间隔可达 180ms。
	// 原 200ms 阈值容错率极低（仅20ms）。现将判定阈值提升至 600ms。
	// 意味着只有当超过 600ms 没收到语音包，才判定该设备本次 PTT 发言结束。
	td := packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()

	// td > 600 表示距离上次语音已经超过 600ms，说明这是一次"新"的按键发言(PTT)
	// 此时仅记录起始时间，推迟到心跳包机制检测到语音彻底结束时，再投递最终包含时长的日志
	if td > 600 {
		dev.LastVoiceBeginTime = packet.TimeStamp
		// 将标记位置为 false，交由 handleDraARLHeartbeat 在松开 PTT 时接管日志生成
		dev.Loged = false
	}

	// 实时更新本次发言的累计持续时间
	dev.LastVoiceDuration = int(packet.TimeStamp.Sub(dev.LastVoiceBeginTime).Milliseconds())
	dev.LastVoiceEndTime = packet.TimeStamp

	// 【前置逻辑说明】时长统计优化
	// 原 63ms 硬编码不适用于 60ms/帧 (动态1-3帧) 架构。
	// 使用时间差 (td) 作为增量更准确，但首次帧时 td 可能为 0 或负数。
	// 采用保守策略：取 min(td, 180) 并确保至少 60ms（单帧最小值）
	voiceIncrement := td
	if voiceIncrement <= 0 {
		voiceIncrement = 60 // 首帧默认 60ms
	} else if voiceIncrement > 180 {
		voiceIncrement = 180 // 最大不超过 180ms（3帧）
	}
	dev.VoiceTime += voiceIncrement
	atomicAddVoiceTime(voiceIncrement)

	dev.LastCtlEndTime = packet.TimeStamp

	// 普通设备语音转发
	// 【通信录制】在转发前录制音频数据
	if len(packet.DATA) > 0 {
		var groupID *uint
		var userID *uint
		if gp != nil {
			gid := uint(gp.ID)
			groupID = &gid
		}
		// 从设备所有者获取用户ID（快照当时的归属关系，避免设备转让后历史记录跟着变）
		if dev.OwnerID > 0 {
			uid := uint(dev.OwnerID)
			userID = &uid
		}
		recordDeviceID := dev.ID
		sourceKey := PhysicalCommRecordSourceKey(recordDeviceID)
		if protocol.IsGhostSSID(dev.SSID) {
			recordDeviceID = 0
			connectionIdentity := dev.GhostSessionID
			if connectionIdentity == "" {
				connectionIdentity = "unknown"
				if dev.UDPAddr != nil {
					connectionIdentity = dev.UDPAddr.String()
				}
			}
			sourceKey = GhostCommRecordSourceKey("udp", dev.OwnerID, uint8(dev.SSID), connectionIdentity)
		}
		sender := CommSenderSnapshot{Username: dev.Username, CallSign: dev.CallSign, Nickname: dev.Nickname, DevModel: int(dev.DevModel)}
		RecordCommPacket(sourceKey, recordDeviceID, uint8(dev.SSID), groupID, userID, sender, packet.DATA)
	}

	forwardDraARLVoice(packet, dev, data, gp)
}

// forwardDraARLVoice 转发 DraARLv1 语音
func forwardDraARLVoice(packet *protocol.DraARLv1Packet, dev *models.Device, data []byte, gp *models.Group) {
	// 【核心优化】优先原地改写入站报文头（清 password、填 callsign），避免整包字段级重编码
	refilledData := protocol.PrepareForwardPacket(
		data,
		dev.Username,
		dev.CallSign,
		dev.SSID,
		protocol.DraARLTypeOpus16K,
		dev.DevModel,
		dev.DMRID,
		packet.DATA,
	)

	// 1. 连通域一次 fan-out（本群 UDP + ghost + 互联组），避免多轮扫描
	forwardVoiceDomain(dev, refilledData, gp.ID)

	// 2. WebSocket：本群 + 连通域其它组（一次遍历域）
	BroadcastVoiceFromUDPDomain(dev, refilledData, gp.ID)
	if err := RelayCenterLocalDevice(dev, refilledData); err != nil {
		log.Printf("[INTERCONNECT] relay centre voice failed: device=%d err=%v", dev.ID, err)
	}
	protocol.ReleaseForwardPacket(refilledData)
}
