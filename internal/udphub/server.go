package udphub

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"strconv"
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
	// 限速器：IP+Port 维度的包速率限制（分片实现见 rate_limit.go）
	// 原 25 包/秒 对抗恶意攻击很好，但如果是 FRP 隧道转发（所有设备共用一个 IP），
	// 或者客户端网络卡顿后执行了"追赶发送"（瞬间连发3-4个包），极易被静默丢弃。
	// 大包架构下丢包体验极差，放宽至 150，兼顾防 DDoS 与业务容错。
	// ==========================================
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

	// 公共群组映射 (保留用于向后兼容)
	publicGroupMap = make(map[int]*models.Group)

	// ==========================================
	// 架构重构：全局统一群组缓存
	// 替代原有的 publicGroupMap 和 userList 的群组路由功能
	// 性能优化：使用 atomic.Value 实现 RCU 模式，无锁读取
	// ==========================================
	globalGroupCacheAtomic atomic.Value // 存储 map[int]*models.Group
	groupCacheMutex        sync.RWMutex // 仅用于写操作保护
	groupRuntimeMu         sync.RWMutex // 保护群组动态成员、设备列表和统计字段

	// 用户列表 (sync.Map)
	userList sync.Map

	// 统计信息（热路径用 atomic 更新，避免 data race）
	totalStats = &models.ServerStats{}
	// OnlineDevNumber 单独原子存储（int 字段不便直接 atomic）
	totalStatsOnline int64

	// 日志缓冲
	logBuffer = make(chan *models.Device, 1000)
)

// UserInfo 用户信息结构
type UserInfo struct {
	ID       int
	CallSign string
	Name     string
	Groups   map[int]*models.Group
}

// CurrentConnPool 当前连接池
// DevConnList 通过 atomic.Value 做 RCU 快照，热路径只读、写路径整体替换。
type CurrentConnPool struct {
	mu            sync.Mutex
	DevConnMap    map[string]*models.Device // key: UDPAddr.String()
	devConnList   atomic.Value              // stores []*models.Device
	UDPAddr       *net.UDPAddr
	LastVoiceTime time.Time
	LastPriority  int
}

// rateLimitEntry 限速器条目
type rateLimitEntry struct {
	count     int
	timestamp int64 // Unix 秒
}

// snapshotConnList 返回当前连接列表快照（只读，禁止修改返回切片）。
func (p *CurrentConnPool) snapshotConnList() []*models.Device {
	if p == nil {
		return nil
	}
	if v := p.devConnList.Load(); v != nil {
		if list, ok := v.([]*models.Device); ok {
			return list
		}
	}
	// 兼容旧代码仍写 DevConnList 字段的路径（过渡期）
	return nil
}

// storeConnList 原子替换连接列表快照。
func (p *CurrentConnPool) storeConnList(list []*models.Device) {
	if p == nil {
		return
	}
	if list == nil {
		list = make([]*models.Device, 0)
	}
	p.devConnList.Store(list)
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
	return startDraARLServer(port, nil)
}

// StartUDPServerWithReady reports the bind/pipeline initialization result
// exactly once. The service continues running until StopUDPServer is called.
// This lets the main process start the TLS node control plane only after the
// shared UDP socket is ready for both device and Type 0 traffic.
func StartUDPServerWithReady(port int, ready chan<- error) error {
	return startDraARLServer(port, ready)
}

// StartDraARLServer 启动 DraARLv1 协议的 UDP 服务器
func StartDraARLServer(port int) error {
	return startDraARLServer(port, nil)
}

func startDraARLServer(port int, ready chan<- error) (result error) {
	reported := false
	report := func(err error) {
		if ready == nil || reported {
			return
		}
		reported = true
		ready <- err
	}
	defer func() {
		if !reported {
			report(result)
		}
	}()
	network := "udp"
	host := ""
	if cfg := config.TryGet(); cfg != nil {
		host = strings.TrimSpace(cfg.System.Host)
		if ip := net.ParseIP(host); ip != nil {
			if ip.To4() != nil {
				network = "udp4"
			} else {
				network = "udp6"
			}
		}
	}
	addr, err := net.ResolveUDPAddr(network, net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		result = fmt.Errorf("resolve UDP address failed: %w", err)
		return result
	}

	conn, err := net.ListenUDP(network, addr)
	if err != nil {
		result = fmt.Errorf("listen UDP failed: %w", err)
		return result
	}
	configureUDPSocketBuffers(conn)

	globalConn = conn
	udpShutdown = make(chan struct{})
	log.Printf("DraARLv1 UDP server started on %s (%s)", conn.LocalAddr(), network)

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

	// 冷启动时先清理数据库残留的在线态。互联模式短时保留远端
	// ownership，供仍在运行的边缘证明中心重启前的会话归属。
	deviceRepo := gormdb.NewDeviceRepository()
	preserveRemoteEntries := false
	if cfg := config.TryGet(); cfg != nil {
		preserveRemoteEntries = cfg.Interconnect.Enabled
	}
	if err := deviceRepo.PrepareDevicesForStartup(preserveRemoteEntries); err != nil {
		log.Printf("[UDP] Reset persisted device online flags failed: %v", err)
	} else {
		log.Printf("[UDP] Reset persisted device online flags on startup (preserve_remote_entries=%t)", preserveRemoteEntries)
	}

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

	// 大群并行 fan-out 发送（热路径优先）
	InitFanoutSender(conn)

	// 域级接收者缓存
	InitDomainReceiverCache()

	// 单/少 reader + worker 池，避免多 goroutine 争抢同一 socket
	startUDPPipeline(conn)
	report(nil)

	// 等待关闭
	<-udpShutdown
	log.Println("[UDP] 服务器正在关闭...")
	return nil
}

// StopUDPServer 停止 UDP 服务器
func StopUDPServer() {
	udpShutdownOnce.Do(func() {
		// StartDraARLServer can fail before creating the shutdown channel
		// (for example when the configured port is already in use). Keep the
		// process-level shutdown path idempotent instead of panicking there.
		if udpShutdown == nil && globalConn == nil {
			return
		}
		if udpShutdown != nil {
			close(udpShutdown)
		}

		// 关闭 UDP 连接（这会使阻塞的 ReadFromUDP 返回错误）
		if globalConn != nil {
			globalConn.Close()
		}

		// 等待收包流水线退出
		stopUDPPipeline()

		globalConn = nil

		// 停止相关组件
		StopCommRecorder()
		// BatchSender/VoiceSmoother 已从热路径移除，不再初始化
		StopFanoutSender()
		StopDomainReceiverCache()
		StopTextMessageBuffer()

		log.Println("[UDP] 服务器已关闭")
	})
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
	dev, isGhost := getDeviceFromMemory(packet.Username, packet.SSID, packet.UDPAddr)

	// 只有当设备不存在（未认证的新设备）且 SSID 为保留范围时才拒绝
	if dev == nil && protocol.IsReservedSSID(packet.SSID) {
		sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusReservedSSID, "reserved_ssid")
		return
	}

	if dev == nil {
		// 新设备，需要先认证
		handleNewDraARLDevice(packet, realAddr, conn, protocol.GetUsernameSSID(packet.Username, packet.SSID), incomingMAC)
		return
	}

	// A runtime object may remain available for management while its current
	// authoritative entry is an edge. It cannot be reused by an old centre UDP
	// address until a heartbeat/JWT authentication takes ownership back.
	remoteOwner := dev.CurrentEntryNodeID != "" && dev.CurrentEntryNodeID != "center"
	if remoteOwner && packet.Type != protocol.DraARLTypeHeartbeat {
		return
	}

	// ==========================================
	// 已存在设备的处理
	// ==========================================
	if packet.Type == protocol.DraARLTypeHeartbeat {
		usernameSSID := protocol.GetUsernameSSID(packet.Username, packet.SSID)
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
			localSessionMissing := CenterInterconnectActive() && !CenterLocalDeviceAuthoritative(dev)
			needsCenterActivation := remoteOwner || localSessionMissing || !dev.ISOnline || dev.CurrentEntryNodeID != "center"
			if remoteOwner || localSessionMissing || !dev.ISOnline || dev.UDPAddr == nil || dev.UDPAddr.String() != currentAddr {
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
			if needsCenterActivation {
				if err := activateAndPersistCenterDevice(dev); err != nil {
					log.Printf("[INTERCONNECT] activate centre device %d failed: %v", dev.ID, err)
					sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "center_session_activation_failed")
					return
				}
			}
		}
		if incomingMAC != "" {
			dev.MAC = incomingMAC
		}
	}
	if (packet.Type == protocol.DraARLTypeTextMessage || packet.Type == protocol.DraARLTypeOpus16K) && !CenterLocalDeviceAuthoritative(dev) {
		return
	}

	// 已存在的设备，更新状态
	dev.LastPacketTime = packet.TimeStamp
	dev.Traffic += int64(protocol.DraARLv1HeaderSize + len(packet.DATA))
	atomicAddTraffic(int64(protocol.DraARLv1HeaderSize + len(packet.DATA)))

	targetGroupID := dev.GroupID
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

// getDeviceFromMemory 获取设备 (先查普通设备，再查 UDP 幽灵设备)
// 返回: device, isGhost (是否为 UDP 幽灵设备)
// 参数: username - 用户名（可能为空，幽灵设备发送时不带用户名）
// 参数: ssid - 设备 SSID
// 参数: udpAddr - UDP 地址（用于在 username 为空时查找幽灵设备）
func getDeviceFromMemory(username string, ssid byte, udpAddr *net.UDPAddr) (*models.Device, bool) {
	// 1. 如果 username 不为空，直接按 username-ssid 查找
	if username != "" {
		if dev := lookupDeviceByUsernameSSID(username, ssid); dev != nil {
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
		if dev.ID > 0 {
			if err := activateAndPersistCenterDevice(dev); err != nil {
				log.Printf("[INTERCONNECT] activate new centre device %d failed: %v", dev.ID, err)
				sendHeartbeatReject(conn, packet, protocol.HeartbeatStatusAuthFailed, "center_session_activation_failed")
				return
			}
		}

		// 默认群组为空时只登记并保持在线，不进入任何转发池。
		if gp, ok := GetGroupFromCache(dev.GroupID); dev.GroupID > 0 && ok {
			attachRuntimeDeviceToGroup(gp, dev)
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, 群组: %d)",
				packet.Username, dev.Name, realAddr, dev.GroupID)
		} else if dev.GroupID == 0 {
			log.Printf("[ONLINE] %s 的设备 %d 已登记为未分组状态，不参与转发", packet.Username, dev.ID)
		} else {
			// 已有设备可能在群组缓存切换的极短窗口内重连。保持在线并响应
			// 心跳，但在目标群组可用前不把它挂入任何转发池。
			log.Printf("[ONLINE] 设备 %d 的群组 %d 暂不在运行时缓存中，本次不参与转发", dev.ID, dev.GroupID)
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
	case dev.OwnerID > 0:
		key = 0x5000000000000000 | uint64(uint32(dev.OwnerID))<<8 | uint64(ssid)
	case dev.Username != "":
		key = 0x6000000000000000 | uint64(fnv32String(dev.Username))<<8 | uint64(ssid)
	case packet != nil && packet.Username != "":
		key = 0x6000000000000000 | uint64(fnv32String(packet.Username))<<8 | uint64(ssid)
	case dev.UDPAddr != nil:
		if addr, ok := udpAddrPort(dev.UDPAddr); ok {
			key = 0x7000000000000000 | (hashAddrPort(addr) & 0x0fffffffffffffff)
		}
	default:
		key = 0x7000000000000000 | uint64(ssid)
	}
	return halfDuplexSpeaker{key: key, labelBase: labelBase, ssid: ssid}
}

func canSendFromDevice(dev *models.Device) bool {
	return dev != nil && !dev.DisableSend
}

// handleDraARLVoice 处理 DraARLv1 语音消息
func handleDraARLVoice(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, gp *models.Group) {
	// 检查设备是否被禁发
	if !canSendFromDevice(dev) {
		return
	}

	if CenterInterconnectActive() {
		if !AcquireCenterLocalDeviceVoice(dev) {
			return
		}
	} else if !tryAcquireHalfDuplex(gp.ID, buildUDPSpeaker(dev, packet), packet.TimeStamp) {
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
			endpoint := "unknown"
			if dev.UDPAddr != nil {
				endpoint = dev.UDPAddr.String()
			}
			sourceKey = GhostCommRecordSourceKey("udp", dev.OwnerID, uint8(dev.SSID), endpoint)
		}
		RecordCommPacket(sourceKey, recordDeviceID, uint8(dev.SSID), groupID, userID, packet.DATA)
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

	// 未分组设备没有连接池，但仍需正常响应心跳并保持在线可管理。
	if gp != nil {
		syncDeviceConnPool(getGroupConnPool(gp), dev, packet.UDPAddr)
	}

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
		groupID := 0
		if gp != nil {
			groupID = gp.ID
		}
		if isGhost {
			log.Printf("[ONLINE] UDP幽灵设备 %s-%d 已上线 (地址: %v, 群组: %d, 型号: %d)",
				dev.Username, dev.SSID, realAddr, groupID, dev.DevModel)
		} else {
			log.Printf("[ONLINE] %s的-%s 已上线 (地址: %v, QTH: %v, 群组: %d, 型号: %d)",
				dev.Username, dev.Name, realAddr, dev.QTH, groupID, dev.DevModel)

			// 【配置同步】普通设备上线时同步配置
			// 仅对普通 UDP 设备进行配置同步（幽灵设备使用 WebSocket API）
			SyncDeviceConfig(dev)
		}

		dev.ISOnline = true
	}
}

func activateAndPersistCenterDevice(dev *models.Device) error {
	if dev == nil || dev.ID <= 0 {
		return nil
	}
	if CenterInterconnectActive() {
		return ActivateCenterLocalDevice(dev)
	}
	now := time.Now()
	if err := gormdb.NewDeviceRepository().UpdateDeviceEntry(dev.ID, "center", "center", 0, true, now); err != nil {
		return err
	}
	SyncRuntimeDeviceEntry(dev.ID, "center", "center", 0, true, now)
	return nil
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
	if !canSendFromDevice(dev) {
		return
	}
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

// forwardDraARLMessage 转发 DraARLv1 文本消息
func forwardDraARLMessage(packet *protocol.DraARLv1Packet, data []byte, dev *models.Device, conn *net.UDPConn, pool *CurrentConnPool, gp *models.Group) {
	refilledData := protocol.PrepareForwardPacket(
		data,
		dev.Username,
		dev.CallSign,
		dev.SSID,
		protocol.DraARLTypeTextMessage,
		dev.DevModel,
		dev.DMRID,
		packet.DATA,
	)

	// 文本同样走连通域 UDP fan-out
	forwardVoiceDomain(dev, refilledData, gp.ID)
	BroadcastTextFromUDPDomain(dev, refilledData, gp.ID)
	if err := RelayCenterLocalDevice(dev, refilledData); err != nil {
		log.Printf("[INTERCONNECT] relay centre text failed: device=%d err=%v", dev.ID, err)
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

// GetGlobalConn 获取全局 UDP 连接
func GetGlobalConn() *net.UDPConn {
	return globalConn
}

// GetTotalStats 获取服务器统计信息（返回快照副本，避免并发读写）
func GetTotalStats() *models.ServerStats {
	return &models.ServerStats{
		PacketNumber:    atomic.LoadInt64(&totalStats.PacketNumber),
		VoiceTime:       atomic.LoadInt64(&totalStats.VoiceTime),
		Traffic:         atomic.LoadInt64(&totalStats.Traffic),
		OnlineDevNumber: int(atomic.LoadInt64(&totalStatsOnline)),
	}
}

func atomicAddPacketNumber(n int64) {
	atomic.AddInt64(&totalStats.PacketNumber, n)
}

func atomicAddTraffic(n int64) {
	atomic.AddInt64(&totalStats.Traffic, n)
}

func atomicAddVoiceTime(n int64) {
	atomic.AddInt64(&totalStats.VoiceTime, n)
}

func setOnlineDevNumber(n int) {
	atomic.StoreInt64(&totalStatsOnline, int64(n))
}

func getOnlineDevNumber() int {
	return int(atomic.LoadInt64(&totalStatsOnline))
}

// GetUserList 获取用户列表
func GetUserList() *sync.Map {
	return &userList
}

// GetPublicGroupMap 获取公共群组映射
func GetPublicGroupMap() map[int]*models.Group {
	return GetAllGroupsFromCache()
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
	groupCacheMutex.Lock()
	defer groupCacheMutex.Unlock()

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
	receiverRoutingChanged := false

	// 记录当前数据库中存在的群组 ID
	validGroupIDs := make(map[int]bool, len(dbGroups)+2)

	// 协议级公共群组 999 始终有效；0 仅表示设备未分组，不是群组对象。
	validGroupIDs[models.GroupIDPublicMin] = true

	for _, dbGroup := range dbGroups {
		modelGroup := dbGroup.ToModelGroup()
		validGroupIDs[modelGroup.ID] = true

		// 检查群组是否已经在内存中
		if existingGroup, exists := oldGroupCache[modelGroup.ID]; exists {
			// RCU 发布前构建新静态对象，避免原地修改已被并发读者持有的群组。
			// 动态设备集合与连接池继续复用，在线设备不会因配置刷新而断开。
			if existingGroup.Status != modelGroup.Status {
				receiverRoutingChanged = true
			}
			groupRuntimeMu.RLock()
			modelGroup.DevMap = existingGroup.DevMap
			modelGroup.DevList = append([]int(nil), existingGroup.DevList...)
			modelGroup.OnlineDevNumber = existingGroup.OnlineDevNumber
			modelGroup.TotalDevNumber = existingGroup.TotalDevNumber
			groupRuntimeMu.RUnlock()
			modelGroup.ConnPool = existingGroup.ConnPool
			newGroupCache[modelGroup.ID] = modelGroup
		} else {
			receiverRoutingChanged = true
			// 【关键操作】：如果是不存在的新群组，初始化它的动态连接池
			newGroup := modelGroup
			// 性能优化：预分配连接池容量
			pool := &CurrentConnPool{
				DevConnMap: make(map[string]*models.Device, 32),
			}
			pool.storeConnList(make([]*models.Device, 0, 32))
			newGroup.ConnPool = pool
			newGroup.DevMap = make(map[int]*models.Device, 32)

			newGroupCache[newGroup.ID] = newGroup
			log.Printf("[CACHE] 新群组已加载: %d - %s", newGroup.ID, newGroup.Name)
		}
	}
	// 999 是协议级系统群组，即使历史数据库没有对应行也必须保留。
	// 旧实现只把它标为 valid，却没有复制到 newGroupCache，首次刷新后会丢失。
	if _, exists := newGroupCache[models.GroupIDPublicMin]; !exists {
		if existing, ok := oldGroupCache[models.GroupIDPublicMin]; ok && existing != nil {
			newGroupCache[models.GroupIDPublicMin] = existing
		} else {
			newGroupCache[models.GroupIDPublicMin] = &models.Group{
				ID:         models.GroupIDPublicMin,
				Name:       "全网互联",
				Type:       models.GroupTypeRelay,
				Status:     1,
				DevMap:     make(map[int]*models.Device),
				CreateTime: time.Now().Format("2006-01-02 15:04:05"),
				UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
				ConnPool:   newConnPool(),
			}
		}
	}

	// 复制旧缓存中仍有效的群组（数据库中未变更的）
	for id := range oldGroupCache {
		if _, valid := validGroupIDs[id]; valid {
			// 已经在上面处理过，跳过
			continue
		}
		// 数据库中已删除的群组，不复制到新缓存
		receiverRoutingChanged = true
		log.Printf("[CACHE] 群组 %d 已从数据库移除，清理缓存", id)
	}

	// 原子替换缓存指针（RCU 模式）
	globalGroupCacheAtomic.Store(newGroupCache)

	// 同时更新 publicGroupMap 以保持向后兼容
	publicGroupMap = newGroupCache
	if receiverRoutingChanged {
		resetDomainGroupReverseCache()
		InvalidateDomainReceiverCache()
	}

	log.Printf("[CACHE] 群组状态同步完成，当前加载了 %d 个有效群组", len(newGroupCache))
}

// refreshDeviceCache 同步设备状态从数据库到内存
// 核心原则：只更新动态属性（GroupID, DisableSend, DisableRecv, Priority），不碰连接状态
// 同时将内存中的在线状态同步回数据库，供 Web 端查询
func refreshDeviceCache() {
	repo := gormdb.NewDeviceRepository()
	dbDevices, err := repo.ListAllDevices()
	if err != nil {
		log.Printf("[CACHE] 从数据库加载设备失败: %v", err)
		return
	}

	updatedCount := 0
	onlineSyncCount := 0
	removedCount := 0
	receiverRoutingChanged := false

	dbDeviceKeys := make(map[string]struct{}, len(dbDevices))
	for _, dbDev := range dbDevices {
		dbDeviceKeys[getOwnerSSIDKey(dbDev.OwnerID, dbDev.SSID)] = struct{}{}
	}

	userCache := loadDeviceOwnerCache(dbDevices)

	for _, dbDev := range dbDevices {
		memDev := findDeviceByOwnerSSIDFromMemory(dbDev.OwnerID, dbDev.SSID)
		if memDev == nil {
			continue
		}

		if dbDev.OwnerID > 0 {
			owner := userCache[dbDev.OwnerID]
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

		// 群组变化必须走统一的 detach/attach 流程，不能只修改 GroupID 字段，
		// 否则旧连接池仍会继续向该设备转发。
		if memDev.GroupID != dbDev.GroupID {
			if _, err := changeDeviceGroup(memDev, dbDev.GroupID); err != nil {
				log.Printf("[CACHE] 同步设备 %d 群组 %d -> %d 失败: %v", memDev.ID, memDev.GroupID, dbDev.GroupID, err)
			} else {
				receiverRoutingChanged = true
				updatedCount++
			}
		}
		if memDev.DisableSend != dbDev.DisableSend || memDev.DisableRecv != dbDev.DisableRecv || memDev.Priority != dbDev.Priority {
			if memDev.DisableRecv != dbDev.DisableRecv {
				receiverRoutingChanged = true
			}
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
	if receiverRoutingChanged {
		InvalidateDomainReceiverCache()
	}

	missingDevices := make([]*models.Device, 0)
	for _, memDev := range getOwnerDeviceMapSnapshot() {
		if memDev == nil {
			continue
		}
		if _, exists := dbDeviceKeys[getOwnerSSIDKey(memDev.OwnerID, memDev.SSID)]; exists {
			continue
		}
		missingDevices = append(missingDevices, memDev)
	}
	for _, missingDev := range missingDevices {
		if RemoveRuntimeDevice(missingDev.OwnerID, missingDev.SSID) {
			removedCount++
		}
	}
	if removedCount > 0 {
		if deviceCache := cache.GetDeviceCache(); deviceCache != nil {
			ctx := context.Background()
			_ = deviceCache.InvalidateDeviceList(ctx)
			for _, missingDev := range missingDevices {
				_ = deviceCache.InvalidateDevice(ctx, missingDev.ID, missingDev.OwnerID, uint8(missingDev.SSID))
				if missingDev.GroupID > 0 {
					_ = deviceCache.InvalidateDevicesByGroup(ctx, missingDev.GroupID)
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
	if removedCount > 0 {
		log.Printf("[CACHE] 已清理 %d 个数据库中已不存在的运行时设备", removedCount)
	}
}

// RefreshDeviceCache 从数据库重新同步设备动态属性和运行时索引。用于数据库
// 已提交、但单设备增量同步失败时立即自愈，而不是等待后台轮询。
func RefreshDeviceCache() {
	refreshDeviceCache()
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
