package udphub

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	"draarl/pkg/cache"
)

// 全局变量声明
var (
	// 全局 UDP 连接
	globalConn *net.UDPConn

	// UDP 服务器关闭信号
	udpShutdown     chan struct{}
	udpShutdownOnce sync.Once

	// ==========================================
	// 性能优化：sync.Pool 复用 UDP 数据包内存
	// 避免每次处理数据包时分配 1460 字节的切片
	// ==========================================
	packetPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 1460)
		},
	}

	// ==========================================
	// 限速器：IP+Port 维度的包速率限制
	// 【前置逻辑说明】
	// 原 25 包/秒 对抗恶意攻击很好，但如果是 FRP 隧道转发（所有设备共用一个 IP），
	// 或者客户端网络卡顿后执行了"追赶发送"（瞬间连发3-4个包），极易被静默丢弃。
	// 大包架构下丢包体验极差，放宽至 150，兼顾防 DDoS 与业务容错。
	// ==========================================
	rateLimiter     = make(map[string]*rateLimitEntry)
	rateLimiterMu   sync.Mutex
	rateLimitMaxPps = 150 // 每秒最大包数 (25 → 150)

	// Username 索引的设备映射 (DraARLv1)
	devUsernameSSIDMap = make(map[string]*models.Device) // key: username-ssid

	// OwnerID 索引的设备映射（运行时唯一键）
	devOwnerSSIDMap = make(map[string]*models.Device) // key: owner_id-ssid

	// CallSign 索引的设备映射 (向后兼容)
	devCallsignSSIDMap = make(map[string]*models.Device) // key: callsign-ssid

	// 在线设备映射
	onlineDevMap       = make(map[int]*models.Device) // key: device ID
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
	// 性能优化：使用 atomic.Value 实现 RCU 模式，无锁读取
	// ==========================================
	globalGroupCacheAtomic atomic.Value // 存储 map[int]*models.Group
	groupCacheMutex        sync.RWMutex // 仅用于写操作保护

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
	DevConnMap    map[string]*models.Device // key: UDPAddr.String()
	DevConnList   []*models.Device
	UDPAddr       *net.UDPAddr
	LastVoiceTime time.Time
	LastPriority  int
}

// rateLimitEntry 限速器条目
type rateLimitEntry struct {
	count     int
	timestamp int64 // Unix 秒
}

// checkRateLimit 检查 IP+Port 的包速率
// 返回 true 表示允许通过，false 表示超限应丢弃
func checkRateLimit(addr string) bool {
	now := time.Now().Unix()

	rateLimiterMu.Lock()
	defer rateLimiterMu.Unlock()

	entry, exists := rateLimiter[addr]
	if !exists || entry.timestamp != now {
		// 新条目或新的一秒，重置计数
		rateLimiter[addr] = &rateLimitEntry{count: 1, timestamp: now}
		return true
	}

	// 同一秒内，检查是否超限
	if entry.count >= rateLimitMaxPps {
		return false
	}

	entry.count++
	return true
}

// cleanupRateLimiter 定期清理过期的限速器条目（由调用方在适当时机调用）
func cleanupRateLimiter() {
	now := time.Now().Unix()

	rateLimiterMu.Lock()
	defer rateLimiterMu.Unlock()

	for addr, entry := range rateLimiter {
		if now-entry.timestamp > 5 { // 清理 5 秒前的条目
			delete(rateLimiter, addr)
		}
	}
}

func isUDPShuttingDown() bool {
	if udpShutdown == nil {
		return false
	}
	select {
	case <-udpShutdown:
		return true
	default:
		return false
	}
}

func waitWithShutdown(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-udpShutdown:
		return false
	case <-timer.C:
		return true
	}
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
	udpShutdown = make(chan struct{})
	log.Printf("DraARLv1 UDP server started on port %d", port)

	// 启动认证失败记录清理器
	StartAuthCleaner()

	// 启动限速器定期清理（每 10 秒清理一次过期条目）
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-udpShutdown:
				return
			case <-ticker.C:
				cleanupRateLimiter()
			}
		}
	}()

	// 初始化公共群组
	initPublicGroups()
	initDeviceMACStore(config.Get())

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

	// 初始化通信录制管理器
	InitCommRecorder()

	// 【修复爆音方案1】初始化批量发送器
	InitBatchSender(conn)

	// 【修复爆音方案3】初始化语音平滑发送器
	InitVoiceSmoother(conn)

	// 处理数据包（响应关闭信号）
	for {
		select {
		case <-udpShutdown:
			log.Println("[UDP] 服务器正在关闭...")
			return nil
		case limitChan <- true:
			go processDraARLConn(conn)
		}
	}
}

// StopUDPServer 停止 UDP 服务器
func StopUDPServer() {
	udpShutdownOnce.Do(func() {
		close(udpShutdown)

		// 关闭 UDP 连接（这会使阻塞的 ReadFromUDP 返回错误）
		if globalConn != nil {
			globalConn.Close()
			globalConn = nil
		}

		// 停止相关组件
		StopCommRecorder()
		StopBatchSender()
		StopVoiceSmoother()
		StopTextMessageBuffer()

		log.Println("[UDP] 服务器已关闭")
	})
}

// processDraARLConn 处理 DraARLv1 UDP 连接
func processDraARLConn(conn *net.UDPConn) {
	defer func() { <-limitChan }()

	// 获取 PROXY Protocol 配置
	proxyProtocolEnabled := config.Get().System.ProxyProtocol == "v2"

	for {
		buf := packetPool.Get().([]byte)
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			packetPool.Put(buf)
			// 正常关闭连接时不记录错误日志（连接被主动关闭是预期的关闭行为）
			if isClosedConnError(err) {
				return
			}
			log.Printf("[ERROR] Read from UDP failed: %v", err)
			return
		}

		processDraARLDatagram(conn, buf[:n], remoteAddr, proxyProtocolEnabled)
		packetPool.Put(buf)
	}
}

// isClosedConnError 检查是否为连接关闭错误
func isClosedConnError(err error) bool {
	if err == nil {
		return false
	}
	// "use of closed network connection" 是连接被主动关闭时的标准错误
	errStr := err.Error()
	return strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "closed")
}

func processDraARLDatagram(conn *net.UDPConn, packetData []byte, remoteAddr *net.UDPAddr, proxyProtocolEnabled bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] Recovered from panic while processing packet from %v: %v", remoteAddr, r)
		}
	}()

	// 解析 PROXY Protocol 头部（如果启用）
	realAddr := remoteAddr
	if proxyProtocolEnabled {
		var proxyInfo *ProxyProtocolInfo
		var isProxyProtocol bool
		proxyInfo, packetData, isProxyProtocol = ParseProxyProtocolV2(packetData)
		if isProxyProtocol && proxyInfo != nil && proxyInfo.IsProxy {
			realAddr = GetRealAddr(remoteAddr, proxyInfo)
		}
	}

	// 处理 DraARLv1 数据包
	if len(packetData) >= 4 &&
		packetData[0] == 'D' &&
		packetData[1] == 'r' &&
		packetData[2] == 'a' &&
		packetData[3] == 'A' {
		processDraARLPacket(packetData, remoteAddr, realAddr, conn)
		return
	}
	log.Printf("[DECODE] Unknown protocol from %v (real: %v): %s", remoteAddr, realAddr, string(packetData[:min(4, len(packetData))]))
}

// processDraARLPacket 处理 DraARLv1 数据包
// remoteAddr: frp转发地址（用于发送响应）
// realAddr: 真实客户端地址（用于识别设备）
func processDraARLPacket(data []byte, remoteAddr, realAddr *net.UDPAddr, conn *net.UDPConn) {
	// 【安全校验】数据包大小限制，静默丢弃（避免日志开销）
	if len(data) > protocol.DraARLv1MaxPacketSize {
		return
	}

	// 【限速策略】IP+Port 维度，25 包/秒上限，静默丢弃
	if !checkRateLimit(realAddr.String()) {
		return
	}

	packet, err := protocol.NewDraARLv1Packet(remoteAddr, data)
	if err != nil {
		log.Printf("[DECODE] DraARLv1 decode error from %v: %v", realAddr, err)
		return
	}

	totalStats.PacketNumber++
	usernameSSID := protocol.GetUsernameSSID(packet.Username, packet.SSID)
	incomingMAC := protocol.ExtractHeartbeatMAC(packet.DATA)

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
	dev, isGhost := getDeviceFromMemory(packet.Username, packet.SSID, packet.UDPAddr)

	// 只有当设备不存在（未认证的新设备）且 SSID 为保留范围时才拒绝
	if dev == nil && protocol.IsReservedSSID(packet.SSID) {
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusReservedSSID, "reserved_ssid")
		return
	}

	if dev == nil {
		// 新设备，需要先认证
		handleNewDraARLDevice(packet, realAddr, conn, usernameSSID, incomingMAC)
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
			dev.LastPacketTime = packet.TimeStamp
			dev.UDPAddr = packet.UDPAddr
			// 继续后续处理
		} else {
			// 普通设备心跳：可能需要重新鉴权
			// 只有当设备原本处于离线状态，或者 IP 地址发生变化时才触发鉴权，节省性能
			if !dev.ISOnline || (dev.UDPAddr != nil && dev.UDPAddr.String() != currentAddr) {
				authResult := AuthenticateDevice(realAddr.IP.String(), packet.Username, packet.DevicePassword)
				if !authResult.Success {
					log.Printf("[AUTH] Device re-authentication failed: %s, error: %s", usernameSSID, authResult.Error)
					sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, authResult.Error)
					return
				}
				if shouldRejectNormalDeviceConflict(dev, packet.UDPAddr, incomingMAC) {
					log.Printf("[AUTH] Device conflict rejected: owner_id=%d ssid=%d existing_addr=%v new_addr=%v",
						dev.OwnerID, dev.SSID, dev.UDPAddr, packet.UDPAddr)
					sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusDeviceConflictOnline, "device_conflict_online")
					return
				}
				// 鉴权成功后，补全由于直接从 DB 加载可能缺失的呼号字段
				dev.CallSign = authResult.CallSign
				if authResult.User != nil {
					dev.Username = authResult.User.Name
				}
				log.Printf("[AUTH] Device re-authenticated: %s (%s) from %v", usernameSSID, dev.CallSign, currentAddr)
			}
		}
		if incomingMAC != "" {
			dev.MAC = incomingMAC
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
		// 检查群组是否已禁用（Status != 1）
		if gp.Status != 1 {
			// 群组已禁用，静默丢弃数据包（避免语音包持续发送时刷屏日志）
			return
		}
		parseDraARL(packet, data, dev, conn, gp, realAddr, isGhost)
	} else {
		// 找不到对应的群组实例
		// 可能是数据库中删除了该群组，或者设备被分配了一个错误的群组 ID
		if packet.Type != protocol.DraARLTypeHeartbeat {
			log.Printf("[ROUTING] 路由丢弃: 设备 %s 请求的群组 ID: %d 不存在", dev.Username, targetGroupID)
		}
	}
}

// getDeviceFromMemory 获取设备 (先查普通设备，再查 UDP 幽灵设备)
// 返回: device, isGhost (是否为 UDP 幽灵设备)
// 参数: username - 用户名（可能为空，幽灵设备发送时不带用户名）
// 参数: ssid - 设备 SSID
// 参数: udpAddr - UDP 地址（用于在 username 为空时查找幽灵设备）
func getDeviceFromMemory(username string, ssid byte, udpAddr *net.UDPAddr) (*models.Device, bool) {
	// 1. 如果 username 不为空，直接按 username-ssid 查找
	if username != "" {
		// 查普通设备
		usernameSSID := protocol.GetUsernameSSID(username, ssid)
		if dev, exists := devUsernameSSIDMap[usernameSSID]; exists {
			return dev, false
		}

		// 查 UDP 幽灵设备
		if ghost := GlobalUDPGhostManager.Get(username, ssid); ghost != nil {
			return ghost, true
		}

		return nil, false
	}

	// 2. username 为空时，通过 SSID + UDP 地址查找幽灵设备
	// 幽灵设备发送数据包时 username 为空，需要通过地址匹配
	if protocol.IsGhostSSID(ssid) && udpAddr != nil {
		ghost := GlobalUDPGhostManager.FindBySSIDAndAddr(ssid, udpAddr)
		if ghost != nil {
			return ghost, true
		}
	}

	return nil, false
}

// applyClientReportedDevModel 处理客户端上报的 DevModel：
// 1) 校验协议范围；
// 2) 仅在发生变化时更新内存；
// 3) 对已落库设备同步写回 devices.dev_model。
func applyClientReportedDevModel(dev *models.Device, reportedDevModel byte) {
	if dev == nil {
		return
	}
	if !protocol.IsValidClientReportedDevModel(reportedDevModel) {
		log.Printf("[DEV_MODEL] 忽略非法设备型号上报: device_id=%d username=%s ssid=%d reported=%d",
			dev.ID, dev.Username, dev.SSID, reportedDevModel)
		return
	}
	if dev.DevModel == reportedDevModel {
		return
	}

	oldModel := dev.DevModel
	dev.DevModel = reportedDevModel
	if dev.ID <= 0 {
		return
	}

	repo := gormdb.NewDeviceRepository()
	if err := repo.UpdateDeviceFields(dev.ID, map[string]interface{}{
		"dev_model": int(reportedDevModel),
	}); err != nil {
		log.Printf("[DEV_MODEL] 持久化设备型号失败: device_id=%d old=%d new=%d err=%v",
			dev.ID, oldModel, reportedDevModel, err)
		return
	}

	if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
		ctx := context.Background()
		_ = deviceCache.InvalidateDevice(ctx, dev.ID, dev.OwnerID, uint8(dev.SSID))
		_ = deviceCache.InvalidateDeviceList(ctx)
		if dev.GroupID > 0 {
			_ = deviceCache.InvalidateDevicesByGroup(ctx, dev.GroupID)
		}
	}

	log.Printf("[DEV_MODEL] 设备型号已更新: device_id=%d old=%d new=%d",
		dev.ID, oldModel, reportedDevModel)
}

// handleNewDraARLDevice 处理新 DraARLv1 设备
// realAddr: 真实客户端地址（用于识别设备和日志）
func handleNewDraARLDevice(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr, conn *net.UDPConn, usernameSSID string, incomingMAC string) {
	// 心跳包需要进行认证
	if packet.Type != protocol.DraARLTypeHeartbeat {
		// 非心跳包，忽略未认证设备
		log.Printf("[AUTH] Ignoring packet from unauthenticated device: %s, type: %d", usernameSSID, packet.Type)
		return
	}

	// 【安全校验】幽灵设备保留 SSID (100-105) 只能通过 JWT 认证
	// 普通设备不允许使用这些 SSID
	if protocol.IsReservedSSID(packet.SSID) {
		log.Printf("[AUTH] Device rejected: SSID %d is reserved for ghost devices (use JWT auth), device: %s", packet.SSID, usernameSSID)
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusReservedSSID, "reserved_ssid")
		return
	}

	// 认证设备（使用真实 IP）
	authResult := AuthenticateDevice(realAddr.IP.String(), packet.Username, packet.DevicePassword)
	if !authResult.Success {
		// 认证失败，不创建设备
		log.Printf("[AUTH] Device authentication failed: %s, error: %s", usernameSSID, authResult.Error)
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, authResult.Error)
		return
	}
	if authResult.User == nil {
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "user_not_found")
		return
	}

	if existingDev := findDeviceByOwnerSSIDFromMemory(authResult.User.ID, packet.SSID); shouldRejectNormalDeviceConflict(existingDev, packet.UDPAddr, incomingMAC) {
		log.Printf("[AUTH] Device conflict rejected: owner_id=%d ssid=%d existing_addr=%v new_addr=%v",
			authResult.User.ID, packet.SSID, existingDev.UDPAddr, packet.UDPAddr)
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
		SSID:     packet.SSID,
		OwnerID:  authResult.User.ID, // 设置所有者ID
		// 使用 fmt.Sprintf 安全地将数字 byte 转换为字符串拼接到呼号后
		CallSignSSID: fmt.Sprintf("%s-%d", authResult.CallSign, packet.SSID),
		DevModel:     reportedDevModel,
		MAC:          incomingMAC,
		Priority:     100,
		Status:       0,
		GroupID:      models.GroupIDPrivate1, // 默认加入群组1（避免 999 在部分库中缺失导致外键失败）
		LastOnlineIP: realAddr.IP.String(),
	}

	// 保存设备到数据库
	dev, err := addDevice(newDevice)
	if err != nil {
		log.Printf("[DEVICE] Add device failed: %v, %v", err, packet.Username)
		return
	}

	if dev != nil {
		applyClientReportedDevModel(dev, packet.DevModel)

		if dev.CallSign == "" {
			dev.CallSign = authResult.CallSign
		}
		if dev.Username == "" && authResult.User != nil {
			dev.Username = authResult.User.Name
		}
		if incomingMAC != "" {
			dev.MAC = incomingMAC
		}
		dev.CallSignSSID = fmt.Sprintf("%s-%d", dev.CallSign, dev.SSID)

		// UDPAddr 存储 frp 转发地址（用于发送响应）
		dev.UDPAddr = packet.UDPAddr
		dev.ISOnline = true
		dev.LastPacketTime = packet.TimeStamp
		dev.OnlineTime = packet.TimeStamp
		dev.LastOnlineIP = realAddr.IP.String()
		indexRuntimeDevice(dev)

		// 加入群组
		if gp, ok := publicGroupMap[dev.GroupID]; ok {
			gp.DevMap[dev.ID] = dev

			pool := gp.ConnPool.(*CurrentConnPool)
			syncDeviceConnPool(pool, dev, packet.UDPAddr)

			// 发送心跳响应（填充 CallSign）- 发送到 frp 转发地址
			response := protocol.EncodeHeartbeatResponse(packet, authResult.CallSign)
			conn.WriteToUDP(response, packet.UDPAddr)
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, 群组: %d)",
				packet.Username, dev.Name, realAddr, dev.GroupID)
		}
	}
}

// parseDraARL 解析并处理 DraARLv1 报文
// realAddr: 真实客户端地址（用于日志和 QTH 查询）
// isGhost: 是否为 UDP 幽灵设备
func parseDraARL(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group, realAddr *net.UDPAddr, isGhost bool) {
	switch packet.Type {
	case protocol.DraARLTypeControl:
		// 控制指令
		log.Printf("Received DraARLv1 control command: %v", packet)

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

// buildUDPSpeakerIdentity 为 UDP 设备构造半双工仲裁使用的说话人身份。
func buildUDPSpeakerIdentity(dev *models.Device, packet *protocol.DraARLv1Packet) (speakerID string, speakerLabel string) {
	if dev == nil {
		return "", ""
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
	if labelBase == "" && dev.UDPAddr != nil {
		labelBase = dev.UDPAddr.String()
	}
	if labelBase == "" {
		labelBase = "unknown"
	}
	speakerLabel = fmt.Sprintf("%s-%d", labelBase, ssid)

	switch {
	case dev.ID > 0:
		speakerID = fmt.Sprintf("udp_dev:%d", dev.ID)
	case dev.Username != "":
		speakerID = fmt.Sprintf("udp_user:%s:%d", dev.Username, ssid)
	case packet != nil && packet.Username != "":
		speakerID = fmt.Sprintf("udp_user:%s:%d", packet.Username, ssid)
	case dev.UDPAddr != nil:
		speakerID = fmt.Sprintf("udp_addr:%s:%d", dev.UDPAddr.String(), ssid)
	default:
		speakerID = fmt.Sprintf("udp_ssid:%d", ssid)
	}

	return speakerID, speakerLabel
}

// handleDraARLVoice 处理 DraARLv1 语音消息
func handleDraARLVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if dev.DisableSend {
		return
	}

	speakerID, speakerLabel := buildUDPSpeakerIdentity(dev, packet)
	if !tryAcquireHalfDuplex(gp.ID, speakerID, speakerLabel, packet.TimeStamp) {
		return
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
	totalStats.VoiceTime += voiceIncrement

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
		// 使用正数 ID 表示普通设备
		RecordCommPacket(int(dev.ID), uint8(dev.SSID), groupID, userID, packet.DATA)
	}

	forwardDraARLVoice(packet, dev, data, gp)
}

// handleDraARLHeartbeat 处理 DraARLv1 心跳包
// realAddr: 真实客户端地址（用于日志和 QTH 查询）
// isGhost: 是否为 UDP 幽灵设备
func handleDraARLHeartbeat(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group, realAddr *net.UDPAddr, isGhost bool) {
	wasOnline := dev.ISOnline
	currentAddr := packet.UDPAddr.String()
	addrChanged := dev.UDPAddr != nil && dev.UDPAddr.String() != currentAddr
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
					dev.Username, dev.SSID, lat, lon, alt)
			}
		} else {
			log.Printf("[GPS] %s-%d: 无效坐标 lat=%.6f, lon=%.6f (超出范围)",
				dev.Username, dev.SSID, lat, lon)
		}
	}

	// 更新设备地址和时间（UDPAddr 存储 frp 转发地址，用于发送响应）
	dev.UDPAddr = packet.UDPAddr
	dev.LastPacketTime = packet.TimeStamp
	if realIP != "" {
		dev.LastOnlineIP = realIP
	}
	applyClientReportedDevModel(dev, packet.DevModel)

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

	// 记录日志（非幽灵设备才记录）
	if !isGhost && !dev.Loged && packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds() > 200 {
		logBuffer <- dev
		dev.Loged = true
	}

	// 加入连接池（使用 frp 转发地址）
	pool := gp.ConnPool.(*CurrentConnPool)
	syncDeviceConnPool(pool, dev, packet.UDPAddr)

	// 发送心跳响应（填充 CallSign）- 发送到 frp 转发地址
	response := protocol.EncodeHeartbeatResponse(packet, dev.CallSign)
	conn.WriteToUDP(response, packet.UDPAddr)

	if !dev.ISOnline {
		// 新设备上线
		dev.OnlineTime = packet.TimeStamp

		// QTH 查询使用真实 IP
		if realAddr != nil && realAddr.IP != nil {
			dev.QTH = getQTH(realAddr.IP.String())
		}

		// 日志区分幽灵设备和普通设备
		if isGhost {
			log.Printf("[ONLINE] UDP幽灵设备 %s-%d 已上线 (地址: %v, 群组: %d, 型号: %d)",
				dev.Username, dev.SSID, realAddr, gp.ID, dev.DevModel)
		} else {
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, QTH: %v, 群组: %d, 型号: %d)",
				dev.Username, dev.Name, realAddr, dev.QTH, gp.ID, dev.DevModel)

			// 【配置同步】普通设备上线时同步配置
			// 仅对普通 UDP 设备进行配置同步（幽灵设备使用 WebSocket API）
			SyncDeviceConfig(dev)
		}

		dev.ISOnline = true
	}
}

// handleDraARLConfig 处理 DraARLv1 设备配置
func handleDraARLConfig(packet *protocol.DraARLv1Packet, dev *models.Device) {
	// 兼容旧的控制包格式（data[0] == 2 且长度 > 512）
	if len(packet.DATA) > 512 && packet.DATA[0] == 2 {
		dev.DeviceParm = decodeControlPacket(packet.DATA)
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
		log.Printf("[CONFIG] 设备 %s-%d 发送了意外的查询请求", dev.CallSign, dev.SSID)
	case ConfigTypeTimeSync:
		// 时间同步响应，通常不需要处理
		log.Printf("[CONFIG] 设备 %s-%d 确认时间同步", dev.CallSign, dev.SSID)
	}
}

// handleDraARLTextMessage 处理 DraARLv1 文本消息
func handleDraARLTextMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
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
		if dev.OwnerID > 0 {
			uid := uint(dev.OwnerID)
			userID = &uid
		}
		// 使用正数 ID 表示普通设备
		RecordTextMessage(int(dev.ID), uint8(dev.SSID), groupID, userID, string(packet.DATA))
	}
}

// handleDraARLServerVoice 处理 DraARLv1 服务器互联语音
func handleDraARLServerVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if dev.DisableSend {
		return
	}

	speakerID, speakerLabel := buildUDPSpeakerIdentity(dev, packet)
	if !tryAcquireHalfDuplex(gp.ID, speakerID, speakerLabel, packet.TimeStamp) {
		return
	}

	// 【前置逻辑说明】同上，服务器互联语音也使用 600ms 阈值
	td := packet.TimeStamp.Sub(dev.LastVoiceEndTime).Milliseconds()
	if td > 600 {
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

	// 【核心修复】重编码数据包：清空 password，填充 callsign
	refilledData := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 准入密码转发为空
		dev.SSID,
		protocol.DraARLTypeOpus16K,
		dev.DevModel,
		dev.DMRID,
		dev.CallSign, // 服务器填充呼号
		packet.DATA,  // 原始语音数据
	)

	// 1. 在本群组内广播（UDP 普通设备）
	forwardToUDPDevices(pool.DevConnList, dev.ID, gp.ID, true, refilledData)

	// 2. 【新增】转发给本群组的 UDP 幽灵设备
	forwardToGhostDevices(dev.Username, dev.SSID, gp.ID, refilledData)

	// 3. 检查该群组是否属于某个互联组，如果是，转发到互联组关联的其他群组
	forwardVoiceToLinkedGroups(dev, refilledData, gp.ID)

	// 4. 【关键修复】转发到 WebSocket 设备（UDP -> WS 桥梁）
	BroadcastVoiceFromUDP(dev, refilledData, gp.ID)
}

// forwardToGhostDevices 转发数据包给 UDP 幽灵设备
// sourceUsername: 源设备用户名
// sourceSSID: 源设备 SSID
// groupID: 目标群组 ID
// data: 要转发的数据
func forwardToGhostDevices(sourceUsername string, sourceSSID byte, groupID int, data []byte) {
	conn := globalConn
	if conn == nil {
		return
	}

	ghosts := GlobalUDPGhostManager.GetByGroup(groupID)
	if len(ghosts) == 0 {
		return
	}
	for _, ghost := range ghosts {
		// 跳过发送者自己
		if ghost.Username == sourceUsername && ghost.SSID == sourceSSID {
			continue
		}
		// 检查设备状态
		if !ghost.ISOnline || ghost.UDPAddr == nil || ghost.DisableRecv {
			continue
		}
		// 【前置逻辑说明：剥离批量/平滑缓冲，保障大包实时性】
		// 在 60ms-180ms 巨型音频帧架构下，平滑器 (VoiceSmoother) 和批量器 (BatchSender)
		// 反而会破坏原有的音频时间戳结构，造成瞬时大量吐包或无端延迟。
		// 放弃排队，直接使用原生 UDP 发送，将 Jitter 交给接收端的 Opus 解码器处理。
		conn.WriteToUDP(data, ghost.UDPAddr)
	}
}

// forwardVoiceToLinkedGroups 将语音转发到互联组关联的其他群组
func forwardVoiceToLinkedGroups(dev *models.Device, data []byte, sourceGroupID int) {
	targetGroupIDs := GetLinkedTargetGroups(sourceGroupID)
	if len(targetGroupIDs) == 0 {
		return // 不属于任何互联组，无需转发
	}

	for _, targetID := range targetGroupIDs {
		// 获取目标群组的转发
		if targetGroup, exists := GetGroupFromCache(targetID); exists {
			pool := targetGroup.ConnPool.(*CurrentConnPool)

			// 1. 发送给目标组的 UDP 普通设备（跨组不排除自己，因为源设备不在目标组）
			forwardToUDPDevices(pool.DevConnList, 0, targetID, false, data)

			// 2. 【新增】转发给目标组的 UDP 幽灵设备
			forwardToGhostDevices(dev.Username, dev.SSID, targetID, data)

			// 3. 【核心修复】：跨虚拟组时，必须同步桥接给目标组的 WS 客户端！
			// 否则 WS 客户端永远听不到其他实体组传来的 UDP 声音
			BroadcastVoiceFromUDP(dev, data, targetID)
		}
	}
}

// forwardDraARLMessage 转发 DraARLv1 文本消息
func forwardDraARLMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, pool *CurrentConnPool, gp *models.Group) {
	// 【核心修复】重编码数据包：清空 password，填充 callsign
	refilledData := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 准入密码转发为空
		dev.SSID,
		protocol.DraARLTypeTextMessage,
		dev.DevModel,
		dev.DMRID,
		dev.CallSign, // 服务器填充呼号
		packet.DATA,  // 原始文本数据
	)

	// 1. 在本群组内广播（UDP 设备）
	forwardToUDPDevices(pool.DevConnList, dev.ID, gp.ID, true, refilledData)

	// 2. 【新增】转发给本群组的 UDP 幽灵设备
	forwardToGhostDevices(dev.Username, dev.SSID, gp.ID, refilledData)

	// 3. 跨虚拟组转发文本消息
	forwardMessageToLinkedGroups(dev, refilledData, gp.ID)

	// 4. 【关键修复】转发到 WebSocket 设备（UDP -> WS 桥梁）
	BroadcastTextFromUDP(dev, refilledData, gp.ID)
}

// forwardMessageToLinkedGroups 将文本消息转发到互联组关联的其他群组
func forwardMessageToLinkedGroups(dev *models.Device, data []byte, sourceGroupID int) {
	targetGroupIDs := GetLinkedTargetGroups(sourceGroupID)
	if len(targetGroupIDs) == 0 {
		return
	}

	for _, targetID := range targetGroupIDs {
		if targetGroup, exists := GetGroupFromCache(targetID); exists {
			pool := targetGroup.ConnPool.(*CurrentConnPool)

			// 1. 发送给目标组的 UDP 设备
			forwardToUDPDevices(pool.DevConnList, 0, targetID, false, data)

			// 2. 【新增】转发给目标组的 UDP 幽灵设备
			forwardToGhostDevices(dev.Username, dev.SSID, targetID, data)

			// 3. 【核心修复】：同步桥接文本消息给目标组的 WS 客户端！
			BroadcastTextFromUDP(dev, data, targetID)
		}
	}
}

// forwardDraARLServerVoice 转发 DraARLv1 服务器互联语音
func forwardDraARLServerVoice(packet *protocol.DraARLv1Packet, dev *models.Device, data []byte, conn *net.UDPConn, gp *models.Group) {
	pool := gp.ConnPool.(*CurrentConnPool)

	// 【核心修复】重编码数据包：清空 password，填充 callsign
	// 服务器互联语音使用 Type 6，保留原始 DATA 区域的扩展头信息
	refilledData := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 准入密码转发为空
		dev.SSID,
		protocol.DraARLTypeServerVoice,
		dev.DevModel,
		dev.DMRID,
		dev.CallSign, // 服务器填充呼号
		packet.DATA,  // 原始语音数据（含扩展头）
	)

	// 1. 在本群组内广播（UDP 设备）
	forwardToUDPDevices(pool.DevConnList, dev.ID, gp.ID, true, refilledData)

	// 2. 跨虚拟组转发服务器语音
	forwardVoiceToLinkedGroups(dev, refilledData, gp.ID)

	// 3. 【关键修复】转发到 WebSocket 设备（UDP -> WS 桥梁）
	BroadcastVoiceFromUDP(dev, refilledData, gp.ID)
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ==========================================
// 性能优化：设备转发辅助函数
// 将多层嵌套 if 简化为组合条件，提高可读性和维护性
// ==========================================

// canForwardToDevice 检查是否可以转发数据到目标 UDP 设备
// 参数说明：
//   - target: 目标设备
//   - sourceID: 源设备 ID（用于排除自己）
//   - expectedGroupID: 期望的群组 ID（用于懒剔除）
//   - skipSelf: 是否排除自己
func canForwardToDevice(target *models.Device, sourceID int, expectedGroupID int, skipSelf bool) bool {
	// 组合条件：只要满足任一条件就跳过
	// 1. 排除自己（如果需要）
	// 2. 群组不匹配（懒剔除）
	// 3. 目标设备禁收
	// 4. 目标设备离线
	// 5. 目标地址无效
	if skipSelf && target.ID == sourceID {
		return false
	}
	if target.GroupID != expectedGroupID {
		return false
	}
	if target.DisableRecv {
		return false
	}
	if !target.ISOnline {
		return false
	}
	if target.UDPAddr == nil {
		return false
	}
	return true
}

// forwardToUDPDevices 统一的 UDP 设备转发逻辑
// 遍历设备列表，将数据转发给所有有效的目标设备
func forwardToUDPDevices(devices []*models.Device, sourceID int, expectedGroupID int, skipSelf bool, data []byte) {
	conn := globalConn
	if conn == nil {
		return
	}
	seenAddr := make(map[string]struct{}, len(devices))
	for _, target := range devices {
		if canForwardToDevice(target, sourceID, expectedGroupID, skipSelf) {
			addrKey := target.UDPAddr.String()
			if _, exists := seenAddr[addrKey]; exists {
				continue
			}
			seenAddr[addrKey] = struct{}{}
			conn.WriteToUDP(data, target.UDPAddr)
		}
	}
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
	InitGroupLinkCache() // 初始化群组互联缓存

	go func() {
		// 每隔 10 秒同步一次数据库中的群组和设备状态
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-udpShutdown:
				return
			case <-ticker.C:
				refreshGroupCache()
				refreshDeviceCache()
				refreshGroupLinkCache() // 同步群组互联缓存
			}
		}
	}()
	log.Println("[CACHE] 数据库群组和设备定时同步任务已启动 (间隔: 10s)")
}

// refreshGroupCache 执行具体的数据库查询与内存缓存增量合并更新
// 核心原则：只更新静态配置属性，绝对不碰动态连接池(ConnPool)
// 性能优化：使用 RCU 模式，构建新 map 后原子替换，避免阻塞读取
func refreshGroupCache() {
	repo := gormdb.NewGroupRepository()
	dbGroups, err := repo.ListGroups()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载群组失败: %v", err)
		return
	}

	// 获取当前缓存（用于合并）
	oldCache := globalGroupCacheAtomic.Load()
	var oldGroupCache map[int]*models.Group
	if oldCache != nil {
		oldGroupCache = oldCache.(map[int]*models.Group)
	} else {
		oldGroupCache = make(map[int]*models.Group)
	}

	// 性能优化：RCU 模式 - 构建新的 map，不阻塞读取
	newGroupCache := make(map[int]*models.Group, len(dbGroups)+2)

	// 记录当前数据库中存在的群组 ID
	validGroupIDs := make(map[int]bool, len(dbGroups)+2)

	// 【关键修复】公共群组 0 和 999 始终有效
	validGroupIDs[0] = true
	validGroupIDs[models.GroupIDPublicMin] = true

	for _, dbGroup := range dbGroups {
		modelGroup := dbGroup.ToModelGroup()
		validGroupIDs[modelGroup.ID] = true

		// 检查群组是否已经在内存中
		if existingGroup, exists := oldGroupCache[modelGroup.ID]; exists {
			// 【关键操作】：如果存在，复制指针到新 map，并更新静态配置
			// 注意：这里直接修改 existingGroup 的字段是安全的，因为指针不变
			existingGroup.Name = modelGroup.Name
			existingGroup.Type = modelGroup.Type
			existingGroup.CallSign = modelGroup.CallSign
			existingGroup.Password = modelGroup.Password
			existingGroup.AllowCallSignSSID = modelGroup.AllowCallSignSSID
			existingGroup.OwerID = modelGroup.OwerID
			existingGroup.MasterServer = modelGroup.MasterServer
			existingGroup.SlaveServer = modelGroup.SlaveServer
			existingGroup.Status = modelGroup.Status
			existingGroup.IsVirtual = modelGroup.IsVirtual
			existingGroup.Note = modelGroup.Note
			existingGroup.UpdateTime = modelGroup.UpdateTime
			// 注意：ConnPool 和 DevMap 保持不变

			newGroupCache[modelGroup.ID] = existingGroup
		} else {
			// 【关键操作】：如果是不存在的新群组，初始化它的动态连接池
			newGroup := modelGroup
			// 性能优化：预分配连接池容量
			newGroup.ConnPool = &CurrentConnPool{
				DevConnMap:  make(map[string]*models.Device, 32),
				DevConnList: make([]*models.Device, 0, 32),
			}
			newGroup.DevMap = make(map[int]*models.Device, 32)

			newGroupCache[newGroup.ID] = newGroup
			log.Printf("[CACHE] 新群组已加载: %d - %s", newGroup.ID, newGroup.Name)
		}
	}

	// 复制旧缓存中仍有效的群组（数据库中未变更的）
	for id := range oldGroupCache {
		if _, valid := validGroupIDs[id]; valid {
			// 已经在上面处理过，跳过
			continue
		}
		// 数据库中已删除的群组，不复制到新缓存
		log.Printf("[CACHE] 群组 %d 已从数据库移除，清理缓存", id)
	}

	// 原子替换缓存指针（RCU 模式）
	globalGroupCacheAtomic.Store(newGroupCache)

	// 同时更新 publicGroupMap 以保持向后兼容
	publicGroupMap = newGroupCache

	log.Printf("[CACHE] 群组状态同步完成，当前加载了 %d 个有效群组", len(newGroupCache))
}

// refreshDeviceCache 同步设备状态从数据库到内存
// 核心原则：只更新动态属性（GroupID, DisableSend, DisableRecv, Priority），不碰连接状态
// 同时将内存中的在线状态同步回数据库，供 Web 端查询
func refreshDeviceCache() {
	repo := gormdb.NewDeviceRepository()
	// 获取所有设备（使用较大的 limit 来获取全部）
	dbDevices, _, err := repo.ListDevices(10000, 1)
	if err != nil {
		log.Printf("[CACHE] 从数据库加载设备失败: %v", err)
		return
	}

	updatedCount := 0
	onlineSyncCount := 0

	// 用户仓库用于获取用户名/呼号
	userRepo := gormdb.NewUserRepository()
	userCache := make(map[int]*gormdb.User)

	for _, dbDev := range dbDevices {
		memDev := findDeviceByOwnerSSIDFromMemory(dbDev.OwnerID, dbDev.SSID)
		if memDev == nil {
			continue
		}

		if dbDev.OwnerID > 0 {
			owner, ok := userCache[dbDev.OwnerID]
			if !ok {
				if user, err := userRepo.GetUserByID(dbDev.OwnerID); err == nil && user != nil {
					owner = user
				}
				userCache[dbDev.OwnerID] = owner
			}

			if owner != nil {
				if memDev.Username != owner.Name {
					removeRuntimeUsernameKey(memDev, memDev.Username)
					memDev.Username = owner.Name
					indexRuntimeDevice(memDev)
				}
				if memDev.CallSign != owner.CallSign {
					removeRuntimeCallSignKey(memDev, memDev.CallSign)
					memDev.CallSign = owner.CallSign
					indexRuntimeDevice(memDev)
				}
			}
		}

		// 检查是否需要更新（包括禁发/禁收状态）
		if memDev.GroupID != dbDev.GroupID || memDev.DisableSend != dbDev.DisableSend || memDev.DisableRecv != dbDev.DisableRecv || memDev.Priority != dbDev.Priority {
			memDev.GroupID = dbDev.GroupID
			memDev.DisableSend = dbDev.DisableSend
			memDev.DisableRecv = dbDev.DisableRecv
			memDev.Priority = dbDev.Priority
			updatedCount++
		}

		onlineStateChanged := memDev.ISOnline != dbDev.ISOnline
		lastOnlineIPChanged := memDev.LastOnlineIP != "" && memDev.LastOnlineIP != dbDev.LastOnlineIP

		// 在线状态与最近上线 IP 的变更都需要同步到数据库，并使缓存失效。
		if onlineStateChanged || lastOnlineIPChanged {
			onlineTime := ""
			if onlineStateChanged && memDev.ISOnline && !memDev.OnlineTime.IsZero() {
				onlineTime = memDev.OnlineTime.Format("2006-01-02 15:04:05")
			}
			repo.UpdateDeviceOnlineStatus(memDev.OwnerID, uint8(memDev.SSID), memDev.ISOnline, onlineTime, memDev.LastOnlineIP)
			onlineSyncCount++

			// 获取缓存接口实例
			if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
				ctx := context.Background()

				// 1. 失效单个设备的详细信息缓存
				_ = deviceCache.InvalidateDevice(ctx, memDev.ID, memDev.OwnerID, uint8(memDev.SSID))

				// 2. 失效全局设备分页列表缓存，确保前端 "所有设备" 页面能刷新状态
				_ = deviceCache.InvalidateDeviceList(ctx)

				// 3. 如果设备已经加入某个群组，还要失效该群组的设备列表缓存
				// 确保前端 "群组内的设备列表" 也能立刻体现设备的上下线情况
				if memDev.GroupID > 0 {
					_ = deviceCache.InvalidateDevicesByGroup(ctx, memDev.GroupID)
				}
			}
		}
	}

	if updatedCount > 0 {
		log.Printf("[CACHE] 设备属性同步完成，更新了 %d 个设备", updatedCount)
	}
	if onlineSyncCount > 0 {
		log.Printf("[CACHE] 设备在线状态/IP 已同步到数据库，更新了 %d 个设备", onlineSyncCount)
	}
}

// GetGroupFromCache 从缓存中获取群组（线程安全）
// 性能优化：使用 RCU 模式，无锁读取
func GetGroupFromCache(groupID int) (*models.Group, bool) {
	cache := globalGroupCacheAtomic.Load()
	if cache == nil {
		return nil, false
	}
	groupCache := cache.(map[int]*models.Group)
	gp, ok := groupCache[groupID]
	return gp, ok
}

// GetAllGroupsFromCache 获取所有群组（线程安全）
func GetAllGroupsFromCache() map[int]*models.Group {
	cache := globalGroupCacheAtomic.Load()
	if cache == nil {
		return make(map[int]*models.Group)
	}
	groupCache := cache.(map[int]*models.Group)

	// 返回副本以避免外部修改
	result := make(map[int]*models.Group, len(groupCache))
	for k, v := range groupCache {
		result[k] = v
	}
	return result
}
