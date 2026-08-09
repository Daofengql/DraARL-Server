package geoip

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/ipipdotnet/ipdb-go"
)

var (
	db     *ipdb.City
	dbOnce sync.Once
)

// Init 初始化 IP 地理位置数据库
func Init(ipdbPath string) error {
	var initErr error
	dbOnce.Do(func() {
		var err error
		db, err = ipdb.NewCity(ipdbPath)
		if err != nil {
			initErr = fmt.Errorf("failed to load IP database: %w", err)
			return
		}
	})
	return initErr
}

// GetQTH 获取 IP 地址的地理位置描述
func GetQTH(ipAddr string) string {
	if db == nil {
		return "未知"
	}

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return "无效IP"
	}

	// 检查是否为内网地址
	if isPrivateIP(ip) {
		return "内网"
	}

	// 查询 IP 归属地
	info, err := db.Find(ipAddr, "CN")
	if err != nil {
		return "火星"
	}

	// 拼接结果
	result := strings.Join(info, "")

	// 过滤纯真网络等无效结果
	if strings.Contains(result, "纯真网络") {
		return "火星"
	}

	// 清理结果中的特殊字符
	result = strings.Trim(strings.ReplaceAll(result, "–", ""), "-")
	if result == "" {
		return "火星"
	}

	return result
}

// GetInfo 获取完整的 IP 地理位置信息
func GetInfo(ipAddr string) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("IP database not initialized")
	}

	return db.Find(ipAddr, "CN")
}

// isPrivateIP 检查是否为私有 IP 地址
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
	}

	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

// IsIPv4 检查数据库是否支持 IPv4
func IsIPv4() bool {
	if db == nil {
		return false
	}
	return db.IsIPv4()
}

// IsIPv6 检查数据库是否支持 IPv6
func IsIPv6() bool {
	if db == nil {
		return false
	}
	return db.IsIPv6()
}

// BuildTime 获取数据库构建时间
func BuildTime() string {
	if db == nil {
		return ""
	}
	return db.BuildTime().Format("2006-01-02 15:04:05")
}

// Languages 获取数据库支持的语言
func Languages() []string {
	if db == nil {
		return nil
	}
	return db.Languages()
}

// Fields 获取数据库支持的字段
func Fields() []string {
	if db == nil {
		return nil
	}
	return db.Fields()
}
