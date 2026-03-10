package aprs

import (
	"fmt"
	"log"
	"strings"
	"time"

	"nrllink/internal/config"
	"nrllink/internal/udphub"
)

var (
	// APRSClient APRS 客户端
	APRSClient *APRS
)

// APRS APRS 客户端
type APRS struct {
	Status    string
	tcpClient *TCPClient
	errorChan chan error
}

// TCPClient TCP 客户端接口
type TCPClient struct {
	host string
	port string
}

// NewTCPClient 创建 TCP 客户端
func NewTCPClient(host, port string, handler func([]byte)) *TCPClient {
	return &TCPClient{
		host: host,
		port: port,
	}
}

// Connect 连接到服务器
func (c *TCPClient) Connect() error {
	log.Printf("TCP client connecting to %s:%s", c.host, c.port)
	// TODO: 实现实际的 TCP 连接
	return nil
}

// Close 关闭连接
func (c *TCPClient) Close() error {
	return nil
}

// Send 发送数据
func (c *TCPClient) Send(data string) error {
	log.Printf("TCP send: %s", data)
	// TODO: 实现实际的 TCP 发送
	return nil
}

// NewAPRS 创建 APRS 客户端
func NewAPRS() *APRS {
	return &APRS{}
}

// Start 启动 APRS 服务
func (a *APRS) Start(cfg *config.Configuration) {
	if cfg.APRS.APRSServerHost == "" || cfg.APRS.APRSServerPort == "" ||
		cfg.APRS.Longitude == 0 || cfg.APRS.CallSign == "" {
		log.Println("APRS: 启动失败，请检查 APRS 配置")
		return
	}

	a.errorChan = make(chan error, 1)

	for {
		a.tcpClient = NewTCPClient(cfg.APRS.APRSServerHost, cfg.APRS.APRSServerPort, a.handleTCPMessage)
		a.tcpClient.Connect()

		a.Login(cfg)

		time.Sleep(5 * time.Second)

		// 启动定时发送
		a.startLocationWatch(cfg)

		err := <-a.errorChan

		a.tcpClient.Close()

		time.Sleep(5 * time.Second)
		log.Printf("APRS 发送错误，重新初始化 TCP 连接: %v", err)
	}
}

// Stop 停止 APRS 服务
func (a *APRS) Stop() {
	if a.tcpClient != nil {
		a.tcpClient.Close()
	}
}

// startLocationWatch 启动位置定时发送
func (a *APRS) startLocationWatch(cfg *config.Configuration) {
	a.sendAPRSPosition(cfg)

	time.Sleep(time.Minute)

	a.sendAPRSPosition(cfg)

	time.Sleep(5 * time.Minute)

	a.sendAPRSPosition(cfg)

	// 启动定时发送（每 10 分钟一次）
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			if err := a.sendAPRSPosition(cfg); err != nil {
				a.errorChan <- fmt.Errorf("发送 APRS 位置失败: %w", err)
				log.Printf("发送 APRS 位置失败: %v", err)
				return
			}
		}
	}()
}

// Login 登录 APRS 服务器
func (a *APRS) Login(cfg *config.Configuration) {
	passcode := cfg.APRS.Passcode

	if cfg.APRS.Passcode == 0 {
		passcode = GenerateAPRSPasscode(cfg.APRS.CallSign)
	}

	for {
		err := a.tcpClient.Send(fmt.Sprintf("user %s pass %d vers NRL 1.0\n", cfg.APRS.CallSign, passcode))

		if err != nil {
			log.Printf("APRS: 认证失败: %v", err)
			time.Sleep(10 * time.Second)
			continue
		} else {
			log.Println("APRS: 认证成功")
			return
		}
	}
}

// sendAPRSPosition 发送 APRS 位置信息
func (a *APRS) sendAPRSPosition(cfg *config.Configuration) error {
	// 构造 APRS 数据包
	aprsPacket := a.formatAPRSPacket(cfg.APRS.CallSign, cfg.APRS.SSID, cfg.APRS.SelfAddress, cfg.APRS.SelfPort,
		cfg.APRS.Longitude, cfg.APRS.Latitude, cfg.APRS.Altitude)

	// 发送数据
	err := a.tcpClient.Send(aprsPacket)
	if err != nil {
		log.Printf("APRS: 发送 APRS 位置失败: %v", err)
		a.Status = "发送失败"
		return err
	} else {
		a.Status = "位置已发送"
	}

	// 发送附加信息
	stats := udphub.GetTotalStats()
	aprsPacket2 := a.formatAPRSPacketTwo("NRLLink", cfg.APRS.CallSign, cfg.APRS.SSID,
		stats.OnlineDevNumber, udphub.GetDeviceCount())

	err = a.tcpClient.Send(aprsPacket2)
	if err != nil {
		log.Printf("APRS: 发送附加信息失败: %v", err)
		a.Status = "发送失败"
		return err
	} else {
		a.Status = "位置已发送"
	}

	return nil
}

// formatAPRSPacket 格式化 APRS 位置数据包
func (a *APRS) formatAPRSPacket(callSign, ssid, address, port string, lat, lon float64, altitude string) string {
	latStr := a.decToAPRS(lat, true)
	lonStr := a.decToAPRS(lon, false)

	return fmt.Sprintf("%s-%s>NRLSRV,TCPIP*:!%s/%sI @udp://%s:%s,NRL互联服务器\n",
		callSign, ssid, latStr, lonStr, address, port)
}

// formatAPRSPacketTwo 格式化 APRS 附加信息数据包
func (a *APRS) formatAPRSPacketTwo(name, callSign, ssid string, onlineNumber, total int) string {
	return fmt.Sprintf("%s-%s>NRLSRV,TCPIP*:>在线:%d,高峰:%d,%s\n",
		callSign, ssid, onlineNumber, total, name)
}

// handleTCPMessage 处理 TCP 消息
func (a *APRS) handleTCPMessage(message []byte) {
	a.Status = "收到服务器响应"

	// 2 秒后清除状态
	time.AfterFunc(2*time.Second, func() {
		a.Status = ""
	})
}

// decToAPRS 将十进制坐标转换为 APRS 格式
func (a *APRS) decToAPRS(dec float64, isLat bool) string {
	dir := ""
	if dec >= 0 {
		if isLat {
			dir = "N"
		} else {
			dir = "E"
		}
	} else {
		if isLat {
			dir = "S"
		} else {
			dir = "W"
		}
	}

	dec = abs(dec)
	deg := int(dec)
	min := (dec - float64(deg)) * 60

	return fmt.Sprintf("%02d%05.2f%s", deg, min, dir)
}

// abs 返回浮点数的绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// GenerateAPRSPasscode 生成 APRS 密码
func GenerateAPRSPasscode(callsign string) int {
	parts := strings.Split(callsign, "-")
	callsign = strings.ToUpper(parts[0])

	passcode := 29666
	i := 0
	for i < len(callsign) {
		passcode ^= int(callsign[i]) * 256
		if i+1 < len(callsign) {
			passcode ^= int(callsign[i+1])
		}
		i += 2
	}
	passcode &= 32767
	return passcode
}

// StartAPRSService 启动 APRS 服务
func StartAPRSService(cfg *config.Configuration) {
	if cfg.APRS.CallSign == "" {
		log.Println("APRS not configured, skipping")
		return
	}

	APRSClient = NewAPRS()
	go APRSClient.Start(cfg)
}

// StopAPRSService 停止 APRS 服务
func StopAPRSService() {
	if APRSClient != nil {
		APRSClient.Stop()
	}
}

// GetAPRSStatus 获取 APRS 状态
func GetAPRSStatus() string {
	if APRSClient != nil {
		return APRSClient.Status
	}
	return "未启动"
}
