package aprs

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"nrllink/internal/gormdb"
	"nrllink/internal/udphub"
	"nrllink/pkg/tcp"
)

var (
	// APRSClient APRS 客户端
	APRSClient *APRS

	// APRSLogBuffer APRS 日志缓冲
	APRSLogBuffer []APRSLogEntry
	APRSLogMutex sync.RWMutex
	APRSLogChan  chan string
)

// APRSLogEntry APRS 日志条目
type APRSLogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// init 初始化日志通道
func init() {
	APRSLogChan = make(chan string, 100)
	// 启动日志收集器
	go collectLogs()
}

// collectLogs 收集日志到缓冲区
func collectLogs() {
	const maxLogs = 500 // 最多保留500条日志
	for {
		msg := <-APRSLogChan
		if msg == "" {
			continue
		}

		APRSLogMutex.Lock()
		entry := APRSLogEntry{
			Timestamp: time.Now().Format("2006-01-02 15:04:05"),
			Message:   msg,
		}
		APRSLogBuffer = append(APRSLogBuffer, entry)
		// 限制日志数量
		if len(APRSLogBuffer) > maxLogs {
			APRSLogBuffer = APRSLogBuffer[len(APRSLogBuffer)-maxLogs:]
		}
		APRSLogMutex.Unlock()
	}
}

// aprsLog APRS 专用日志函数
func aprsLog(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Println("[APRS]", msg)
	APRSLogChan <- msg
}

// APRS APRS 客户端
type APRS struct {
	Status    string
	tcpClient *tcp.Client
	errorChan chan error
	mu        sync.RWMutex
}

// APRSTVResponse APRS.TV API 响应
type APRSTVResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		Scall  string   `json:"scall"`
		Call   string   `json:"call"`
		Ssid   string   `json:"ssid"`
		Tm     string   `json:"tm"`
		Lat    string   `json:"lat"`
		Lon    string   `json:"lon"`
		Alt    float64  `json:"alt"`
		Stable string   `json:"stable"`
		Symbol string   `json:"symbol"`
		Rotate int      `json:"rotate"`
		Fmt    string   `json:"fmt"`
		Speed  float64  `json:"speed"`
		Course float64  `json:"course"`
		Power  *float64 `json:"power"`
		Gain   *float64 `json:"gain"`
		Msg    string   `json:"msg"`
		Path   string   `json:"path"`
		State  string   `json:"state"`
		Dev    *string  `json:"dev"`
	} `json:"data"`
}

// NRLStatsResponse NRL 统计响应
type NRLStatsResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		Type  string `json:"type"`
		Total int    `json:"total"`
	} `json:"data"`
}

// ApiServer API 服务器信息
type ApiServer struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port string `json:"port"`
	Ower string `json:"ower"`
}

// APRSConfig APRS 配置（从数据库加载）
type APRSConfig struct {
	APRSServerHost string
	APRSServerPort string
	SelfAddress    string
	SelfPort       string
	CallSign       string
	SSID           string
	Passcode       int
	Latitude       float64
	Longitude      float64
	Altitude       string
}

var currentAPRSConfig APRSConfig

// LoadAPRSConfig 从数据库加载 APRS 配置
func LoadAPRSConfig() error {
	repo := gormdb.GetSiteConfigRepo()
	config, err := repo.GetAPRSConfig()
	if err != nil {
		return err
	}

	currentAPRSConfig = APRSConfig{
		APRSServerHost: config.APRSServerHost,
		APRSServerPort: config.APRSServerPort,
		SelfAddress:    config.SelfAddress,
		SelfPort:       config.SelfPort,
		CallSign:       config.CallSign,
		SSID:           config.SSID,
		Passcode:       config.Passcode,
		Latitude:       config.Latitude,
		Longitude:      config.Longitude,
		Altitude:       config.Altitude,
	}
	return nil
}

// NewAPRS 创建 APRS 客户端
func NewAPRS() *APRS {
	return &APRS{}
}

// Start 启动 APRS 服务
func (a *APRS) Start() {
	if currentAPRSConfig.APRSServerHost == "" || currentAPRSConfig.APRSServerPort == "" ||
		currentAPRSConfig.Longitude == 0 || currentAPRSConfig.CallSign == "" {
		aprsLog("启动失败，请检查 APRS 配置")
		return
	}

	a.errorChan = make(chan error, 1)

	for {
		a.mu.Lock()
		a.tcpClient = tcp.NewClient(currentAPRSConfig.APRSServerHost, currentAPRSConfig.APRSServerPort, a.handleTCPMessage)
		a.mu.Unlock()

		if err := a.tcpClient.Connect(); err != nil {
			aprsLog("TCP 连接失败: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		a.Login()

		time.Sleep(5 * time.Second)

		// 启动定时发送
		a.startLocationWatch()

		err := <-a.errorChan

		a.mu.Lock()
		if a.tcpClient != nil {
			a.tcpClient.Close()
		}
		a.mu.Unlock()

		time.Sleep(5 * time.Second)
		aprsLog("发送错误，重新初始化 TCP 连接: %v", err)
	}
}

// Stop 停止 APRS 服务
func (a *APRS) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tcpClient != nil {
		a.tcpClient.Close()
	}
}

// startLocationWatch 启动位置定时��送
func (a *APRS) startLocationWatch() {
	a.sendAPRSPosition()

	time.Sleep(time.Minute)

	a.sendAPRSPosition()

	time.Sleep(5 * time.Minute)

	a.sendAPRSPosition()

	// 启动定时发送（每 10 分钟一次）
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			if err := a.sendAPRSPosition(); err != nil {
				a.errorChan <- fmt.Errorf("发送 APRS 位置失败: %w", err)
				aprsLog("定时发送 APRS 位置失败: %v", err)
				return
			}
		}
	}()
}

// Login 登录 APRS 服务器
func (a *APRS) Login() {
	passcode := currentAPRSConfig.Passcode

	if currentAPRSConfig.Passcode == 0 {
		passcode = GenerateAPRSPasscode(currentAPRSConfig.CallSign)
	}

	for {
		err := a.tcpClient.Send(fmt.Sprintf("user %s pass %d vers DraARL 1.0\n", currentAPRSConfig.CallSign, passcode))

		if err != nil {
			aprsLog("认证失败: %v", err)
			time.Sleep(10 * time.Second)
			continue
		} else {
			aprsLog("认证成功")
			return
		}
	}
}

// sendAPRSPosition 发送 APRS 位置信息
func (a *APRS) sendAPRSPosition() error {
	a.mu.RLock()
	client := a.tcpClient
	a.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("TCP client not connected")
	}

	// 构造 APRS 数据包
	aprsLog("原始坐标: 纬度=%.6f, 经度=%.6f", currentAPRSConfig.Latitude, currentAPRSConfig.Longitude)
	aprsPacket := a.formatAPRSPacket(currentAPRSConfig.CallSign, currentAPRSConfig.SSID, currentAPRSConfig.SelfAddress, currentAPRSConfig.SelfPort,
		currentAPRSConfig.Latitude, currentAPRSConfig.Longitude, currentAPRSConfig.Altitude)

	aprsLog("发送位置数据包: %s", strings.TrimSpace(aprsPacket))

	// 发送数据
	err := client.Send(aprsPacket)
	if err != nil {
		aprsLog("发送 APRS 位置失败: %v", err)
		a.Status = "发送失败"
		return err
	} else {
		a.Status = "位置已发送"
	}

	// 发送附加信息
	stats := udphub.GetTotalStats()
	aprsPacket2 := a.formatAPRSPacketTwo("DraARL", currentAPRSConfig.CallSign, currentAPRSConfig.SSID,
		stats.OnlineDevNumber, udphub.GetDeviceCount())

	aprsLog("发送统计信息包: %s", strings.TrimSpace(aprsPacket2))

	err = client.Send(aprsPacket2)
	if err != nil {
		aprsLog("发送附加信息失败: %v", err)
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

	// 正确处理呼号和SSID，避免空SSID时出现多余连字符
	fullCallSign := callSign
	if ssid != "" && ssid != "0" {
		fullCallSign = fmt.Sprintf("%s-%s", callSign, ssid)
	}

	return fmt.Sprintf("%s>DARLSV,TCPIP*:!%s/%sI @udp://%s:%s,DraARL互联服务器\r\n",
		fullCallSign, latStr, lonStr, address, port)
}

// formatAPRSPacketTwo 格式化 APRS 附加信息数据包
func (a *APRS) formatAPRSPacketTwo(name, callSign, ssid string, onlineNumber, total int) string {
	// 正确处理呼号和SSID，避免空SSID时出现多余连字符
	fullCallSign := callSign
	if ssid != "" && ssid != "0" {
		fullCallSign = fmt.Sprintf("%s-%s", callSign, ssid)
	}

	return fmt.Sprintf("%s>DARLSV,TCPIP*:>在线:%d,高峰:%d,%s\r\n",
		fullCallSign, onlineNumber, total, name)
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

	// 纬度使用2位整数，经度使用3位整数（AX.25协议标准）
	if isLat {
		return fmt.Sprintf("%02d%05.2f%s", deg, min, dir)
	}
	return fmt.Sprintf("%03d%05.2f%s", deg, min, dir)
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
func StartAPRSService() {
	// 从数据库加载 APRS 配置
	if err := LoadAPRSConfig(); err != nil {
		aprsLog("加载配置失败: %v", err)
		return
	}

	if currentAPRSConfig.CallSign == "" {
		aprsLog("未配置呼号，跳过启动")
		return
	}

	APRSClient = NewAPRS()
	go APRSClient.Start()

	// 启动 APRS.TV 平台发现
	go startAPRSTVService()
}

// StopAPRSService 停止 APRS 服务
func StopAPRSService() {
	if APRSClient != nil {
		APRSClient.Stop()
	}
}

// RestartAPRSService 重启 APRS 服务
func RestartAPRSService() {
	// 停���现有服务
	StopAPRSService()

	// 重新加载配置
	if err := LoadAPRSConfig(); err != nil {
		aprsLog("加载配置失败: %v", err)
		return
	}

	if currentAPRSConfig.CallSign == "" {
		aprsLog("未配置呼号，跳过启动")
		return
	}

	// 启动新服务
	APRSClient = NewAPRS()
	go APRSClient.Start()
	aprsLog("服务已重启")
}

// GetAPRSStatus 获取 APRS 状态
func GetAPRSStatus() string {
	if APRSClient != nil {
		return APRSClient.Status
	}
	return "未启动"
}

// startAPRSTVService 启动 APRS.TV 平台发现服务
func startAPRSTVService() {
	if currentAPRSConfig.CallSign == "" {
		return
	}

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		getNRLFromAPRSTV()
	}
}

// getNRLFromAPRSTV 从 APRS.TV 获取 NRL 服务器列表
func getNRLFromAPRSTV() {
	apiURL := "https://aprs.tv/api/findnrl"

	// 构造查询参数
	params := url.Values{}
	params.Add("dest", "DARLSV")
	params.Add("duration", "60")

	// 拼接完整 URL
	uri, err := url.Parse(apiURL)
	if err != nil {
		log.Println("Error parsing URL:", err)
		return
	}
	uri.RawQuery = params.Encode()

	// 发起 POST 请求（无 body）
	resp, err := http.Post(uri.String(), "", nil)
	if err != nil {
		log.Println("APRS.TV request failed:", err)
		return
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return
	}

	// 解析 JSON 响应
	var apiResponse APRSTVResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		log.Println("Error unmarshaling JSON response:", err)
		return
	}

	// 处理响应数据
	for _, item := range apiResponse.Data {
		host, port, err := decodeMsgFromAPRS(item.Msg)
		if err != nil {
			continue
		}

		online, total, name, err := decodeStateFromAPRS(item.State)
		if err != nil {
			continue
		}

		// ���滤自身
		if host == currentAPRSConfig.SelfAddress {
			continue
		}

		aprsLog("APRS.TV: 发现服务器 %s at %s:%s (在线:%d, 总数:%d)",
			name, host, port, online, total)
	}
}

// decodeMsgFromAPRS 从 APRS 消息解码地址
func decodeMsgFromAPRS(str string) (host, port string, err error) {
	s1 := strings.Split(strings.TrimSpace(str)[1:], ",")

	parsedURL, err := url.Parse(s1[0])
	if err != nil {
		return "", "", err
	}

	host = parsedURL.Hostname()
	port = parsedURL.Port()

	return
}

// decodeStateFromAPRS 从 APRS 状态解码统计信息
func decodeStateFromAPRS(str string) (online, total int, name string, err error) {
	s1 := strings.Split(str, ",")

	if len(s1) != 3 {
		return 0, 0, "", fmt.Errorf("Error parsing state")
	}

	s2 := strings.Split(s1[0], ":")
	s3 := strings.Split(s1[1], ":")
	online, _ = strconv.Atoi(s2[1])
	total, _ = strconv.Atoi(s3[1])
	name = s1[2]

	return
}

// getNRLStatsFromAPRSTV 从 APRS.TV 获取 NRL 统计信息
func getNRLStatsFromAPRSTV() map[string]int {
	apiURL := "https://aprs.tv/api/findnrltotal"

	// 构造查询参数
	params := url.Values{}
	params.Add("duration", "60")

	// 拼接完整 URL
	uri, err := url.Parse(apiURL)
	if err != nil {
		log.Println("Error parsing URL:", err)
		return nil
	}
	uri.RawQuery = params.Encode()

	// 发起 POST 请求
	resp, err := http.Post(uri.String(), "", nil)
	if err != nil {
		log.Println("APRS.TV stats request failed:", err)
		return nil
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return nil
	}

	// 解析 JSON 响应
	var apiResponse NRLStatsResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		log.Println("Error unmarshaling JSON response:", err)
		return nil
	}

	stats := make(map[string]int)
	for _, item := range apiResponse.Data {
		switch item.Type {
		case "DARLSV":
			stats["servers"] = item.Total
		case "DraARLBOX":
			stats["boxes"] = item.Total
		case "DraARLAPP":
			stats["apps"] = item.Total
		case "DraARLMP":
			stats["mps"] = item.Total
		}
	}

	return stats
}

// GetAPRSLogs 获取 APRS 日志
func GetAPRSLogs() []APRSLogEntry {
	APRSLogMutex.RLock()
	defer APRSLogMutex.RUnlock()

	// 返回副本避免并发问题
	result := make([]APRSLogEntry, len(APRSLogBuffer))
	copy(result, APRSLogBuffer)
	return result
}
