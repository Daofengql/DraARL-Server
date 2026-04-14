package udphub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/geoip"
)

var deviceRegistrationLocks sync.Map

func lockDeviceRegistration(ownerID int, ssid byte) func() {
	key := getOwnerSSIDKey(ownerID, ssid)
	muAny, _ := deviceRegistrationLocks.LoadOrStore(key, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// getGroupConnPool 获取群组连接池
func getGroupConnPool(gp *models.Group) *CurrentConnPool {
	if gp.ConnPool == nil {
		return nil
	}
	return gp.ConnPool.(*CurrentConnPool)
}

// loadAllDevices 从数据库加载所有设备
func loadAllDevices() {
	repo := gormdb.NewDeviceRepository()
	devices, _, err := repo.ListDevices(10000, 1)
	if err != nil {
		log.Printf("Load devices from database failed: %v", err)
		return
	}

	devOwnerSSIDMap = make(map[string]*models.Device, len(devices))
	devUsernameSSIDMap = make(map[string]*models.Device, len(devices))
	devCallsignSSIDMap = make(map[string]*models.Device, len(devices))

	// 批量获取所有用户信息（用于获取呼号）
	userRepo := gormdb.NewUserRepository()
	userCache := make(map[int]*gormdb.User)
	for _, dev := range devices {
		if dev.OwnerID > 0 {
			if _, ok := userCache[dev.OwnerID]; !ok {
				if user, err := userRepo.GetUserByID(dev.OwnerID); err == nil && user != nil {
					userCache[dev.OwnerID] = user
				}
			}
		}
	}

	for _, dev := range devices {
		// 转换为 models.Device
		modelDev := dev.ToModelDevice()

		// 从用户缓存获取呼号
		if dev.OwnerID > 0 {
			if owner, ok := userCache[dev.OwnerID]; ok && owner != nil {
				modelDev.CallSign = owner.CallSign
				modelDev.Username = owner.Name
			}
		}

		callsignSSID := protocol.GetCallSignSSID(modelDev.CallSign, modelDev.SSID)
		modelDev.CallSignSSID = callsignSSID

		// 255 设备加入 999 群组
		if modelDev.SSID == models.SSIDServerMax || modelDev.DevModel == models.DevModelFullNet {
			modelDev.GroupID = models.GroupIDPublicMin
		}

		indexRuntimeDevice(modelDev)

		// 加入群组
		if gp, ok := publicGroupMap[modelDev.GroupID]; ok {
			gp.DevMap[modelDev.ID] = modelDev
			gp.DevList = append(gp.DevList, modelDev.ID)
		} else {
			// 如果群组不存在，加入默认群组
			if gp0, ok := publicGroupMap[0]; ok {
				gp0.DevMap[modelDev.ID] = modelDev
				gp0.DevList = append(gp0.DevList, modelDev.ID)
			}
		}
	}

	log.Printf("Loaded %d devices from database", len(devices))
}

// addDevice 添加新设备（如果已存在则从数据库加载）
func addDevice(dev *models.Device) (*models.Device, error) {
	repo := gormdb.NewDeviceRepository()
	unlock := lockDeviceRegistration(dev.OwnerID, dev.SSID)
	defer unlock()

	// 检查设备是否已存在
	existingDev, err := repo.GetDeviceByOwnerSSID(dev.OwnerID, dev.SSID)
	if err == nil && existingDev != nil {
		// 设备已存在，转换为 models.Device 并返回
		modelDev := existingDev.ToModelDevice()

		// 【关键修复】获取所有者信息填充 Username 和 CallSign
		// 设备表不存储 CallSign，需要从用户表获取
		if existingDev.OwnerID > 0 {
			userRepo := gormdb.NewUserRepository()
			if owner, err := userRepo.GetUserByID(existingDev.OwnerID); err == nil && owner != nil {
				modelDev.Username = owner.Name
				modelDev.CallSign = owner.CallSign // 从用户表获取呼号
			}
		}

		log.Printf("Device %s-%d already exists in database (ID: %d), reusing", dev.CallSign, dev.SSID, modelDev.ID)
		return modelDev, nil
	}

	// 设备不存在，创建新设备
	gormDev := gormdb.FromModelDevice(dev)
	gormDev.CreateTime = time.Now()
	gormDev.UpdateTime = time.Now()

	if err := repo.CreateDevice(gormDev); err != nil {
		if errors.Is(err, gormdb.ErrOwnerSSIDConflict) {
			existingDev, getErr := repo.GetDeviceByOwnerSSID(dev.OwnerID, dev.SSID)
			if getErr == nil && existingDev != nil {
				modelDev := existingDev.ToModelDevice()
				if existingDev.OwnerID > 0 {
					userRepo := gormdb.NewUserRepository()
					if owner, ownerErr := userRepo.GetUserByID(existingDev.OwnerID); ownerErr == nil && owner != nil {
						modelDev.Username = owner.Name
						modelDev.CallSign = owner.CallSign
					}
				}
				modelDev.CallSignSSID = protocol.GetCallSignSSID(modelDev.CallSign, modelDev.SSID)
				return modelDev, nil
			}
		}
		return nil, fmt.Errorf("add device to database failed: %w", err)
	}

	// 转换回 models.Device
	modelDev := gormDev.ToModelDevice()

	// 保留认证链路已经拿到的运行时字段，避免新设备首次上线时出现空呼号。
	modelDev.CallSign = dev.CallSign
	modelDev.Username = dev.Username

	// 获取所有者信息填充 Username/CallSign（数据库为准，补齐运行时字段）
	if gormDev.OwnerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(gormDev.OwnerID); err == nil && owner != nil {
			modelDev.Username = owner.Name
			modelDev.CallSign = owner.CallSign
		}
	}
	modelDev.CallSignSSID = protocol.GetCallSignSSID(modelDev.CallSign, modelDev.SSID)

	log.Printf("Created new device in database: %s-%d (ID: %d)", dev.CallSign, dev.SSID, modelDev.ID)
	return modelDev, nil
}

// getDevice 获取设备
func getDevice(callsign string, ssid byte) *models.Device {
	callsignSSID := protocol.GetCallSignSSID(callsign, ssid)
	if dev, ok := devCallsignSSIDMap[callsignSSID]; ok {
		return dev
	}

	// 从数据库查询
	repo := gormdb.NewDeviceRepository()
	gormDev, err := repo.GetDeviceByCallSignSSID(callsign, ssid)
	if err != nil || gormDev == nil {
		return nil
	}

	dev := gormDev.ToModelDevice()
	dev.CallSignSSID = callsignSSID

	// 获取所有者信息填充 Username
	if gormDev.OwnerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(gormDev.OwnerID); err == nil && owner != nil {
			dev.Username = owner.Name
			dev.CallSign = owner.CallSign
		}
	}

	indexRuntimeDevice(dev)
	return dev
}

// getDeviceByDMRID 通过 DMRID 获取设备
func getDeviceByDMRID(dmrid uint32) *models.Device {
	repo := gormdb.NewDeviceRepository()
	gormDev, err := repo.GetDeviceByDMRID(int64(dmrid))
	if err != nil || gormDev == nil {
		return nil
	}

	// 转换为 models.Device
	dev := gormDev.ToModelDevice()

	// 获取所有者呼号（运行时填充）
	if dev.OwnerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(dev.OwnerID); err == nil && owner != nil {
			dev.CallSign = owner.CallSign
			dev.Username = owner.Name
		}
	}

	// 检查是否已在内存中（使用 owner_id 索引）
	if existingDev := findDeviceByOwnerSSIDFromMemory(dev.OwnerID, dev.SSID); existingDev != nil {
		return existingDev
	}

	indexRuntimeDevice(dev)

	return dev
}

// changeDeviceGroup 更改设备群组
func changeDeviceGroup(dev *models.Device, groupID int) (string, error) {
	// 检查目标群组是否允许此设备加入
	if gp, ok := publicGroupMap[groupID]; ok {
		if len(gp.AllowCallSignSSID) > 0 {
			allowed := strings.Split(gp.AllowCallSignSSID, ",")
			found := false
			for _, a := range allowed {
				if a == dev.CallSignSSID {
					found = true
					break
				}
			}
			if !found {
				return "", errors.New("group does not allow this callsign")
			}
		}
	}

	// 从原群组删除
	if dev.GroupID >= models.GroupIDPublicMin || dev.GroupID == 0 {
		if oldGp, ok := publicGroupMap[dev.GroupID]; ok {
			pool := getGroupConnPool(oldGp)
			if pool != nil {
				delete(pool.DevConnMap, dev.UDPAddr.String())

				// 性能优化：预分配切片容量
				list := make([]*models.Device, 0, len(pool.DevConnMap))
				for _, vv := range pool.DevConnMap {
					list = append(list, vv)
				}
				pool.DevConnList = list
			}

			delete(oldGp.DevMap, dev.ID)
		}
	} else {
		// 从私有群组删除
		if u, ok := userList.Load(dev.CallSign); ok {
			if oldGp, ok := u.(*UserInfo).Groups[dev.GroupID]; ok {
				delete(oldGp.DevMap, dev.ID)
				pool := getGroupConnPool(oldGp)
				if pool != nil {
					delete(pool.DevConnMap, dev.UDPAddr.String())

					// 性能优化：预分配切片容量
					list := make([]*models.Device, 0, len(pool.DevConnMap))
					for _, vv := range pool.DevConnMap {
						list = append(list, vv)
					}
					pool.DevConnList = list
				}
			}
		}
	}

	// 加入新群组
	if groupID >= models.GroupIDPublicMin || groupID == 0 {
		if gp, ok := publicGroupMap[groupID]; ok {
			dev.GroupID = groupID
			gp.DevMap[dev.ID] = dev
			return fmt.Sprintf("%d%s", gp.ID, gp.Name), nil
		}
		return "", errors.New("group not found")
	} else {
		// 私有群组
		if u, ok := userList.Load(dev.CallSign); ok {
			if gp, ok := u.(*UserInfo).Groups[groupID]; ok {
				gp.DevMap[dev.ID] = dev
				dev.GroupID = groupID
				return fmt.Sprintf("%d%s", gp.ID, gp.Name), nil
			}
		}
		return "", errors.New("private group not found")
	}
}

// checkDeviceOnline 检查设备在线状态
func checkDeviceOnline() {
	if !waitWithShutdown(10 * time.Second) {
		return
	}

	// 配置参数
	const (
		checkInterval  = 5 * time.Second  // 检查间隔
		offlineTimeout = 20 * time.Second // 离线超时
		reconnectGrace = 10 * time.Second // 重连宽限期
		maxRetryLog    = 3                // 最大重复日志次数
	)

	for {
		if !waitWithShutdown(checkInterval) {
			return
		}

		// 【新增】查询数据库获取已审核通过的用户总数
		// 这代表了所有可以随时登录的幽灵设备的理论总数
		var approvedUserCount int64
		if db := gormdb.Get(); db != nil {
			userRepo := gormdb.NewUserRepository()
			approvedUserCount, _ = userRepo.GetApprovedUserCount()
		}

		onlineMap := make(map[int]*models.Device, 100)
		t := time.Now()
		totalStats.OnlineDevNumber = 0

		// 检查公共群组设备
		for _, gp := range publicGroupMap {
			change := false
			gp.OnlineDevNumber = 0

			pool := getGroupConnPool(gp)
			if pool != nil {
				for addrStr, dev := range pool.DevConnMap {
					// 255 设备不参与在线统计
					if dev.DevModel == models.DevModelFullNet || dev.SSID == models.SSIDServerMax {
						continue
					}

					// 检查地址变化（重连检测）
					if dev.UDPAddr != nil && addrStr != dev.UDPAddr.String() {
						log.Printf("[RECONNECT] Device %v-%v address changed from %v to %v",
							dev.CallSign, dev.SSID, addrStr, dev.UDPAddr.String())

						// 保存旧地址
						if dev.PreviousUDPAddr == "" {
							dev.PreviousUDPAddr = addrStr
						}

						delete(pool.DevConnMap, addrStr)
						change = true
						continue
					}

					// 计算最后包时间
					timeSinceLastPacket := t.Sub(dev.LastPacketTime)

					// 设备超时检测
					if timeSinceLastPacket > offlineTimeout {
						if dev.ISOnline {
							// 检查是否在重连宽限期内
							timeSinceDisconnect := t.Sub(dev.LastDisconnectTime)
							isGracePeriod := !dev.LastDisconnectTime.IsZero() && timeSinceDisconnect < reconnectGrace

							if isGracePeriod {
								// 在宽限期内，延长超时时间
								if timeSinceLastPacket < offlineTimeout+reconnectGrace {
									log.Printf("[GRACE] Device %v-%v in reconnection grace period, waiting... (%v since last packet)",
										dev.CallSign, dev.SSID, timeSinceLastPacket)
									gp.OnlineDevNumber++
									onlineMap[dev.ID] = dev
									continue
								}
							}

							// 确认离线
							log.Printf("[OFFLINE] %s的-%s 已下线 (群组: %d, 地址: %s, 超时: %v)",
								dev.Username, dev.Name, dev.GroupID, dev.UDPAddr, timeSinceLastPacket)

							dev.LastDisconnectTime = t
							dev.ISOnline = false
							dev.ReconnectCount++
							removeRuntimeDeviceMAC(dev)

							delete(pool.DevConnMap, addrStr)
							change = true
							continue
						}
					}

					// 设备在线
					if dev.ISOnline {
						gp.OnlineDevNumber++
						onlineMap[dev.ID] = dev
					}
				}

				// 更新连接列表
				if change {
					list := make([]*models.Device, 0, len(pool.DevConnMap))
					for _, dev := range pool.DevConnMap {
						list = append(list, dev)
					}
					pool.DevConnList = list
				}
			}

			// 【修复】更新本群组总设备数 = 实体硬件设备数 + 已审核幽灵设备准入总数
			gp.TotalDevNumber = len(gp.DevMap) + int(approvedUserCount)

			// 【修复】从 WS 管理器中获取本群组的 WS 在线设备，并叠加到在线总数中
			if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
				wsDevices := GlobalMessageRouter.wsManager.GetDevicesByGroup(gp.ID)
				gp.OnlineDevNumber += len(wsDevices)
			}

			totalStats.OnlineDevNumber += gp.OnlineDevNumber
		}

		// 检查私有群组设备
		userList.Range(func(k, v any) bool {
			u := v.(*UserInfo)
			for _, gp := range u.Groups {
				gp.OnlineDevNumber = 0

				pool := getGroupConnPool(gp)
				if pool != nil {
					for addrStr, dev := range pool.DevConnMap {
						// 跳过特殊设备
						if dev.DevModel == models.DevModelFullNet || dev.SSID == models.SSIDServerMax {
							continue
						}

						// 检查地址变化
						if dev.UDPAddr != nil && addrStr != dev.UDPAddr.String() {
							delete(pool.DevConnMap, addrStr)
							continue
						}

						timeSinceLastPacket := t.Sub(dev.LastPacketTime)

						if timeSinceLastPacket > offlineTimeout {
							if dev.ISOnline {
								log.Printf("[OFFLINE] Private group device %v-%v group %v timed out (addr: %v)",
									dev.CallSign, dev.SSID, dev.GroupID, dev.UDPAddr)

								dev.LastDisconnectTime = t
								dev.ISOnline = false
								dev.ReconnectCount++
								removeRuntimeDeviceMAC(dev)

								delete(pool.DevConnMap, addrStr)
								continue
							}
						}

						if dev.ISOnline {
							gp.OnlineDevNumber++
							onlineMap[dev.ID] = dev
						}
					}
				}

				// 【修复】更新私有群组总数 = 实体硬件设备数 + 已审核幽灵设备准入总数
				gp.TotalDevNumber = len(gp.DevMap) + int(approvedUserCount)

				// 【修复】叠加本群组的 WS 在线设备
				if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
					wsDevices := GlobalMessageRouter.wsManager.GetDevicesByGroup(gp.ID)
					gp.OnlineDevNumber += len(wsDevices)
				}

				totalStats.OnlineDevNumber += gp.OnlineDevNumber
			}
			return true
		})

		onlineDevMap = onlineMap

		// 【新增】UDP 幽灵设备超时检测
		GlobalUDPGhostManager.CheckTimeout(offlineTimeout)

		// 【新增】统计 UDP 幽灵设备在线数
		udpGhostTotal, udpGhostOnline := GlobalUDPGhostManager.GetStats()
		_ = udpGhostTotal // 避免未使用警告

		// 【日志】输出在线设备统计信息
		if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
			wsNormalCount, wsGhostCount := GlobalMessageRouter.wsManager.GetOnlineCount()
			udpOnlineCount := totalStats.OnlineDevNumber - wsNormalCount - wsGhostCount
			log.Printf("[ONLINE] 在线设备统计: 实体UDP=%d, UDP幽灵=%d, WS普通=%d, WS幽灵=%d, 服务器总在线=%d",
				udpOnlineCount, udpGhostOnline, wsNormalCount, wsGhostCount, totalStats.OnlineDevNumber+udpGhostOnline)
		}
	}
}

// processLogBuffer 处理日志缓冲
func processLogBuffer() {
	for dev := range logBuffer {
		if dev == nil {
			continue
		}
		// TODO: 记录设备操作日志
		log.Printf("Device activity: %v-%v, voice: %dms, control: %dms",
			dev.CallSign, dev.SSID, dev.LastVoiceDuration, dev.LastCtlDuration)
	}
}

// deviceAT 发送 AT 命令到设备
func deviceAT(at *models.ATCommand) (*models.Device, error) {
	usernameSSID := protocol.GetUsernameSSID(at.CallSign, at.SSID)
	dev, ok := devUsernameSSIDMap[usernameSSID]
	if !ok {
		// 向后兼容：尝试 callsign 索引
		callsignSSID := protocol.GetCallSignSSID(at.CallSign, at.SSID)
		dev, ok = devCallsignSSIDMap[callsignSSID]
		if !ok {
			return nil, errors.New("device not found")
		}
	}

	atCommand := append([]byte{at.Type}, []byte(at.ATcommand+"="+at.Data+"\r\n")...)
	packet := protocol.EncodeDraARLv1(dev.Username, "", at.SSID, protocol.DraARLTypeATPassThrough, models.DevModelServer, dev.DMRID, dev.CallSign, atCommand)

	if globalConn != nil && dev.UDPAddr != nil {
		globalConn.WriteToUDP(packet, dev.UDPAddr)
	}

	return dev, nil
}

// queryDeviceParm 查询设备参数
func queryDeviceParm(callsignSSID string) (*models.Device, error) {
	dev, ok := devCallsignSSIDMap[callsignSSID]
	if !ok {
		return nil, errors.New("device not found")
	}

	if globalConn != nil && dev.UDPAddr != nil {
		packet := protocol.EncodeDraARLv1(dev.Username, "", dev.SSID, protocol.DraARLTypeConfig, 0, 0, dev.CallSign, []byte{0x01})
		globalConn.WriteToUDP(packet, dev.UDPAddr)
		time.Sleep(300 * time.Millisecond)
	}

	return dev, nil
}

// decodeControlPacket 解析控制数据包
func decodeControlPacket(data []byte) map[string]string {
	result := make(map[string]string)

	if data[0] == 2 && len(data) > 512 {
		// 解析设备配置参数
		result["dcd_select"] = strconv.Itoa(int(data[1]))
		result["ptt_enable"] = strconv.Itoa(int(data[2]))
		result["ptt_level_reversed"] = strconv.Itoa(int(data[3]))
		result["add_tail_voice"] = strconv.Itoa(int(binary.BigEndian.Uint16(data[4:6])))
		result["remove_tail_voice"] = strconv.Itoa(int(binary.BigEndian.Uint16(data[6:8])))
		result["ptt_resistive"] = strconv.Itoa(int(data[9]))
		result["monitor"] = strconv.Itoa(int(data[10]))
		result["key_func"] = strconv.Itoa(int(data[11]))
		result["realy_status"] = strconv.Itoa(int(data[12]))
		result["allow_relay_control"] = strconv.Itoa(int(data[13]))
		result["voice_bitrate"] = strconv.Itoa(int(data[14]))

		result["dmrid"] = string(bytesTrim(data[17:27]))
		result["password"] = string(bytesTrim(data[27:32]))

		result["local_ipaddr"] = fmt.Sprintf("%v.%v.%v.%v", data[33], data[34], data[35], data[36])
		result["gateway"] = fmt.Sprintf("%v.%v.%v.%v", data[37], data[38], data[39], data[40])
		result["netmask"] = fmt.Sprintf("%v.%v.%v.%v", data[41], data[42], data[43], data[44])
		result["dns_ipaddr"] = fmt.Sprintf("%v.%v.%v.%v", data[45], data[46], data[47], data[48])

		result["ssid"] = strconv.Itoa(int(data[65]))
		result["callsign"] = string(bytesTrim(data[66:73]))
		result["dest_domainname"] = string(bytesTrim(data[80:128]))
	}

	return result
}

// decodeATPacket 解析 AT 数据包
func decodeATPacket(callsign string, ssid byte, data []byte) *models.ATCommand {
	at := &models.ATCommand{CallSign: callsign, SSID: ssid}

	if len(data) < 2 {
		log.Printf("AT command error: %s %d %v", callsign, ssid, data)
		return at
	}

	at.Type = data[0]

	if at.Type == 0x02 {
		// 解析多条 AT 命令响应
		lines := strings.Split(string(data[1:]), "\r\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "DraARL") {
				at.Data = line
				continue
			}

			kv := strings.SplitN(line, "=", 2)
			if len(kv) == 2 {
				if at.ATcommand == "" {
					at.ATcommand = kv[0]
				}
			}
		}
	}

	return at
}

// bytesTrim 去除字节中的 null 和回车
func bytesTrim(b []byte) []byte {
	return bytesTrimRight(bytesTrimRight(b, 0), 13)
}

func bytesTrimRight(b []byte, cut byte) []byte {
	for len(b) > 0 && b[len(b)-1] == cut {
		b = b[:len(b)-1]
	}
	return b
}

// getGroupListForDevice 获取设备群组列表
func getGroupListForDevice(packet []byte) []byte {
	groupList := make([]string, 0)

	for _, v := range publicGroupMap {
		groupList = append(groupList, fmt.Sprintf("%d,%s", v.ID, v.Name))
	}

	output := strings.Join(groupList, "\n")
	return append(packet, []byte(output)...)
}

// getQTH 获取 QTH 信息
func getQTH(ip string) string {
	return geoip.GetQTH(ip)
}

// SetDeviceOnline 设置设备在线状态
func SetDeviceOnline(dev *models.Device, online bool) {
	dev.ISOnline = online
	if online {
		dev.OnlineTime = time.Now()
	}
}

// GetDevice 根据 CallSign 和 SSID 获取设备（公开函数，供 websocket 包使用）
func GetDevice(callsign string, ssid byte) *models.Device {
	return getDevice(callsign, ssid)
}

// GetDeviceByID 根据 DeviceID 获取设备（公开函数，供 websocket 包调用）
// 注意：由于呼号字段不唯一，此方法比 GetDevice 更可靠
func GetDeviceByID(deviceID int) *models.Device {
	// 先从 username 索引查找
	for _, d := range devOwnerSSIDMap {
		if d.ID == deviceID {
			return d
		}
	}

	// 如果没找到，从 callsign 索引查找
	for _, d := range devCallsignSSIDMap {
		if d.ID == deviceID {
			return d
		}
	}

	// ==========================================
	// 【新增修复】: 内存中找不到时，从数据库加载设备
	// 解决 WS 普通设备认证后不在 UDP Hub 内存中的问题
	// ==========================================
	repo := gormdb.NewDeviceRepository()
	gormDev, err := repo.GetDeviceByID(deviceID)
	if err != nil || gormDev == nil {
		return nil
	}

	dev := gormDev.ToModelDevice()
	callsignSSID := protocol.GetCallSignSSID(dev.CallSign, dev.SSID)
	dev.CallSignSSID = callsignSSID

	// 获取所有者信息填充 Username 和 CallSign
	if gormDev.OwnerID > 0 {
		userRepo := gormdb.NewUserRepository()
		if owner, err := userRepo.GetUserByID(gormDev.OwnerID); err == nil && owner != nil {
			dev.Username = owner.Name
			dev.CallSign = owner.CallSign
		}
	}

	// 添加到内存缓存
	indexRuntimeDevice(dev)
	log.Printf("[UDP] Device ID:%d loaded from database into memory cache", deviceID)

	return dev
}

// GetDeviceCount 获取设备总数
func GetDeviceCount() int {
	return len(devOwnerSSIDMap)
}

// GetAllDevices 获取所有设备
func GetAllDevices() map[string]*models.Device {
	return devOwnerSSIDMap
}

// ChangeDeviceGroupByID 通过设备ID更改设备群组（供 API 调用）
func ChangeDeviceGroupByID(deviceID int, newGroupID int) error {
	// 在内存中查找设备
	var dev *models.Device

	// 先从 username 索引查找
	for _, d := range devOwnerSSIDMap {
		if d.ID == deviceID {
			dev = d
			break
		}
	}

	// 如果没找到，从 callsign 索引查找
	if dev == nil {
		for _, d := range devCallsignSSIDMap {
			if d.ID == deviceID {
				dev = d
				break
			}
		}
	}

	if dev == nil {
		return errors.New("device not found in memory")
	}

	// 使用内部的 changeDeviceGroup 函数
	_, err := changeDeviceGroup(dev, newGroupID)
	if err != nil {
		return err
	}

	log.Printf("[GROUP] Device %s (ID: %d) changed to group %d", dev.CallSign, deviceID, newGroupID)
	return nil
}

// SyncDeviceCommControlByID 同步设备禁发/禁收到 UDP Hub 运行时内存。
// 设计目标：设备控制接口更新数据库后，立即在内存转发路径生效，不依赖 10 秒轮询同步。
func SyncDeviceCommControlByID(deviceID int, disableSend, disableRecv bool) {
	seen := make(map[*models.Device]struct{}, 8)
	updated := 0

	apply := func(dev *models.Device) {
		if dev == nil || dev.ID != deviceID {
			return
		}
		if _, ok := seen[dev]; ok {
			return
		}
		seen[dev] = struct{}{}
		dev.DisableSend = disableSend
		dev.DisableRecv = disableRecv
		updated++
	}

	// 1) 两套主索引
	for _, dev := range devOwnerSSIDMap {
		apply(dev)
	}
	for _, dev := range devCallsignSSIDMap {
		apply(dev)
	}

	// 2) 群组缓存中的连接池与设备映射（覆盖所有转发读取路径）
	cache := globalGroupCacheAtomic.Load()
	if cache != nil {
		if groupMap, ok := cache.(map[int]*models.Group); ok {
			for _, gp := range groupMap {
				if gp == nil {
					continue
				}
				for _, dev := range gp.DevMap {
					apply(dev)
				}
				pool := getGroupConnPool(gp)
				if pool == nil {
					continue
				}
				for _, dev := range pool.DevConnMap {
					apply(dev)
				}
				for _, dev := range pool.DevConnList {
					apply(dev)
				}
			}
		}
	}

	if updated > 0 {
		log.Printf("[DEVICE] Device ID %d comm control synced in memory: disable_send=%v disable_recv=%v (refs=%d)", deviceID, disableSend, disableRecv, updated)
	}
}
