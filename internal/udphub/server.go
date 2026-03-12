package udphub

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

var (
	// Global UDP connection
	globalConn *net.UDPConn

	// Device maps
	devCallsignSSIDMap = make(map[string]*models.Device) // key: callsign-ssid
	onlineDevMap        = make(map[int]*models.Device)  // key: device ID
	serverMap           = make(map[string]*models.Device) // key: callsign

	// Group maps
	publicGroupMap = make(map[int]*models.Group) // public groups (0, 999, 1000+)

	// QTH maps
	qthMap    = make(map[string]string)               // callsign-ssid -> qth
	qthMapNew = make(map[string]models.QTH)           // callsign-ssid -> QTH

	// User list (for private groups)
	// TODO: 使用 username 作为 key 而不是 callsign，以支持多设备共享同一用户账户
	userList sync.Map // callsign -> *UserInfo

	// Statistics
	totalStats = &models.TotalStats{}

	// Channel for device logging
	logBuffer = make(chan *models.Device, 100)

	// Limit channel for concurrent packet processing
	limitChan = make(chan bool, 1)
)

// UserInfo 用户信息（用于私有群组）
// TODO: 将 CallSign 字段改为 Username，以支持多设备共享同一用户账户
type UserInfo struct {
	ID      int
	CallSign string
	Name    string
	Roles   []string
	Groups  map[int]*models.Group // 1, 2, 3 - private groups
	DevList map[int]*models.Device
}

// CurrentConnPool 当前连接池
type CurrentConnPool struct {
	UDPAddr       *net.UDPAddr
	LastVoiceTime time.Time
	LastCtlTime   time.Time
	LastPriority  int

	DevConnMap  map[string]*models.Device // key: UDP address string
	DevConnList []*models.Device
}

// StartUDPServer 启动 UDP 服务器
func StartUDPServer(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("resolve UDP address failed: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP failed: %w", err)
	}

	globalConn = conn
	log.Printf("UDP server started on port %d", port)

	// Initialize public groups
	initPublicGroups()

	// Load all devices from database
	loadAllDevices()

	// Start device online checker
	go checkDeviceOnline()

	// Start log processor
	go processLogBuffer()

	// Process packets
	for {
		limitChan <- true
		processUDPConn(conn)
	}
}

// processUDPConn 处理 UDP 连接
func processUDPConn(conn *net.UDPConn) {
	defer func() { <-limitChan }()

	data := make([]byte, 1460)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(data)
		if err != nil {
			log.Printf("[ERROR] Read from UDP failed: %v", err)
			return
		}

		// Panic recovery for packet processing
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] Recovered from panic while processing packet from %v: %v", remoteAddr, r)
				}
			}()

			nrl, err := protocol.NewNRL2Packet(remoteAddr, data[:n])
			if err != nil {
				log.Printf("[DECODE] Error from %v: %v % X", remoteAddr, err, data[:n])
				return
			}

			totalStats.PacketNumber++

			callsignSSID := protocol.GetCallSignSSID(nrl.CallSign, nrl.SSID)

			if dev, ok := devCallsignSSIDMap[callsignSSID]; ok {
				// 设备已存在
				dev.LastPacketTime = nrl.TimeStamp
				dev.Traffic += int64(42 + 48 + len(nrl.DATA))
				totalStats.Traffic += int64(42 + 48 + len(nrl.DATA))

				// 更新 DMRID（非 200 设备）
				if nrl.DevModel != models.DevModelServer {
					protocol.SetDevDMRID(dev.DMRID, data[:n])
				}

				// 根据群组类型处理
				if dev.GroupID > 0 && dev.GroupID <= 3 {
					// 私有群组
					if u, ok := userList.Load(dev.CallSign); ok {
						if gp, ok := u.(*UserInfo).Groups[dev.GroupID]; ok {
							parseNRL2(nrl, data[:n], dev, conn, gp)
						}
					}
				} else if dev.GroupID >= models.GroupIDPublicMin || dev.GroupID == 0 {
					// 公共群组
					if gp, ok := publicGroupMap[dev.GroupID]; ok {
						parseNRL2(nrl, data[:n], dev, conn, gp)
					}
				}
			} else {
			// 新设备，添加到默认群组
			newDevice := &models.Device{
				CallSign:    nrl.CallSign,
				SSID:        nrl.SSID,
				CallSignSSID: callsignSSID,
				DevModel:    nrl.DevModel,
				Priority:    100,
				Status:      0,
				ChanName:    make([]string, 8),
				PcmBuffer:   make([]int, 160),
			}

			// 255 设备加入 999 全网通群组
			if nrl.DevModel == models.DevModelFullNet && nrl.SSID == 255 {
				newDevice.GroupID = models.GroupIDPublicMin
			}

			dev, err := addDevice(newDevice)
			if err != nil {
				log.Printf("Add device failed: %v, %v", err, nrl)
				return
			}

			if dev != nil {
				dev.PcmG711Chan = make(chan [][]byte, 3)
				dev.PcmBuffer = make([]int, 160)
				devCallsignSSIDMap[callsignSSID] = dev

				// 加入群组
				if gp, ok := publicGroupMap[dev.GroupID]; ok {
					gp.DevMap[dev.ID] = dev
					parseNRL2(nrl, data[:n], dev, conn, gp)
				} else {
					// 加入默��群组
					if gp0, ok := publicGroupMap[0]; ok {
						gp0.DevMap[dev.ID] = dev
						parseNRL2(nrl, data[:n], dev, conn, gp0)
					}
				}
			}
		}
		}()
	}
}

// parseNRL2 解析并处理 NRL2 报文
func parseNRL2(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	switch nrl.Type {
	case models.TypeControl:
		// 控制指令
		log.Printf("Received control command: %v", nrl)

	case models.TypeG711Voice, models.TypeOpus16K:
		// 语音消息
		handleVoice(nrl, packet, dev, conn, gp)

	case models.TypeHeartbeat:
		// 心跳包
		handleHeartbeat(nrl, packet, dev, conn, gp)

	case models.TypeConfig:
		// 设备配置
		handleConfig(nrl, dev)

	case models.TypeTextMessage:
		// 文本消息
		handleTextMessage(nrl, packet, dev, conn, gp)

	case models.TypeDeviceControl:
		// 设备控制
		handleDeviceControl(nrl, packet, dev, conn, gp)

	case models.TypeGroupCommand:
		// 组加入指令
		handleGroupCommand(nrl, packet, dev, conn)

	case models.TypeServerVoice:
		// 服务器互联语音
		handleServerVoice(nrl, packet, dev, conn, gp)

	case models.TypeATPassThrough:
		// AT 透传
		handleATCommand(nrl, dev)

	default:
		log.Printf("Unknown packet type: %d, %v", nrl.Type, nrl)
	}
}

// handleVoice 处理语音消息
func handleVoice(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备状态（是否禁发）
	if (dev.Status & models.DevStatusTxDisable) == models.DevStatusTxDisable {
		return
	}

	// 记录语音开始时间
	td := nrl.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()
	if td > 200 {
		dev.LastVoiceBeginTime = nrl.TimeStamp
		logBuffer <- dev
		dev.Loged = true
	}
	dev.Loged = false

	dev.LastVoiceDuration = int(nrl.TimeStamp.Sub(dev.LastVoiceBeginTime).Milliseconds())
	dev.LastVoiceEndTime = nrl.TimeStamp

	dev.VoiceTime += 63
	totalStats.VoiceTime += 63

	dev.LastVoiceEndTime = nrl.TimeStamp
	dev.LastCtlEndTime = nrl.TimeStamp

	// 来自 255 设备的包
	if nrl.DevModel == models.DevModelFullNet && nrl.SSID == 255 {
		dev.ISOnline = true
		forwardServerVoice(nrl, dev, packet, conn, gp)
		return
	}

	// 来自 200 设备的包
	if nrl.DevModel == models.DevModelServer && nrl.SSID == models.DevModelServer {
		forwardServerVoice(nrl, dev, packet, conn, gp)
		return
	}

	// 普通设备语音转发
	forwardVoice(nrl, dev, packet, gp)
}

// handleHeartbeat 处理心跳包
func handleHeartbeat(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 处理服务器转发的定制心跳（不响应）
	if len(packet) == 52 {
		dev.QTH = getQTH(net.IP(packet[48:]).String())
		log.Printf("[FORWARD] Device online: %v %v %v-%v %v", nrl.UDPAddrStr, net.IP(packet[48:]).String(), dev.CallSign, dev.SSID, dev.QTH)
		return
	}

	wasOnline := dev.ISOnline
	currentAddr := nrl.UDPAddr.String()
	addrChanged := dev.UDPAddr != nil && dev.UDPAddr.String() != currentAddr

	// 更新设备地址和时间
	dev.UDPAddr = nrl.UDPAddr
	dev.LastPacketTime = nrl.TimeStamp

	// 检测重连
	if addrChanged && wasOnline {
		log.Printf("[RECONNECT] Device %v-%v reconnected from %v to %v",
			dev.CallSign, dev.SSID, dev.PreviousUDPAddr, currentAddr)
		dev.ReconnectCount++
		dev.PreviousUDPAddr = currentAddr
		dev.IsReconnecting = true
	} else if !wasOnline && !dev.LastDisconnectTime.IsZero() {
		// 从离线恢复
		timeOffline := nrl.TimeStamp.Sub(dev.LastDisconnectTime)
		log.Printf("[RECOVER] Device %v-%v back online after %v (addr: %v, reconnect count: %d)",
			dev.CallSign, dev.SSID, timeOffline, currentAddr, dev.ReconnectCount)
		dev.IsReconnecting = false
	}

	// 记录日志
	if !dev.Loged && nrl.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds() > 200 {
		logBuffer <- dev
		dev.Loged = true
	}

	// 判断设备是否已加入连接池
	pool := gp.ConnPool.(*CurrentConnPool)
	if _, exists := pool.DevConnMap[currentAddr]; !exists {
		pool.DevConnMap[currentAddr] = dev
		pool.DevConnList = append(pool.DevConnList, dev)

		// 200 设备保存到 serverMap
		if nrl.SSID == models.SSIDServerMin {
			serverMap[nrl.CallSign] = dev
		}
	}

	// 如果不是主动发出心跳的设备，需要回复
	if dev.UDPSocket == nil {
		if dev.DeviceParm == nil && dev.DevModel < 100 {
			// 发送查询设备参数
			conn.WriteToUDP(protocol.Encode(dev.CallSign, dev.SSID, models.TypeConfig, 0, 0, []byte{0x01}), dev.UDPAddr)
		} else {
			conn.WriteToUDP(packet, nrl.UDPAddr)
		}
	}

	if !dev.ISOnline {
		// 新设备上线
		// 更新设备型号
		if nrl.DevModel != 0 {
			dev.DevModel = nrl.DevModel
		}

		// 查询设备 QTH 信息
		dev.QTH = getQTH(dev.UDPAddr.IP.String())
		qthMap[dev.CallSignSSID] = dev.QTH
		qthMapNew[dev.CallSignSSID] = models.QTH{
			QTH:          dev.QTH,
			CallSignSSID: dev.CallSignSSID,
			JoinTime:     time.Now(),
			Name:         dev.Name,
		}

		log.Printf("[ONLINE] Device %v-%v online from %v, QTH: %v, group: %d, model: %d",
			dev.CallSign, dev.SSID, dev.UDPAddr.String(), dev.QTH, gp.ID, dev.DevModel)

		if dev.DevModel != models.DevModelServer {
			// 发送 AT 查询
			at := &models.ATCommand{CallSign: dev.CallSign, SSID: dev.SSID, Type: 0x01, ATcommand: "AT+READ", Data: "123"}
			deviceAT(at)
		}

		dev.ISOnline = true
	}
}

// handleConfig 处理设备配置
func handleConfig(nrl *models.NRL2Packet, dev *models.Device) {
	// 解析设备配置参数
	dev.DeviceParm = decodeControlPacket(nrl.DATA)
}

// handleTextMessage 处理文本消息
func handleTextMessage(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	forwardMessage(nrl, packet, dev, conn, gp.ConnPool.(*CurrentConnPool))
}

// handleDeviceControl 处理设备控制
func handleDeviceControl(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	if (dev.Status & models.DevStatusTxDisable) == models.DevStatusTxDisable {
		return
	}

	if nrl.TimeStamp.Sub(dev.LastCtlEndTime).Milliseconds() > 200 {
		dev.LastCtlBeginTime = nrl.TimeStamp
	}
	dev.LastCtlDuration = int(nrl.TimeStamp.Sub(dev.LastCtlBeginTime).Milliseconds())
	dev.LastCtlEndTime = nrl.TimeStamp

	dev.CtlTime += 63

	forwardControl(nrl, packet, conn, gp)
}

// handleGroupCommand 处理组加入指令
func handleGroupCommand(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn) {
	// 边界检查：数据包至少需要 53 字节才能读取 cmdType 和 groupID
	if len(packet) < 53 {
		log.Printf("Device %v-%v group command packet too short: %d bytes", dev.CallSign, dev.SSID, len(packet))
		return
	}

	cmdType := packet[48]

	switch cmdType {
	case 1: // 切换组指令
		groupID := int(binary.BigEndian.Uint32(packet[49:53]))
		log.Printf("Device %v-%v change group from %v to %v, data: % X", dev.CallSign, dev.SSID, dev.GroupID, groupID, packet)

		str, err := changeDeviceGroup(dev, groupID)
		if err != nil {
			log.Println("Change group error:", err)
			conn.WriteToUDP(append(packet, []byte(strconv.Itoa(groupID)+",error")...), nrl.UDPAddr)
		} else {
			conn.WriteToUDP(append(packet, str...), nrl.UDPAddr)
		}

	case 2: // 获取组列表
		resp := getGroupListForDevice(packet)
		conn.WriteToUDP(resp, nrl.UDPAddr)
		log.Printf("Device %v-%v download group list, size: %v", dev.CallSign, dev.SSID, len(resp))
	}
}

// handleServerVoice 处理服务器互联语音
func handleServerVoice(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	if (dev.Status & models.DevStatusTxDisable) == models.DevStatusTxDisable {
		return
	}

	td := nrl.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()
	if td > 200 {
		dev.LastVoiceBeginTime = nrl.TimeStamp
		logBuffer <- dev
		dev.Loged = true
	}
	dev.Loged = false

	dev.LastVoiceDuration = int(nrl.TimeStamp.Sub(dev.LastVoiceBeginTime).Milliseconds())
	dev.LastVoiceEndTime = nrl.TimeStamp

	dev.VoiceTime += 20
	totalStats.VoiceTime += 20

	dev.LastVoiceEndTime = nrl.TimeStamp
	dev.LastCtlEndTime = nrl.TimeStamp

	forwardServerVoice(nrl, dev, packet, conn, gp)
}

// handleATCommand 处理 AT 命令
func handleATCommand(nrl *models.NRL2Packet, dev *models.Device) {
	at := decodeATPacket(dev.CallSign, dev.SSID, nrl.DATA)
	dev.LastATcommand = at
}

// GetTotalStats 获取统计信息
func GetTotalStats() *models.TotalStats {
	return totalStats
}

// GetOnlineDeviceCount 获取在线设备数量
func GetOnlineDeviceCount() int {
	return totalStats.OnlineDevNumber
}

// GetQTHMap 获取 QTH 映射
func GetQTHMap() map[string]string {
	return qthMap
}

// GetQTHMapNew 获取新 QTH 映射
func GetQTHMapNew() map[string]models.QTH {
	return qthMapNew
}

// GetDeviceByCallsignSSID 通过呼号-SSID 获取设备
func GetDeviceByCallsignSSID(callsignSSID string) (*models.Device, bool) {
	dev, ok := devCallsignSSIDMap[callsignSSID]
	return dev, ok
}

// AddUser 添加用户到用户列表
// TODO: 将 callsign 认证改为 username，以支持多设备共享同一用户账户
func AddUser(userInfo *UserInfo) {
	userList.Store(userInfo.CallSign, userInfo)
}

// GetUser 获取用户信息
// TODO: 将 callsign 认证改为 username，以支持多设备共享同一用户账户
func GetUser(callsign string) (*UserInfo, bool) {
	if u, ok := userList.Load(callsign); ok {
		return u.(*UserInfo), true
	}
	return nil, false
}
