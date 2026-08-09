package udphub

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
)

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
	ResetAcceptedVoiceActivity(time.Now())
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
