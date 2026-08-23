package udphub

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/cache"
)

// getDeviceFromMemory resolves standard physical devices. UDP ghosts are
// always resolved through their authenticated session tag.
func getDeviceFromMemory(username string, ssid byte, udpAddr *net.UDPAddr) (*models.Device, bool) {
	if username != "" {
		if dev := lookupDeviceByUsernameSSID(username, ssid); dev != nil {
			return dev, false
		}
	}
	return nil, false
}

// getDeviceForPacket enforces the UDP ghost session binding. The tag
// is only interpreted for reserved ghost SSIDs, so physical devices retain
// their existing lookup and single-endpoint behavior even if Reserved is set.
func getDeviceForPacket(packet *protocol.DraARLv1Packet, udpAddr *net.UDPAddr) (*models.Device, bool) {
	if packet == nil {
		return nil, false
	}
	tag := protocol.ReservedUint32(packet.Reserved)
	if !protocol.IsGhostSSID(packet.SSID) {
		return getDeviceFromMemory(packet.Username, packet.SSID, udpAddr)
	}
	if tag == 0 {
		ghostPacketInvalidTags.Add(1)
		return nil, false
	}
	device := GlobalUDPGhostManager.FindBySessionTag(tag)
	if device == nil {
		ghostPacketInvalidTags.Add(1)
		return nil, false
	}
	state := device.RuntimeSnapshot()
	if state.GhostSessionTag != tag {
		ghostPacketInvalidTags.Add(1)
		return nil, false
	}
	if state.Username != packet.Username || state.SSID != packet.SSID || state.DevModel != packet.DevModel {
		ghostPacketIdentityRejects.Add(1)
		return nil, false
	}
	if !sameUDPAddr(state.UDPAddr, udpAddr) {
		ghostPacketEndpointRejects.Add(1)
		return nil, false
	}
	session, exists := ghostsession.Global.FindByTag(tag)
	if !exists || !session.Connected || session.Transport != ghostsession.TransportUDP ||
		session.SessionID != state.GhostSessionID || session.OwnerID != state.OwnerID ||
		session.Username != state.Username || session.SSID != state.SSID || session.DevModel != state.DevModel {
		ghostPacketRegistryRejects.Add(1)
		return nil, false
	}
	return device, true
}

// applyClientReportedDevModel 处理客户端上报的 DevModel：
// 1) 校验协议范围；
// 2) 仅在发生变化时更新内存；
// 3) 对已落库设备同步写回 devices.dev_model。
func applyClientReportedDevModel(dev *models.Device, reportedDevModel byte) {
	if dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	if !protocol.IsValidClientReportedDevModel(reportedDevModel) {
		log.Printf("[DEV_MODEL] 忽略非法设备型号上报: device_id=%d username=%s ssid=%d reported=%d",
			state.ID, state.Username, state.SSID, reportedDevModel)
		return
	}
	if state.DevModel == reportedDevModel {
		return
	}

	oldModel := state.DevModel
	dev.UpdateRuntime(func(current *models.Device) {
		current.DevModel = reportedDevModel
	})
	if state.ID <= 0 {
		return
	}

	repo := gormdb.NewDeviceRepository()
	if err := repo.UpdateDeviceFields(state.ID, map[string]interface{}{
		"dev_model": int(reportedDevModel),
	}); err != nil {
		log.Printf("[DEV_MODEL] 持久化设备型号失败: device_id=%d old=%d new=%d err=%v",
			state.ID, oldModel, reportedDevModel, err)
		return
	}

	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		ctx := context.Background()
		_ = deviceCache.InvalidateDevice(ctx, state.ID, state.OwnerID, uint8(state.SSID))
		_ = deviceCache.InvalidateDeviceList(ctx)
		if state.GroupID > 0 {
			_ = deviceCache.InvalidateDevicesByGroup(ctx, state.GroupID)
		}
	}

	log.Printf("[DEV_MODEL] 设备型号已更新: device_id=%d old=%d new=%d",
		state.ID, oldModel, reportedDevModel)
}

// registerAuthenticatedDraARLDevice creates or restores a normal UDP device
// after the dedicated authentication pool has verified its credentials.
func registerAuthenticatedDraARLDevice(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr, conn *net.UDPConn, authResult *DeviceAuthResult, incomingMAC string) {
	if packet == nil || authResult == nil || !authResult.Success || authResult.User == nil {
		return
	}

	if existingDev := findDeviceByOwnerSSIDFromMemory(authResult.User.ID, packet.SSID); shouldRejectNormalDeviceConflictForModel(existingDev, normalDeviceConflictAddr(existingDev, packet.UDPAddr, realAddr), incomingMAC, packet.DevModel) {
		existingState := existingDev.RuntimeSnapshot()
		log.Printf("[AUTH] Device conflict rejected: owner_id=%d ssid=%d existing_addr=%v new_addr=%v",
			authResult.User.ID, packet.SSID, existingState.UDPAddr, packet.UDPAddr)
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusDeviceConflictOnline, "device_conflict_online")
		return
	}

	// 认证成功，创建或更新设备
	reportedDevModel := packet.DevModel
	if !protocol.IsValidClientReportedDevModel(reportedDevModel) {
		log.Printf("[DEV_MODEL] 新设备上报非法设备型号，回退为 Unknown: username=%s ssid=%d reported=%d",
			packet.Username, packet.SSID, packet.DevModel)
		reportedDevModel = protocol.DraARLDevModelUnknown
	}
	newDevice := &models.Device{
		Username: packet.Username,
		CallSign: authResult.CallSign,
		Nickname: authResult.User.NickName,
		SSID:     packet.SSID,
		OwnerID:  authResult.User.ID, // 设置所有者ID
		// 使用 fmt.Sprintf 安全地将数字 byte 转换为字符串拼接到呼号后
		CallSignSSID: fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID),
		DevModel:     reportedDevModel,
		MAC:          incomingMAC,
		Priority:     100,
		Status:       0,
		GroupID:      0,
		LastOnlineIP: realAddr.IP.String(),
	}

	// 保存设备到数据库。默认群组解析位于“确认不存在”之后，已有设备重连
	// 始终保留设备自己的 group_id，不会再次继承用户默认值。
	dev, err := addDevice(newDevice, func() int {
		return resolveAvailableNewDeviceDefaultGroup(authResult.User)
	})
	if err != nil {
		log.Printf("[DEVICE] Add device failed: %v, %v", err, packet.Username)
		return
	}

	if dev != nil {
		applyClientReportedDevModel(dev, packet.DevModel)
		dev.UpdateRuntime(func(current *models.Device) {
			if current.CallSign == "" {
				current.CallSign = authResult.CallSign
			}
			if current.Username == "" && authResult.User != nil {
				current.Username = authResult.User.Name
			}
			if authResult.User != nil {
				current.Nickname = authResult.User.NickName
			}
			if incomingMAC != "" {
				current.MAC = incomingMAC
			}
			current.CallSignSSID = fmt.Sprintf("%s-%d", current.CallSign, current.SSID)
			// UDPAddr 存储 frp 转发地址（用于发送响应）
			current.UDPAddr = packet.UDPAddr
			current.RealUDPAddr = realAddr
			current.ISOnline = true
			current.LastPacketTime = packet.TimeStamp
			current.OnlineTime = packet.TimeStamp
			if realAddr != nil && realAddr.IP != nil {
				current.LastOnlineIP = realAddr.IP.String()
			}
		})
		state := dev.RuntimeSnapshot()
		indexRuntimeDevice(dev)
		if dev.ID > 0 {
			if err := activateAndPersistCenterDevice(dev); err != nil {
				log.Printf("[INTERCONNECT] activate new centre device %d failed: %v", dev.ID, err)
				sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "center_session_activation_failed")
				return
			}
		}

		// 默认群组为空时只登记并保持在线，不进入任何转发池。
		if gp, ok := GetGroupFromCache(state.GroupID); state.GroupID > 0 && ok {
			attachRuntimeDeviceToGroup(gp, dev)
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, 群组: %d)",
				packet.Username, state.Name, realAddr, state.GroupID)
		} else if state.GroupID == 0 {
			log.Printf("[ONLINE] %s 的设备 %d 已登记为未分组状态，不参与转发", packet.Username, dev.ID)
		} else {
			// 已有设备可能在群组缓存切换的极短窗口内重连。保持在线并响应
			// 心跳，但在目标群组可用前不把它挂入任何转发池。
			log.Printf("[ONLINE] 设备 %d 的群组 %d 暂不在运行时缓存中，本次不参与转发", dev.ID, state.GroupID)
		}

		// 登记成功与是否已加入转发池无关；三种状态都必须响应首个心跳。
		response := protocol.EncodeHeartbeatResponse(packet, authResult.CallSign)
		if _, err := conn.WriteToUDP(response, packet.UDPAddr); err != nil {
			log.Printf("[ONLINE] 发送设备 %d 首次心跳响应失败: %v", dev.ID, err)
		}
	}
}

func resolveAvailableNewDeviceDefaultGroup(user *gormdb.User) int {
	groupID := resolveNewDeviceDefaultGroup(user)
	if groupID <= 0 {
		return 0
	}
	if group, ok := GetGroupFromCache(groupID); ok && runtimeGroupAllowsNewDevice(group) {
		return groupID
	}

	// 群组刚创建或服务正在刷新缓存时，先同步一次再登记设备。
	// 若仍不可用则安全回退为空组，避免写入一个当前无法参与转发的默认值。
	RefreshGroupCache()
	if group, ok := GetGroupFromCache(groupID); ok && runtimeGroupAllowsNewDevice(group) {
		return groupID
	}
	log.Printf("[DEVICE] 新设备默认群组 %d 在运行时不可用，回退为未分组", groupID)
	return 0
}

func runtimeGroupAllowsNewDevice(group *models.Group) bool {
	return group != nil && group.Status == 1 && !group.IsVirtual &&
		(group.Type == models.GroupTypeRelay || group.Type == models.GroupTypeReserved)
}

func resolveNewDeviceDefaultGroup(user *gormdb.User) int {
	if user == nil {
		return 0
	}
	userRepo := gormdb.NewUserRepository()
	groupID, err := userRepo.GetUserDefaultDeviceGroupID(user.ID)
	if err != nil || groupID <= 0 {
		return 0
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(groupID)
	if err != nil || group == nil || group.Status != 1 || group.IsVirtual || (group.Type != 1 && group.Type != 2) {
		if err == nil {
			_ = userRepo.SetUserDefaultDeviceGroupID(user.ID, 0)
		}
		return 0
	}
	if group.Type == 2 && !user.HasRole("admin") && group.OwerID != user.ID {
		member, memberErr := gormdb.NewGroupMemberRepository().GetVerifiedMemberByGroupAndUser(group.ID, user.ID)
		if memberErr != nil {
			return 0
		}
		if member == nil {
			_ = userRepo.SetUserDefaultDeviceGroupID(user.ID, 0)
			return 0
		}
	}
	return group.ID
}

// handleDraARLHeartbeat 处理 DraARLv1 心跳包
// realAddr: 真实客户端地址（用于日志和 QTH 查询）
// isGhost: 是否为 UDP 幽灵设备
func handleDraARLHeartbeat(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group, realAddr *net.UDPAddr, isGhost bool) {
	if packet == nil || dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	wasOnline := state.ISOnline
	currentAddr := ""
	if packet.UDPAddr != nil {
		currentAddr = packet.UDPAddr.String()
	}
	boundRealAddr := state.RealUDPAddr
	if boundRealAddr == nil {
		boundRealAddr = state.UDPAddr
	}
	currentRealAddr := realAddr
	if currentRealAddr == nil {
		currentRealAddr = packet.UDPAddr
	}
	addrChanged := !sameUDPAddr(boundRealAddr, currentRealAddr)
	realIP := ""
	if realAddr != nil && realAddr.IP != nil {
		realIP = realAddr.IP.String()
	}

	// 解析 GPS 信息 (DATA 区域前 24 字节)
	if len(packet.DATA) >= 24 {
		lat := math.Float64frombits(binary.BigEndian.Uint64(packet.DATA[0:8]))
		lon := math.Float64frombits(binary.BigEndian.Uint64(packet.DATA[8:16]))
		alt := math.Float64frombits(binary.BigEndian.Uint64(packet.DATA[16:24]))

		// 校验 GPS 坐标是否在有效范围内
		if lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 {
			if lat != 0 || lon != 0 {
				log.Printf("[GPS] %s-%d: lat=%.6f, lon=%.6f, alt=%.1fm",
					state.Username, state.SSID, lat, lon, alt)
			}
		} else {
			log.Printf("[GPS] %s-%d: 无效坐标 lat=%.6f, lon=%.6f (超出范围)",
				state.Username, state.SSID, lat, lon)
		}
	}

	// 更新设备地址和时间（UDPAddr 存储 frp 转发地址，用于发送响应）。
	// 所有已发布设备的动态状态都通过同一把 runtimeMu 更新，避免与
	// 接收者快照、在线检查和管理线程交叉读写。
	dev.UpdateRuntime(func(current *models.Device) {
		current.UDPAddr = packet.UDPAddr
		current.RealUDPAddr = currentRealAddr
		current.LastPacketTime = packet.TimeStamp
		if realIP != "" {
			current.LastOnlineIP = realIP
		}
	})
	applyClientReportedDevModel(dev, packet.DevModel)

	// 检测重连
	if addrChanged && wasOnline {
		log.Printf("[RECONNECT] DraARLv1 device %s-%d reconnected from %v to %v",
			state.Username, state.SSID, state.PreviousUDPAddr, currentAddr)
		dev.UpdateRuntime(func(current *models.Device) {
			current.ReconnectCount++
			current.PreviousUDPAddr = currentAddr
			current.IsReconnecting = true
		})
	} else if !wasOnline && !state.LastDisconnectTime.IsZero() {
		timeOffline := packet.TimeStamp.Sub(state.LastDisconnectTime)
		log.Printf("[RECOVER] DraARLv1 device %s-%d back online after %v",
			state.Username, state.SSID, timeOffline)
		dev.UpdateRuntime(func(current *models.Device) {
			current.IsReconnecting = false
		})
	}

	// 记录日志（非幽灵设备才记录）
	if !isGhost && !state.Loged && packet.TimeStamp.Sub(state.LastVoiceEndTime).Milliseconds() > 200 {
		select {
		case logBuffer <- dev:
		default:
			// A saturated audit queue must not delay heartbeat responses.
			log.Printf("[LOG] device activity log queue full: device=%d", dev.ID)
		}
		dev.UpdateRuntime(func(current *models.Device) {
			current.Loged = true
		})
	}

	// 未分组设备没有连接池，但仍需正常响应心跳并保持在线可管理。
	// UDP 幽灵设备由会话订阅索引负责接收，不能混入实体设备连接池。
	if gp != nil && !isGhost {
		syncDeviceConnPool(getGroupConnPool(gp), dev, packet.UDPAddr)
	}

	// 发送心跳响应（填充 CallSign）- 发送到 frp 转发地址
	state = dev.RuntimeSnapshot()
	response := protocol.EncodeHeartbeatResponse(packet, state.CallSign)
	if _, err := conn.WriteToUDP(response, packet.UDPAddr); err != nil {
		log.Printf("[HEARTBEAT] response send failed: device=%d addr=%v err=%v", state.ID, packet.UDPAddr, err)
	}

	if !wasOnline {
		// 新设备上线
		dev.UpdateRuntime(func(current *models.Device) {
			current.OnlineTime = packet.TimeStamp
			current.ISOnline = true
		})

		// QTH 查询使用真实 IP
		if realAddr != nil && realAddr.IP != nil {
			qth := getQTH(realAddr.IP.String())
			dev.UpdateRuntime(func(current *models.Device) {
				current.QTH = qth
			})
			state.QTH = qth
		}

		// 日志区分幽灵设备和普通设备
		groupID := 0
		if gp != nil {
			groupID = gp.ID
		}
		if isGhost {
			log.Printf("[ONLINE] UDP幽灵设备 %s-%d 已上线 (地址: %v, 群组: %d, 型号: %d)",
				state.Username, state.SSID, realAddr, groupID, state.DevModel)
		} else {
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, QTH: %v, 群组: %d, 型号: %d)",
				state.Username, state.Name, realAddr, state.QTH, groupID, state.DevModel)

			// 【配置同步】普通设备上线时同步配置（幽灵设备使用 WebSocket API）
			// 【性能修复】配置同步含多次 DB 查询 + 逐包下发，异步化避免阻塞
			// 数据面 worker 的心跳处理；dev 字段在注册后稳定，读取安全。
			syncDev := dev
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[CONFIG] 异步配置同步 panic: %v", r)
					}
				}()
				SyncDeviceConfig(syncDev)
			}()
		}

	}
}

func activateAndPersistCenterDevice(dev *models.Device) error {
	return activateAndPersistCenterDeviceContext(context.Background(), dev)
}

func activateAndPersistCenterDeviceContext(ctx context.Context, dev *models.Device) error {
	if dev == nil || dev.ID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if CenterInterconnectActive() {
		if err := ActivateCenterLocalDeviceContext(ctx, dev); err != nil {
			return err
		}
		return ctx.Err()
	}
	now := time.Now()
	if err := gormdb.NewDeviceRepository().UpdateDeviceEntryContext(ctx, dev.ID, "center", "center", 0, true, now); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	SyncRuntimeDeviceEntry(dev.ID, "center", "center", 0, true, now)
	return nil
}
