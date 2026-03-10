package udphub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"nrllink/internal/db"
	"nrllink/internal/models"
	"nrllink/internal/protocol"
	"nrllink/pkg/geoip"
)

// getGroupConnPool 获取群组连接池
func getGroupConnPool(gp *models.Group) *CurrentConnPool {
	if gp.ConnPool == nil {
		return nil
	}
	return gp.ConnPool.(*CurrentConnPool)
}

// loadAllDevices 从数据库加载所有设备
func loadAllDevices() {
	repo := db.NewDeviceRepository()
	devices, _, err := repo.ListDevices(10000, 1)
	if err != nil {
		log.Printf("Load devices from database failed: %v", err)
		return
	}

	for _, dev := range devices {
		dev.PcmG711Chan = make(chan [][]byte, 3)
		dev.PcmBuffer = make([]int, 160)

		callsignSSID := protocol.GetCallSignSSID(dev.CallSign, dev.SSID)
		dev.CallSignSSID = callsignSSID

		// 255 设备加入 999 群组
		if dev.SSID == models.SSIDServerMax || dev.DevModel == models.DevModelFullNet {
			dev.GroupID = models.GroupIDPublicMin
		}

		devCallsignSSIDMap[callsignSSID] = dev

		// 加入群组
		if gp, ok := publicGroupMap[dev.GroupID]; ok {
			gp.DevMap[dev.ID] = dev
			gp.DevList = append(gp.DevList, dev.ID)
		} else {
			// 如果群组不存在，加入默认群组
			if gp0, ok := publicGroupMap[0]; ok {
				gp0.DevMap[dev.ID] = dev
				gp0.DevList = append(gp0.DevList, dev.ID)
			}
		}
	}

	log.Printf("Loaded %d devices from database", len(devices))
}

// addDevice 添加新设备（如果已存在则从数据库加载）
func addDevice(dev *models.Device) (*models.Device, error) {
	repo := db.NewDeviceRepository()

	// 检查设备是否已存在
	existingDev, err := repo.GetDevice(dev.CallSign, dev.SSID)
	if err == nil {
		// 设备已存在，直接返回现有设备
		log.Printf("Device %s-%d already exists in database (ID: %d), reusing", dev.CallSign, dev.SSID, existingDev.ID)
		return existingDev, nil
	}

	// 设备不存在，创建新设备
	dev.CreateTime = time.Now()
	dev.UpdateTime = time.Now()

	if err := repo.AddDevice(dev); err != nil {
		return nil, fmt.Errorf("add device to database failed: %w", err)
	}

	log.Printf("Created new device in database: %s-%d (ID: %d)", dev.CallSign, dev.SSID, dev.ID)
	return dev, nil
}

// getDevice 获取设备
func getDevice(callsign string, ssid byte) *models.Device {
	callsignSSID := protocol.GetCallSignSSID(callsign, ssid)
	if dev, ok := devCallsignSSIDMap[callsignSSID]; ok {
		return dev
	}

	// 从数据库查询
	repo := db.NewDeviceRepository()
	dev, err := repo.GetDevice(callsign, ssid)
	if err != nil {
		return nil
	}

	dev.CallSignSSID = callsignSSID
	dev.PcmG711Chan = make(chan [][]byte, 3)
	dev.PcmBuffer = make([]int, 160)

	devCallsignSSIDMap[callsignSSID] = dev
	return dev
}

// getDeviceByDMRID 通过 DMRID 获取设备
func getDeviceByDMRID(dmrid uint32) *models.Device {
	repo := db.NewDeviceRepository()
	dev, err := repo.GetDeviceByDMRID(dmrid)
	if err != nil {
		return nil
	}

	callsignSSID := protocol.GetCallSignSSID(dev.CallSign, dev.SSID)
	dev.CallSignSSID = callsignSSID

	if _, ok := devCallsignSSIDMap[callsignSSID]; !ok {
		dev.PcmG711Chan = make(chan [][]byte, 3)
		dev.PcmBuffer = make([]int, 160)
		devCallsignSSIDMap[callsignSSID] = dev
	}

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

				list := make([]*models.Device, 0)
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

					list := make([]*models.Device, 0)
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
	time.Sleep(10 * time.Second)

	// 配置参数
	const (
		checkInterval    = 5 * time.Second // 检查间隔
		offlineTimeout   = 20 * time.Second // 离线超时
		reconnectGrace   = 10 * time.Second // 重连宽限期
		maxRetryLog      = 3               // 最大重复日志次数
	)

	for {
		time.Sleep(checkInterval)

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
							log.Printf("[OFFLINE] Device %v-%v group %v timed out after %v (addr: %v)",
								dev.CallSign, dev.SSID, dev.GroupID, timeSinceLastPacket, dev.UDPAddr)

							dev.LastDisconnectTime = t
							dev.ISOnline = false
							dev.ReconnectCount++

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

			gp.TotalDevNumber = len(gp.DevMap)
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

				gp.TotalDevNumber = len(gp.DevMap)
				totalStats.OnlineDevNumber += gp.OnlineDevNumber
			}
			return true
		})

		onlineDevMap = onlineMap
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
	callsignSSID := protocol.GetCallSignSSID(at.CallSign, at.SSID)
	dev, ok := devCallsignSSIDMap[callsignSSID]
	if !ok {
		return nil, errors.New("device not found")
	}

	atCommand := append([]byte{at.Type}, []byte(at.ATcommand+"="+at.Data+"\r\n")...)
	packet := protocol.Encode(at.CallSign, at.SSID, models.TypeATPassThrough, models.DevModelServer, dev.DMRID, atCommand)

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
		globalConn.WriteToUDP(protocol.Encode(dev.CallSign, dev.SSID, models.TypeConfig, 0, 0, []byte{0x01}), dev.UDPAddr)
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
			if strings.HasPrefix(line, "NRL") {
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

// GetDeviceCount 获取设备总数
func GetDeviceCount() int {
	return len(devCallsignSSIDMap)
}

// GetAllDevices 获取所有设备
func GetAllDevices() map[string]*models.Device {
	return devCallsignSSIDMap
}
