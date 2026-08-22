package udphub

import (
	"log"
	"net"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

// processDraARLPacket 处理 DraARLv1 数据包
// remoteAddr: frp转发地址（用于发送响应）
// realAddr: 真实客户端地址（用于识别设备）
func processDraARLPacket(data []byte, remoteAddr, realAddr *net.UDPAddr, conn *net.UDPConn) {
	// 【安全校验】数据包大小限制，静默丢弃（避免日志开销）
	if len(data) > protocol.DraARLv1MaxPacketSize {
		return
	}

	// 限速已在 udp reader 完成，避免 worker 侧重复计数/加锁

	packet, err := protocol.NewDraARLv1RoutingPacket(remoteAddr, data)
	if err != nil {
		log.Printf("[DECODE] DraARLv1 decode error from %v: %v", realAddr, err)
		return
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(packet)

	atomicAddPacketNumber(1)
	incomingMAC := ""
	if packet.Type == protocol.DraARLTypeHeartbeat {
		incomingMAC = protocol.ExtractHeartbeatMAC(packet.DATA)
	}

	// ==========================================
	// 【新增】JWT 认证包处理 (Type=1)
	// 幽灵设备 (DevModel 101-104) 通过 JWT Token 认证
	// ==========================================
	if packet.Type == protocol.DraARLTypeJWTAuth {
		HandleJWTAuthPacket(packet, realAddr, conn)
		return
	}

	// ==========================================
	// 【新增】SSID 合法性检查
	// 普通设备不能使用保留 SSID 范围 (100-105 和 255)
	// ==========================================
	// 先查找设备（包括幽灵设备），避免误拦截已认证的幽灵设备
	dev, isGhost := getDeviceForPacket(packet, packet.UDPAddr)

	// 只有当设备不存在（未认证的新设备）且 SSID 为保留范围时才拒绝
	if dev == nil && protocol.IsReservedSSID(packet.SSID) {
		// A non-zero tag means this was an attempted Session-bound ghost packet.
		// Do not answer an unauthenticated or forged endpoint with a generic
		// physical-device heartbeat status.
		if protocol.ReservedUint32(packet.Reserved) != 0 {
			return
		}
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusReservedSSID, "reserved_ssid")
		return
	}

	if dev == nil {
		if packet.Type != protocol.DraARLTypeHeartbeat {
			log.Printf("[AUTH] ignoring packet from unauthenticated device: %s, type: %d",
				protocol.GetUsernameSSID(packet.Username, packet.SSID), packet.Type)
			return
		}
		admission := queueDeviceAuthentication(packet, data, nil, conn, realAddr, incomingMAC, false)
		if admission == deviceAuthRejected {
			log.Printf("[AUTH] device authentication queue full, rejecting: %s", protocol.GetUsernameSSID(packet.Username, packet.SSID))
			sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "auth_busy")
		}
		return
	}

	// A runtime object may remain available for management while its current
	// authoritative entry is an edge. It cannot be reused by an old centre UDP
	// address until a heartbeat/JWT authentication takes ownership back.
	devState := dev.RuntimeSnapshot()
	remoteOwner := devState.CurrentEntryNodeID != "" && devState.CurrentEntryNodeID != "center"
	if remoteOwner && packet.Type != protocol.DraARLTypeHeartbeat {
		return
	}

	// 【S1 安全修复】普通设备语音/文本/配置报文必须来自设备已绑定源地址，
	// 防止群内成员仅凭报文头 username+ssid 冒名注入（与幽灵设备 sameUDPAddr 校验一致）。
	// 心跳包不走此校验：地址变化的心跳会在下方触发重认证并重新绑定地址。
	if !isGhost && !enforceNormalDeviceEndpointBinding(dev, packet, realAddr) {
		return
	}

	// ==========================================
	// 已存在设备的处理
	// ==========================================
	if packet.Type == protocol.DraARLTypeHeartbeat {
		currentAddr := ""
		if packet.UDPAddr != nil {
			currentAddr = packet.UDPAddr.String()
		}

		// 幽灵设备心跳处理：不验证密码，只更新状态
		if isGhost {
			// 幽灵设备已在 JWT 认证时验证过，心跳只更新活动状态
			dev.UpdateRuntime(func(current *models.Device) {
				current.LastPacketTime = packet.TimeStamp
				current.UDPAddr = packet.UDPAddr
			})
			// 继续后续处理
		} else {
			// 普通设备心跳：可能需要重新鉴权
			// 只有当设备原本处于离线状态，或者 IP 地址发生变化时才触发鉴权，节省性能
			localSessionMissing := CenterInterconnectActive() && !CenterLocalDeviceAuthoritative(dev)
			needsCenterActivation := remoteOwner || localSessionMissing || !devState.ISOnline || devState.CurrentEntryNodeID != "center"
			boundRealAddr := devState.RealUDPAddr
			if boundRealAddr == nil {
				boundRealAddr = devState.UDPAddr
			}
			currentRealAddr := realAddr
			if currentRealAddr == nil {
				currentRealAddr = packet.UDPAddr
			}
			if remoteOwner || localSessionMissing || !devState.ISOnline || boundRealAddr == nil || !sameUDPAddr(boundRealAddr, currentRealAddr) || devState.UDPAddr == nil || devState.UDPAddr.String() != currentAddr {
				admission := queueDeviceAuthentication(packet, data, dev, conn, realAddr, incomingMAC, needsCenterActivation)
				if admission == deviceAuthRejected {
					// 认证队列已满：说明认证风暴超过容量，直接拒绝且不消耗 CPU
					log.Printf("[AUTH] device re-authentication queue full, rejecting: %s", protocol.GetUsernameSSID(packet.Username, packet.SSID))
					sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "auth_busy")
					return
				}
				return
			}
			if needsCenterActivation {
				if err := activateAndPersistCenterDevice(dev); err != nil {
					log.Printf("[INTERCONNECT] activate centre device %d failed: %v", dev.ID, err)
					sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "center_session_activation_failed")
					return
				}
			}
		}
		if incomingMAC != "" {
			dev.UpdateRuntime(func(current *models.Device) {
				current.MAC = incomingMAC
			})
		}
	}
	// 已存在设备的后续处理统一收敛到 continueDraARLPacket，
	// 供同步路径与【H5】异步认证 worker 复用。
	continueDraARLPacket(packet, data, dev, conn, realAddr, isGhost)
}

// continueDraARLPacket 处理已存在设备报文的公共尾部：更新状态并按群组
// 分发给心跳/语音/文本/配置处理。同步路径（无需重认证）与异步认证
// worker（重认证成功后）都会调用本函数。
func continueDraARLPacket(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, realAddr *net.UDPAddr, isGhost bool) {
	if packet == nil || dev == nil {
		return
	}
	if (packet.Type == protocol.DraARLTypeTextMessage || packet.Type == protocol.DraARLTypeOpus16K) && !CenterLocalDeviceAuthoritative(dev) {
		return
	}
	devState := dev.RuntimeSnapshot()
	if isGhost && devState.GhostSessionID != "" {
		GlobalUDPGhostManager.UpdateSessionActivity(devState.GhostSessionID, packet.TimeStamp)
	}

	// 已存在的设备，更新状态
	traffic := int64(protocol.DraARLv1HeaderSize + len(packet.DATA))
	dev.UpdateRuntime(func(current *models.Device) {
		current.LastPacketTime = packet.TimeStamp
		current.Traffic += traffic
	})
	atomicAddTraffic(int64(protocol.DraARLv1HeaderSize + len(packet.DATA)))

	targetGroupID := devState.GroupID
	if targetGroupID == 0 {
		// 未分组设备保持在线且允许心跳/配置管理，但不进入任何语音、文本
		// 或互联转发域。绝不能再把 0 隐式映射成公共群组。
		handleNonForwardingDevicePacket(packet, data, dev, conn, realAddr, isGhost)
		return
	}

	// ==========================================
	// 架构重构：使用纯粹的全局缓存进行路由分发
	// 不再区分"私有群组"和"公共群组"，统一从数据库加载的群组缓存中查找
	// ==========================================
	gp, exists := GetGroupFromCache(targetGroupID)
	if exists {
		// 检查群组是否已禁用（Status != 1）
		if gp.Status != 1 {
			// 群组禁用只停止业务转发；心跳与配置管理仍需可用，方便用户
			// 在设备管理中把在线设备切换到其他群组。
			handleNonForwardingDevicePacket(packet, data, dev, conn, realAddr, isGhost)
			return
		}
		parseDraARL(packet, data, dev, conn, gp, realAddr, isGhost)
	} else {
		// 缓存刷新窗口或历史悬空 group_id 不应中断设备心跳；在群组
		// 恢复可用前仅关闭业务转发。
		handleNonForwardingDevicePacket(packet, data, dev, conn, realAddr, isGhost)
	}
}

func handleNonForwardingDevicePacket(
	packet *protocol.DraARLv1Packet,
	data []byte,
	dev *models.Device,
	conn *net.UDPConn,
	realAddr *net.UDPAddr,
	isGhost bool,
) {
	switch packet.Type {
	case protocol.DraARLTypeHeartbeat:
		handleDraARLHeartbeat(packet, data, dev, conn, nil, realAddr, isGhost)
	case protocol.DraARLTypeConfig:
		handleDraARLConfig(packet, dev)
	}
}

// parseDraARL 解析并处理 DraARLv1 报文
// realAddr: 真实客户端地址（用于日志和 QTH 查询）
// isGhost: 是否为 UDP 幽灵设备
func parseDraARL(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group, realAddr *net.UDPAddr, isGhost bool) {
	switch packet.Type {
	case protocol.DraARLTypeOpus16K:
		// 语音消息 (Opus 16K)
		handleDraARLVoice(packet, data, dev, conn, gp)

	case protocol.DraARLTypeHeartbeat:
		// 心跳包
		handleDraARLHeartbeat(packet, data, dev, conn, gp, realAddr, isGhost)

	case protocol.DraARLTypeConfig:
		// 设备配置
		handleDraARLConfig(packet, dev)

	case protocol.DraARLTypeTextMessage:
		// 文本消息
		handleDraARLTextMessage(packet, data, dev, conn, gp)

	default:
		log.Printf("Unknown DraARLv1 packet type: %d, %v", packet.Type, packet)
	}
}
