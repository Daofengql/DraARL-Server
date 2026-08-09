package udphub

import (
	"bytes"
	"encoding/binary"
	"net"
)

// PROXY Protocol v2 常量
const (
	// PROXY Protocol v2 版本
	proxyProtocolVersion2 = 0x20

	// 命令类型
	proxyCommandLocal = 0x00 // LOCAL 命令（健康检查等）
	proxyCommandProxy = 0x01 // PROXY 命令（携带原始地址）

	// 地址族和协议
	afUnspec    = 0x00 // AF_UNSPEC
	afInet      = 0x10 // AF_INET (IPv4)
	afInet6     = 0x20 // AF_INET6 (IPv6)
	afUnix      = 0x30 // AF_UNIX
	protoUnspec = 0x00 // UNSPEC
	protoStream = 0x01 // TCP
	protoDgram  = 0x02 // UDP

	// 地址长度
	ipv4AddrLen = 12 // IPv4: 源IP(4) + 目的IP(4) + 源端口(2) + 目的端口(2)
	ipv6AddrLen = 36 // IPv6: 源IP(16) + 目的IP(16) + 源端口(2) + 目的端口(2)
)

var proxyProtocolV2Signature = [12]byte{
	0x0D, 0x0A, 0x0D, 0x0A,
	0x00, 0x0D, 0x0A, 0x51,
	0x55, 0x49, 0x54, 0x0A,
}

// ProxyProtocolInfo PROXY Protocol 解析结果
type ProxyProtocolInfo struct {
	SourceIP        net.IP
	SourcePort      uint16
	DestinationIP   net.IP
	DestinationPort uint16
	IsProxy         bool // true 表示是 PROXY 命令，false 表示 LOCAL 命令
}

// ParseProxyProtocolV2 解析 PROXY Protocol v2 头部
// 返回: 解析结果, 业务数据, 是否解析成功
// 如果数据不是 PROXY Protocol 格式，返回原始数据
func ParseProxyProtocolV2(data []byte) (*ProxyProtocolInfo, []byte, bool) {
	// 检查最小长度 (签名 12 字节 + 头部 4 字节 = 16 字节)
	if len(data) < 16 {
		return nil, data, false
	}

	// 检查签名
	if !bytes.Equal(data[0:12], proxyProtocolV2Signature[:]) {
		return nil, data, false
	}

	// 解析版本和命令 (第 13 字节)
	verCmd := data[12]
	version := verCmd & 0xF0
	command := verCmd & 0x0F

	// 只支持版本 2
	if version != proxyProtocolVersion2 {
		return nil, data, false
	}

	// 解析地址族和协议 (第 14 字节)
	afProto := data[13]
	af := afProto & 0xF0
	proto := afProto & 0x0F

	// 解析地址长度 (第 15-16 字节，大端)
	addrLen := binary.BigEndian.Uint16(data[14:16])

	// 检查完整数据包长度
	headerLen := 16 + int(addrLen)
	if len(data) < headerLen {
		return nil, data, false
	}

	info := &ProxyProtocolInfo{
		IsProxy: command == proxyCommandProxy,
	}

	// 如果是 LOCAL 命令，不携带地址信息
	if command == proxyCommandLocal {
		return info, data[headerLen:], true
	}

	// 解析地址信息
	addrData := data[16:headerLen]

	switch af {
	case afInet:
		// IPv4
		if proto == protoDgram && len(addrData) >= ipv4AddrLen {
			info.SourceIP = net.IP(addrData[0:4])
			info.DestinationIP = net.IP(addrData[4:8])
			info.SourcePort = binary.BigEndian.Uint16(addrData[8:10])
			info.DestinationPort = binary.BigEndian.Uint16(addrData[10:12])
		}

	case afInet6:
		// IPv6
		if proto == protoDgram && len(addrData) >= ipv6AddrLen {
			info.SourceIP = net.IP(addrData[0:16])
			info.DestinationIP = net.IP(addrData[16:32])
			info.SourcePort = binary.BigEndian.Uint16(addrData[32:34])
			info.DestinationPort = binary.BigEndian.Uint16(addrData[34:36])
		}

	case afUnspec:
		// 未指定地址族，不解析地址

	case afUnix:
		// Unix 域套接字，不支持
	}

	return info, data[headerLen:], true
}

// GetRealAddr 获取真实客户端地址
// 如果解析了 PROXY Protocol，返回真实地址；否则返回原始 UDP 地址
func GetRealAddr(remoteAddr *net.UDPAddr, proxyInfo *ProxyProtocolInfo) *net.UDPAddr {
	if proxyInfo == nil || !proxyInfo.IsProxy || proxyInfo.SourceIP == nil {
		return remoteAddr
	}

	return &net.UDPAddr{
		IP:   proxyInfo.SourceIP,
		Port: int(proxyInfo.SourcePort),
		Zone: "",
	}
}
