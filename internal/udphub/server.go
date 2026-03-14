package udphub

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"nrllink/internal/gormdb"
	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

// 全局变量声明
var (
	// 全局 UDP 连接
	globalConn *net.UDPConn

	// Username 索引的设备映射 (DraARLv1)
	devUsernameSSIDMap = make(map[string]*models.Device) // key: username-ssid

	// CallSign 索引的设���映射 (向后兼容)
	devCallsignSSIDMap = make(map[string]*models.Device) // key: callsign-ssid

	// 在线设备映射
	onlineDevMap    = make(map[int]*models.Device) // key: device ID
	onlineDevMapDraARL = make(map[int]*models.Device) // key: device ID (DraARLv1)

	// 已认证设备缓存 (username -> auth result)
	authedUserCache = make(map[string]*DeviceAuthResult)
	authCacheMutex  sync.RWMutex

	// 服务器映射
	serverMap = make(map[int]*models.Server)

	// 公共群组映射 (保留用于向后兼容)
	publicGroupMap = make(map[int]*models.Group)

	// ==========================================
	// 架构重构：全局统一群组缓存
	// 替代原有的 publicGroupMap 和 userList 的群组路由功能
	// ==========================================
	globalGroupCache = make(map[int]*models.Group)
	groupCacheMutex  sync.RWMutex

	// QTH 映射
	qthMap    = make(map[string]string)
	qthMapNew = make(map[string]models.QTH)

	// 用户列表 (sync.Map)
	userList sync.Map

	// 统计信息
	totalStats = &models.ServerStats{}

	// 日志缓冲
	logBuffer = make(chan *models.Device, 1000)

	// 并发限制
	limitChan = make(chan bool, 100)
)

// UserInfo 用户信息结构
type UserInfo struct {
	ID       int
	CallSign string
	Name     string
	Groups   map[int]*models.Group
}

// CurrentConnPool 当前连接池
type CurrentConnPool struct {
	DevConnMap   map[string]*models.Device // key: UDPAddr.String()
	DevConnList  []*models.Device
	UDPAddr      *net.UDPAddr
	LastVoiceTime time.Time
	LastPriority int
}

// StartUDPServer 启动 UDP 服务器（DraARLv1 协议）
func StartUDPServer(port int) error {
	return StartDraARLServer(port)
}

// StartDraARLServer 启动 DraARLv1 协议的 UDP 服务器
func StartDraARLServer(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("resolve UDP address failed: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP failed: %w", err)
	}

	globalConn = conn
	log.Printf("DraARLv1 UDP server started on port %d", port)

	// 启动认证失败记录清理器
	StartAuthCleaner()

	// 初始化公共群组
	initPublicGroups()

	// ==========================================
	// 架构重构：启动全局群组缓存定时同步
	// ==========================================
	StartGroupCacheSync()

	// 加载所有设备
	loadAllDevices()

	// 启动设备在线检查
	go checkDeviceOnline()

	// 启动日志处理器
	go processLogBuffer()

	// 处理数据包
	for {
		limitChan <- true
		processDraARLConn(conn)
	}
}

// processDraARLConn 处理 DraARLv1 UDP 连接
func processDraARLConn(conn *net.UDPConn) {
	defer func() { <-limitChan }()

	data := make([]byte, 1460)

	for {
		n, remoteAddr, err := conn.ReadFromUDP(data)
		if err != nil {
			log.Printf("[ERROR] Read from UDP failed: %v", err)
			return
		}

		// Panic recovery
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] Recovered from panic while processing packet from %v: %v", remoteAddr, r)
				}
			}()

			// 处理 DraARLv1 数据包
			if n >= 4 && string(data[0:4]) == "DraA" {
				processDraARLPacket(data[:n], remoteAddr, conn)
			} else {
				log.Printf("[DECODE] Unknown protocol from %v: %s", remoteAddr, string(data[:min(4, n)]))
			}
		}()
	}
}

// processDraARLPacket 处理 DraARLv1 数据包
func processDraARLPacket(data []byte, remoteAddr *net.UDPAddr, conn *net.UDPConn) {
	packet, err := protocol.NewDraARLv1Packet(remoteAddr, data)
	if err != nil {
		log.Printf("[DECODE] DraARLv1 decode error from %v: %v", remoteAddr, err)
		return
	}

	totalStats.PacketNumber++
	usernameSSID := protocol.GetUsernameSSID(packet.Username, packet.SSID)

	// 查找已存在的设备
	dev, exists := devUsernameSSIDMap[usernameSSID]
	if !exists {
		// 新设备，需要先认证
		handleNewDraARLDevice(packet, data, conn, usernameSSID)
		return
	}

	// ==========================================
	// 修复1：即使设备已在内存中(如从数据库加载)，
	// 当它发送心跳包上线或更换 IP 端口时，依然需要执行密码鉴权
	// ==========================================
	if packet.Type == protocol.DraARLTypeHeartbeat {
		currentAddr := packet.UDPAddr.String()
		// 只有当设备原本处于离线状态，或者 IP 地址发生变化时才触发鉴权，节省性能
		if !dev.ISOnline || (dev.UDPAddr != nil && dev.UDPAddr.String() != currentAddr) {
			authResult := AuthenticateDevice(packet.UDPAddr.IP.String(), packet.Username, packet.DevicePassword)
			if !authResult.Success {
				log.Printf("[AUTH] Device re-authentication failed: %s, error: %s", usernameSSID, authResult.Error)
				return // 密码错误，直接丢弃该数据包
			}
			// 鉴权成功后，补全由于直接从 DB 加载可能缺失的呼号字段
			dev.CallSign = authResult.CallSign
			log.Printf("[AUTH] Device re-authenticated: %s (%s) from %v", usernameSSID, dev.CallSign, currentAddr)
		}
	}

	// 已存在的设备，更新状态
	dev.LastPacketTime = packet.TimeStamp
	dev.Traffic += int64(protocol.DraARLv1HeaderSize + len(packet.DATA))
	totalStats.Traffic += int64(protocol.DraARLv1HeaderSize + len(packet.DATA))

	// ==========================================
	// 修复2：修正 GroupID 为 0 时导致数据包被静默丢弃的问题
	// ==========================================
	targetGroupID := dev.GroupID
	if targetGroupID == 0 {
		targetGroupID = models.GroupIDPublicMin // 如果读出为 0，映射到默认的公共群组(999)
		dev.GroupID = targetGroupID             // 同步修正设备内存状态
	}

	// ==========================================
	// 架构重构：使用纯粹的全局缓存进行路由分发
	// 不再区分"私有群组"和"公共群组"，统一从数据库加载的群组缓存中查找
	// ==========================================
	gp, exists := GetGroupFromCache(targetGroupID)
	if exists {
		parseDraARL(packet, data, dev, conn, gp)
	} else {
		// 找不到对应的群组实例
		// 可能是数据库中删除了该群组，或者设备被分配了一个错误的群组 ID
		if packet.Type != protocol.DraARLTypeHeartbeat {
			log.Printf("[ROUTING] 路由丢弃: 设备 %s 请求的群组 ID: %d 不存在或已停用", dev.Username, targetGroupID)
		}
	}
}

// handleNewDraARLDevice 处理新 DraARLv1 设备
func handleNewDraARLDevice(packet *protocol.DraARLv1Packet, data []byte, conn *net.UDPConn, usernameSSID string) {
	// 心跳包需要进行认证
	if packet.Type != protocol.DraARLTypeHeartbeat {
		// 非心跳包，忽略未认证设备
		log.Printf("[AUTH] Ignoring packet from unauthenticated device: %s, type: %d", usernameSSID, packet.Type)
		return
	}

	// 认证设备
	authResult := AuthenticateDevice(packet.UDPAddr.IP.String(), packet.Username, packet.DevicePassword)
	if !authResult.Success {
		// 认证失败，不创建设备
		log.Printf("[AUTH] Device authentication failed: %s, error: %s", usernameSSID, authResult.Error)
		return
	}

	// 认证成功，创建或更新设备
	newDevice := &models.Device{
		Username:     packet.Username,
		CallSign:     authResult.CallSign,
		SSID:         packet.SSID,
		// 使用 fmt.Sprintf 安全地将数字 byte 转换为字符串拼接到呼号后
		CallSignSSID: fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID),
		DevModel:     packet.DevModel,
		Priority:     100,
		Status:       0,
		ChanName:     make([]string, 8),
		PcmBuffer:    make([]int, 160),
		GroupID:      models.GroupIDPublicMin, // 默认加入公共群组
	}

	// 保存设备到数据库
	dev, err := addDevice(newDevice)
	if err != nil {
		log.Printf("[DEVICE] Add device failed: %v, %v", err, packet.Username)
		return
	}

	if dev != nil {
		dev.PcmG711Chan = make(chan [][]byte, 3)
		dev.PcmBuffer = make([]int, 160)
		dev.UDPAddr = packet.UDPAddr
		dev.ISOnline = true
		dev.LastPacketTime = packet.TimeStamp
		devUsernameSSIDMap[usernameSSID] = dev

		// 同时更新 callsign 索引（向后兼容）
		callsignSSID := protocol.GetCallSignSSID(dev.CallSign, dev.SSID)
		devCallsignSSIDMap[callsignSSID] = dev

		// 加入群组
		if gp, ok := publicGroupMap[dev.GroupID]; ok {
			gp.DevMap[dev.ID] = dev

			// 加入连接池
			pool := gp.ConnPool.(*CurrentConnPool)
			if pool.DevConnMap == nil {
				pool.DevConnMap = make(map[string]*models.Device)
			}
			pool.DevConnMap[packet.UDPAddr.String()] = dev
			pool.DevConnList = append(pool.DevConnList, dev)

			// 发送心跳响应（填充 CallSign）
			response := protocol.EncodeHeartbeatResponse(packet, authResult.CallSign)
			conn.WriteToUDP(response, packet.UDPAddr)
			log.Printf("[ONLINE] New DraARLv1 device online: %s (%s) from %v, group: %d",
				packet.Username, authResult.CallSign, packet.UDPAddr, dev.GroupID)
		}
	}
}

// parseDraARL 解析并处理 DraARLv1 报文
func parseDraARL(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	switch packet.Type {
	case protocol.DraARLTypeControl:
		// 控制指令
		log.Printf("Received DraARLv1 control command: %v", packet)

	case protocol.DraARLTypeG711Voice, protocol.DraARLTypeOpus16K:
		// 语音消息
		handleDraARLVoice(packet, data, dev, conn, gp)

	case protocol.DraARLTypeHeartbeat:
		// 心跳包
		handleDraARLHeartbeat(packet, data, dev, conn, gp)

	case protocol.DraARLTypeConfig:
		// 设备配置
		handleDraARLConfig(packet, dev)

	case protocol.DraARLTypeTextMessage:
		// 文本消息
		handleDraARLTextMessage(packet, data, dev, conn, gp)

	case protocol.DraARLTypeServerVoice:
		// 服务器互联语音
		handleDraARLServerVoice(packet, data, dev, conn, gp)

	case protocol.DraARLTypeATPassThrough:
		// AT 透传
		handleDraARLATCommand(packet, dev)

	default:
		log.Printf("Unknown DraARLv1 packet type: %d, %v", packet.Type, packet)
	}
}

// handleDraARLVoice 处理 DraARLv1 ���音消息
func handleDraARLVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if dev.DisableSend {
		return
	}

	// 计算距离上次收到语音包的时间间隔
	td := packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()

	// td > 200 表示距离上次语音已经超过 200ms，说明这是一次"新"的按键发言(PTT)
	// 此时仅记录起始时间，推迟到心跳包机制检测到语音彻底结束时，再投递最终包含时长的日志
	if td > 200 {
		dev.LastVoiceBeginTime = packet.TimeStamp
		// 将标记位置为 false，交由 handleDraARLHeartbeat 在松开 PTT 时接管日志生成
		dev.Loged = false
	}

	// 实时更新本次发言的累计持续时间
	dev.LastVoiceDuration = int(packet.TimeStamp.Sub(dev.LastVoiceBeginTime).Milliseconds())
	dev.LastVoiceEndTime = packet.TimeStamp

	dev.VoiceTime += 63
	totalStats.VoiceTime += 63

	dev.LastCtlEndTime = packet.TimeStamp

	// 普通设备语��转发
	forwardDraARLVoice(packet, dev, data, gp)
}

// handleDraARLHeartbeat 处理 DraARLv1 心跳包
func handleDraARLHeartbeat(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	wasOnline := dev.ISOnline
	currentAddr := packet.UDPAddr.String()
	addrChanged := dev.UDPAddr != nil && dev.UDPAddr.String() != currentAddr

	// 更新设备地址和时间
	dev.UDPAddr = packet.UDPAddr
	dev.LastPacketTime = packet.TimeStamp

	// 检测重连
	if addrChanged && wasOnline {
		log.Printf("[RECONNECT] DraARLv1 device %s-%d reconnected from %v to %v",
			dev.Username, dev.SSID, dev.PreviousUDPAddr, currentAddr)
		dev.ReconnectCount++
		dev.PreviousUDPAddr = currentAddr
		dev.IsReconnecting = true
	} else if !wasOnline && !dev.LastDisconnectTime.IsZero() {
		timeOffline := packet.TimeStamp.Sub(dev.LastDisconnectTime)
		log.Printf("[RECOVER] DraARLv1 device %s-%d back online after %v",
			dev.Username, dev.SSID, timeOffline)
		dev.IsReconnecting = false
	}

	// 记录日志
	if !dev.Loged && packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds() > 200 {
		logBuffer <- dev
		dev.Loged = true
	}

	// 加入连接池
	pool := gp.ConnPool.(*CurrentConnPool)
	if _, exists := pool.DevConnMap[currentAddr]; !exists {
		pool.DevConnMap[currentAddr] = dev
		pool.DevConnList = append(pool.DevConnList, dev)
	}

	// 发送心跳响应（填充 CallSign）
	response := protocol.EncodeHeartbeatResponse(packet, dev.CallSign)
	conn.WriteToUDP(response, packet.UDPAddr)

	if !dev.ISOnline {
		// 新设备上线
		if packet.DevModel != 0 {
			dev.DevModel = packet.DevModel
		}

		dev.QTH = getQTH(dev.UDPAddr.IP.String())
		log.Printf("[ONLINE] DraARLv1 device %s (%s) online from %v, QTH: %v, group: %d, model: %d",
			dev.Username, dev.CallSign, dev.UDPAddr.String(), dev.QTH, gp.ID, dev.DevModel)

		dev.ISOnline = true
	}
}

// handleDraARLConfig 处理 DraARLv1 设备配置
func handleDraARLConfig(packet *protocol.DraARLv1Packet, dev *models.Device) {
	dev.DeviceParm = decodeControlPacket(packet.DATA)
}

// handleDraARLTextMessage 处理 DraARLv1 文本消息
func handleDraARLTextMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	forwardDraARLMessage(packet, data, dev, conn, gp.ConnPool.(*CurrentConnPool), gp)
}

// handleDraARLServerVoice 处理 DraARLv1 服务器互联语音
func handleDraARLServerVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if dev.DisableSend {
		return
	}

	td := packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()
	if td > 200 {
		dev.LastVoiceBeginTime = packet.TimeStamp
		logBuffer <- dev
		dev.Loged = true
	}
	dev.Loged = false

	dev.LastVoiceDuration = int(packet.TimeStamp.Sub(dev.LastVoiceBeginTime).Milliseconds())
	dev.LastVoiceEndTime = packet.TimeStamp

	dev.VoiceTime += 20
	totalStats.VoiceTime += 20

	dev.LastCtlEndTime = packet.TimeStamp

	forwardDraARLServerVoice(packet, dev, data, conn, gp)
}

// handleDraARLATCommand 处理 DraARLv1 AT 命令
func handleDraARLATCommand(packet *protocol.DraARLv1Packet, dev *models.Device) {
	at := decodeATPacket(dev.CallSign, dev.SSID, packet.DATA)
	dev.LastATcommand = at
}

// forwardDraARLVoice 转发 DraARLv1 语音
func forwardDraARLVoice(packet *protocol.DraARLv1Packet, dev *models.Device, data []byte, gp *models.Group) {
	pool := gp.ConnPool.(*CurrentConnPool)

	for _, targetDev := range pool.DevConnList {
		if targetDev.ID == dev.ID {
			continue // 不转发给自己
		}

		// ==========================================
		// 终极防御：懒剔除拦截
		// 检查池子里的这个设备，它当前的真实 GroupID 还是不是本群组？
		// 如果不是，说明它是被移出的"幽灵设备"，直接跳过！
		// ==========================================
		if targetDev.GroupID != gp.ID {
			continue
		}

		// 检查目标设备是否禁收
		if targetDev.DisableRecv {
			continue
		}

		if targetDev.UDPAddr != nil && targetDev.ISOnline {
			// 转发时保留原始发送方信息
			globalConn.WriteToUDP(data, targetDev.UDPAddr)
		}
	}
}

// forwardDraARLMessage 转发 DraARLv1 文本消息
func forwardDraARLMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, pool *CurrentConnPool, gp *models.Group) {
	for _, targetDev := range pool.DevConnList {
		if targetDev.ID == dev.ID {
			continue
		}

		// 懒剔除拦截：检查目标设备是否还属于本群组
		if targetDev.GroupID != gp.ID {
			continue
		}

		// 检查目标设备是否禁收
		if targetDev.DisableRecv {
			continue
		}

		if targetDev.UDPAddr != nil && targetDev.ISOnline {
			globalConn.WriteToUDP(data, targetDev.UDPAddr)
		}
	}
}

// forwardDraARLServerVoice 转发 DraARLv1 服务器互联语音
func forwardDraARLServerVoice(packet *protocol.DraARLv1Packet, dev *models.Device, data []byte, conn *net.UDPConn, gp *models.Group) {
	pool := gp.ConnPool.(*CurrentConnPool)

	for _, targetDev := range pool.DevConnList {
		if targetDev.ID == dev.ID {
			continue
		}

		// 懒剔除拦截：检查目标设备是否还属于本群组
		if targetDev.GroupID != gp.ID {
			continue
		}

		// 检查目标设备是否禁收
		if targetDev.DisableRecv {
			continue
		}

		if targetDev.UDPAddr != nil && targetDev.ISOnline {
			globalConn.WriteToUDP(data, targetDev.UDPAddr)
		}
	}
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetGlobalConn 获取全局 UDP 连接
func GetGlobalConn() *net.UDPConn {
	return globalConn
}

// GetTotalStats 获取服务器统计信息
func GetTotalStats() *models.ServerStats {
	return totalStats
}

// GetUserList 获取用户列表
func GetUserList() *sync.Map {
	return &userList
}

// GetPublicGroupMap 获取公共群组映射
func GetPublicGroupMap() map[int]*models.Group {
	return publicGroupMap
}

// ==========================================
// 架构重构：全局群组缓存管理
// ==========================================

// StartGroupCacheSync 启动群组和设备缓存定时同步后台任务
func StartGroupCacheSync() {
	// 启动时立即执行一次，确保服务器刚启动就有数据
	refreshGroupCache()
	refreshDeviceCache()

	go func() {
		// 每隔 10 秒同步一次数据库中的群组和设备状态
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			<-ticker.C
			refreshGroupCache()
			refreshDeviceCache()
		}
	}()
	log.Println("[CACHE] 数据库群组和设备定时同步任务已启动 (间隔: 10s)")
}

// refreshGroupCache 执行具体的数据库查询与内存缓存增量合并更新
// 核心原则：只更新静态配置属性，绝对不碰动态连接池(ConnPool)
func refreshGroupCache() {
	repo := gormdb.NewGroupRepository()
	dbGroups, err := repo.ListGroups()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载群组失败: %v", err)
		return
	}

	// 加写锁，安全更新内存
	groupCacheMutex.Lock()
	defer groupCacheMutex.Unlock()

	// 记录当前数据库中存在的群组 ID，用于后续清理被删除的群组
	validGroupIDs := make(map[int]bool)

	for _, dbGroup := range dbGroups {
		modelGroup := dbGroup.ToModelGroup()
		validGroupIDs[modelGroup.ID] = true

		// 检查群组是否已经在内存中
		if existingGroup, exists := globalGroupCache[modelGroup.ID]; exists {
			// 【关键操作】：如果存在，只更新静态配置，绝对不碰 ConnPool 和 DevMap！
			existingGroup.Name = modelGroup.Name
			existingGroup.Type = modelGroup.Type
			existingGroup.CallSign = modelGroup.CallSign
			existingGroup.Password = modelGroup.Password
			existingGroup.AllowCallSignSSID = modelGroup.AllowCallSignSSID
			existingGroup.AllowDMRID = modelGroup.AllowDMRID
			existingGroup.OwerID = modelGroup.OwerID
			existingGroup.OwerCallSign = modelGroup.OwerCallSign
			existingGroup.MasterServer = modelGroup.MasterServer
			existingGroup.SlaveServer = modelGroup.SlaveServer
			existingGroup.Status = modelGroup.Status
			existingGroup.Note = modelGroup.Note
			existingGroup.UpdateTime = modelGroup.UpdateTime
			// 注意：ConnPool 和 DevMap 保持不变，在线设备状态不受影响
		} else {
			// 【关键操作】：如果是不存在的新群组，初始化它的动态连接池
			newGroup := modelGroup
			newGroup.ConnPool = &CurrentConnPool{
				DevConnMap:  make(map[string]*models.Device),
				DevConnList: make([]*models.Device, 0),
			}
			newGroup.DevMap = make(map[int]*models.Device)

			// 如果是会议模式，启动混音
			if newGroup.Type == models.GroupTypeMeeting {
				go startMixPCM(newGroup)
			}

			globalGroupCache[newGroup.ID] = newGroup
			log.Printf("[CACHE] 新群组已加载: %d - %s", newGroup.ID, newGroup.Name)
		}
	}

	// 清理内存中存在，但数据库中已经被删除/停用的群组
	for id := range globalGroupCache {
		if !validGroupIDs[id] {
			// 注意：这里只删除 map 中的引用，不影响正在使用该群组的设备
			// 它们会在下次心跳时重新路由到有效群组
			delete(globalGroupCache, id)
			log.Printf("[CACHE] 群组 %d 已从数据库移除，清理缓存", id)
		}
	}

	// 同时更新 publicGroupMap 以保持向后兼容
	publicGroupMap = globalGroupCache

	log.Printf("[CACHE] 群组状态同步完成，当前加载了 %d 个有效群组", len(globalGroupCache))
}

// refreshDeviceCache 同步设备状态从数据库到内存
// 核心原则：只更新动态属性（GroupID, DisableSend, DisableRecv, Priority），不碰连接状态
func refreshDeviceCache() {
	repo := gormdb.NewDeviceRepository()
	// 获取所有设备（使用较大的 limit 来获取全部）
	dbDevices, _, err := repo.ListDevices(10000, 1)
	if err != nil {
		log.Printf("[CACHE] 从数据库加载设备失败: %v", err)
		return
	}

	updatedCount := 0
	for _, dbDev := range dbDevices {
		usernameSSID := protocol.GetUsernameSSID(dbDev.Username, dbDev.SSID)

		// 只更新已在内存中的设备
		if memDev, exists := devUsernameSSIDMap[usernameSSID]; exists {
			// 检查是否需要更新（包括禁发/禁收状态）
			if memDev.GroupID != dbDev.GroupID || memDev.DisableSend != dbDev.DisableSend || memDev.DisableRecv != dbDev.DisableRecv || memDev.Priority != dbDev.Priority {
				memDev.GroupID = dbDev.GroupID
				memDev.DisableSend = dbDev.DisableSend
				memDev.DisableRecv = dbDev.DisableRecv
				memDev.Priority = dbDev.Priority
				updatedCount++
			}
		}
	}

	if updatedCount > 0 {
		log.Printf("[CACHE] 设备状态同步完成，更新了 %d 个设备的属性", updatedCount)
	}
}

// GetGroupFromCache 从缓存中获取群组（线程安全）
func GetGroupFromCache(groupID int) (*models.Group, bool) {
	groupCacheMutex.RLock()
	defer groupCacheMutex.RUnlock()
	gp, ok := globalGroupCache[groupID]
	return gp, ok
}

// GetAllGroupsFromCache 获取所有群组（线程安全）
func GetAllGroupsFromCache() map[int]*models.Group {
	groupCacheMutex.RLock()
	defer groupCacheMutex.RUnlock()

	// 返回副本以避免外部修改
	result := make(map[int]*models.Group, len(globalGroupCache))
	for k, v := range globalGroupCache {
		result[k] = v
	}
	return result
}
