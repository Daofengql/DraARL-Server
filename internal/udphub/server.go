package udphub

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/models"
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
