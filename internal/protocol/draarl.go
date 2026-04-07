package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DraARLv1 协议版本标识
const DraARLVersion = "DraA"

// 固定头部大小
const DraARLv1HeaderSize = 90

// 最大包体大小（含头部）
// 计算：90B 头部 + 3帧×(2B长度+~200B Opus) ≈ 700B，取整 800B
const DraARLv1MaxPacketSize = 800

// DraARLv1 数据包类型常量
const (
	DraARLTypeControl       byte = 0 // 控制指令
	DraARLTypeJWTAuth       byte = 1 // JWT 认证包
	DraARLTypeHeartbeat     byte = 2 // 心跳包
	DraARLTypeConfig        byte = 3 // 设备配置
	DraARLTypeTextMessage   byte = 4 // 文本消息
	DraARLTypeOpus16K       byte = 5 // Opus 16K 语音
	DraARLTypeServerVoice   byte = 6 // 服务器互联语音
	DraARLTypeATPassThrough byte = 7 // AT 透传
)

// DraARLv1 设备型号常量
const (
	DraARLDevModelUnknown      byte = 0   // 未知设备
	DraARLDevModelWeChatMini   byte = 100 // 微信小程序
	DraARLDevModelAndroid      byte = 101 // Android 客户端
	DraARLDevModelIOS          byte = 102 // iOS 客户端
	DraARLDevModelWindows      byte = 103 // Windows 客户端
	DraARLDevModelMacOS        byte = 104 // macOS 客户端 (预留)
	DraARLDevModelBrowser      byte = 105 // 浏览器客户端
	DraARLDevModelInterconnect byte = 106 // 互联设备
	DraARLDevModelESP32        byte = 107 // ESP32 链路台/手咪
	DraARLDevModelNSBridge     byte = 110 // 南山对讲桥接器
	DraARLDevModelHTBridge     byte = 111 // HT 对讲桥接器
	DraARLDevModelTTBridge     byte = 112 // 涛涛对讲桥接器
)

// ==========================================
// SSID 范围常量（双轨制认证）
// ==========================================

const (
	// 普通设备 SSID 范围（两段）
	SSIDRangeNormal1Min byte = 1   // 普通设备第一段最小 SSID
	SSIDRangeNormal1Max byte = 99  // 普通设备第一段最大 SSID
	SSIDRangeNormal2Min byte = 106 // 普通设备第二段最小 SSID
	SSIDRangeNormal2Max byte = 235 // 普通设备第二段最大 SSID

	// 幽灵设备保留 SSID 范围
	SSIDRangeGhostMin byte = 100 // 幽灵设备保留最小
	SSIDRangeGhostMax byte = 105 // 幽灵设备保留最大 (含 Web)

	// 服务器互联保留 SSID 范围
	SSIDRangeInterconnectMin byte = 236 // 服务器互联最小
	SSIDRangeInterconnectMax byte = 255 // 服务器互联最大

	// 幽灵设备 SSID（等于 DevModel）
	SSIDGhostAndroid byte = 101 // Android App
	SSIDGhostIOS     byte = 102 // iOS App
	SSIDGhostWindows byte = 103 // Windows PC
	SSIDGhostMacOS   byte = 104 // macOS (预留)
	SSIDGhostWeb     byte = 105 // Web 浏览器
)

// JWT 认证响应状态码
const (
	JWTAuthSuccess         byte = 0 // 认证成功
	JWTAuthInvalidToken    byte = 1 // Token 无效或过期
	JWTAuthUserNotFound    byte = 2 // 用户不存在
	JWTAuthUserDisabled    byte = 3 // 用户已禁用
	JWTAuthUserNotApproved byte = 4 // 用户未审核
	JWTAuthInvalidDevModel byte = 5 // 无效的设备型号 (非 101-104)
)

// DraARLv1Packet DraARLv1协议数据包
type DraARLv1Packet struct {
	TimeStamp  time.Time
	UDPAddrStr string
	UDPAddr    *net.UDPAddr

	// Header fields (90 bytes)
	Version        string // 4B  - "DraA"
	Length         uint16 // 2B  - 报文总长度
	Username       string // 32B - 用户名
	DevicePassword string // 10B - 设备准入密码
	Type           byte   // 1B  - 数据包类型
	DevModel       byte   // 1B  - 设备型号
	SSID           byte   // 1B  - 设备子号
	DMRID          uint32 // 3B  - DMR ID (uint24)
	CallSign       string // 32B - 呼号（服务器填充）
	Reserved       []byte // 4B  - 保留

	// DATA region
	DATA []byte

	// ServerVoice type specific fields (parsed from DATA)
	OriginalUsername string // 32B - 原始发送方用户名
	OriginalCallSign string // 32B - 原始发送方呼号
	OriginalIP       net.IP // 4B  - 原始服务器IP
	VoiceData        []byte // 实际语音数据
}

// NewDraARLv1Packet 创建新的 DraARLv1 数据包
func NewDraARLv1Packet(remoteAddr *net.UDPAddr, data []byte) (*DraARLv1Packet, error) {
	packet := &DraARLv1Packet{
		UDPAddr:    remoteAddr,
		UDPAddrStr: remoteAddr.String(),
		TimeStamp:  time.Now(),
	}

	err := packet.Decode(data)
	if err != nil {
		return nil, err
	}

	return packet, nil
}

// Decode 解码 DraARLv1 报文
func (p *DraARLv1Packet) Decode(data []byte) error {
	if len(data) < DraARLv1HeaderSize {
		return errors.New("packet too short, minimum 90 bytes required")
	}

	// 解析 Version (0-3)
	p.Version = string(data[0:4])
	if p.Version != DraARLVersion {
		return fmt.Errorf("invalid protocol version: expected %s, got %s", DraARLVersion, p.Version)
	}

	// 解析 Length (4-5)
	p.Length = binary.BigEndian.Uint16(data[4:6])

	// 解析 Username (6-37)
	p.Username = string(bytes.TrimRight(data[6:38], "\x00"))

	// 解析 DevicePassword (38-47)
	p.DevicePassword = string(bytes.TrimRight(data[38:48], "\x00"))

	// 解析 Type (48)
	p.Type = data[48]

	// 解析 DevModel (49)
	p.DevModel = data[49]

	// 解析 SSID (50)
	p.SSID = data[50]

	// 解析 DMRID (51-53) - uint24 big-endian
	p.DMRID = bytesToUint24(data[51:54])

	// 解析 CallSign (54-85)
	p.CallSign = string(bytes.TrimRight(data[54:86], "\x00"))

	// 解析 Reserved (86-89)
	p.Reserved = data[86:90]

	// 解析 DATA (90+)
	if len(data) > DraARLv1HeaderSize {
		p.DATA = data[DraARLv1HeaderSize:]

		// 如果是服务器互联语音类型，解析原始发送方信息
		if p.Type == DraARLTypeServerVoice && len(p.DATA) >= 68 {
			p.OriginalUsername = string(bytes.TrimRight(p.DATA[0:32], "\x00"))
			p.OriginalCallSign = string(bytes.TrimRight(p.DATA[32:64], "\x00"))
			p.OriginalIP = net.IP(p.DATA[64:68])
			p.VoiceData = p.DATA[68:]
		}
	} else {
		p.DATA = nil
	}

	return nil
}

// Encode 编码 DraARLv1 报文
func EncodeDraARLv1(username, devicePassword string, ssid, packetType, devModel byte, dmrid uint32, callsign string, data []byte) []byte {
	totalSize := DraARLv1HeaderSize + len(data)
	packet := make([]byte, totalSize)

	// 写入 Version (0-3)
	copy(packet[0:4], []byte(DraARLVersion))

	// 写入 Length (4-5)
	binary.BigEndian.PutUint16(packet[4:6], uint16(totalSize))

	// 写入 Username (6-37)
	usernameBytes := []byte(username)
	if len(usernameBytes) > 32 {
		usernameBytes = usernameBytes[:32]
	}
	copy(packet[6:38], usernameBytes)

	// 写入 DevicePassword (38-47)
	passwordBytes := []byte(devicePassword)
	if len(passwordBytes) > 10 {
		passwordBytes = passwordBytes[:10]
	}
	copy(packet[38:48], passwordBytes)

	// 写入 Type (48)
	packet[48] = packetType

	// 写入 DevModel (49)
	packet[49] = devModel

	// 写入 SSID (50)
	packet[50] = ssid

	// 写入 DMRID (51-53)
	uint24ToBytes(dmrid, packet[51:54])

	// 写入 CallSign (54-85)
	callsignBytes := []byte(callsign)
	if len(callsignBytes) > 32 {
		callsignBytes = callsignBytes[:32]
	}
	copy(packet[54:86], callsignBytes)

	// Reserved (86-89) - 已经是 0

	// 写入 DATA (90+)
	if len(data) > 0 {
		copy(packet[DraARLv1HeaderSize:], data)
	}

	return packet
}

// EncodeHeartbeatResponse 编码心跳响应包（填充 CallSign）
func EncodeHeartbeatResponse(req *DraARLv1Packet, callsign string) []byte {
	totalSize := DraARLv1HeaderSize + len(req.DATA)
	packet := make([]byte, totalSize)

	// 复制原始请求的大部分字段
	copy(packet[0:4], []byte(DraARLVersion))
	binary.BigEndian.PutUint16(packet[4:6], uint16(totalSize))

	// 复制 Username
	usernameBytes := []byte(req.Username)
	copy(packet[6:38], usernameBytes)

	// 复制 DevicePassword
	passwordBytes := []byte(req.DevicePassword)
	copy(packet[38:48], passwordBytes)

	// 复制 Type
	packet[48] = req.Type

	// 复制 DevModel
	packet[49] = req.DevModel

	// 复制 SSID
	packet[50] = req.SSID

	// 复制 DMRID
	uint24ToBytes(req.DMRID, packet[51:54])

	// 填充 CallSign（服务器填充）
	callsignBytes := []byte(callsign)
	if len(callsignBytes) > 32 {
		callsignBytes = callsignBytes[:32]
	}
	copy(packet[54:86], callsignBytes)

	// Reserved - 已经是 0

	// 复制 DATA
	if len(req.DATA) > 0 {
		copy(packet[DraARLv1HeaderSize:], req.DATA)
	}

	return packet
}

// EncodeServerVoice 编码服务器互联语音包
func EncodeServerVoice(username, callsign string, ssid, devModel byte, dmrid uint32,
	originalUsername, originalCallsign string, originalIP net.IP, voiceData []byte) []byte {
	// DATA 区域前 68 字节存储原始发送方信息
	data := make([]byte, 68+len(voiceData))

	// OriginalUsername (0-31)
	origUserBytes := []byte(originalUsername)
	if len(origUserBytes) > 32 {
		origUserBytes = origUserBytes[:32]
	}
	copy(data[0:32], origUserBytes)

	// OriginalCallSign (32-63)
	origCallBytes := []byte(originalCallsign)
	if len(origCallBytes) > 32 {
		origCallBytes = origCallBytes[:32]
	}
	copy(data[32:64], origCallBytes)

	// OriginalIP (64-67)
	copy(data[64:68], originalIP)

	// VoiceData (68+)
	copy(data[68:], voiceData)

	return EncodeDraARLv1(username, "", ssid, DraARLTypeServerVoice, devModel, dmrid, callsign, data)
}

// bytesToUint24 将 3 字节转换为 uint32 (big-endian)
func bytesToUint24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// uint24ToBytes 将 uint32 转换为 3 字节 (big-endian)
func uint24ToBytes(v uint32, b []byte) {
	if len(b) < 3 {
		return
	}
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

// String 返回报文的字符串表示
func (p *DraARLv1Packet) String() string {
	return fmt.Sprintf("DraARLv1[ver:%s len:%d user:%s type:%d model:%d ssid:%d dmrid:%d callsign:%s data_len:%d]",
		p.Version, p.Length, p.Username, p.Type, p.DevModel, p.SSID, p.DMRID, p.CallSign, len(p.DATA))
}

// GetUsernameSSID 获取组合 username-ssid
func GetUsernameSSID(username string, ssid byte) string {
	return username + "-" + strconv.Itoa(int(ssid))
}

// GetCallSignSSID 获取组合 callsign-ssid（向后兼容）
func GetCallSignSSID(callsign string, ssid byte) string {
	return callsign + "-" + strconv.Itoa(int(ssid))
}

// ParseUsernameSSID 解析 username-ssid
func ParseUsernameSSID(s string) (username string, ssid byte, err error) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			username = s[:i]
			var ssidInt int
			fmt.Sscanf(s[i+1:], "%d", &ssidInt)
			return username, byte(ssidInt), nil
		}
	}
	return "", 0, errors.New("invalid username-ssid format")
}

// IsValidDevicePassword 验证设备密码格式
// 仅允许大小写字母和数字，长度 6-10 位
func IsValidDevicePassword(password string) bool {
	length := len(password)
	if length < 6 || length > 10 {
		return false
	}
	for _, c := range password {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// MaskDevicePassword 脱敏显示设备密码
// 例如: "Abc12345" -> "A****5"
func MaskDevicePassword(password string) string {
	length := len(password)
	if length <= 2 {
		return "***"
	}
	return string(password[0]) + "****" + string(password[length-1])
}

// ==========================================
// 幽灵设备辅助函数
// ==========================================

// IsGhostDevModel 判断是否为 UDP 幽灵设备型号
// UDP 幽灵设备: 101 (Android), 102 (iOS), 103 (Windows), 104 (macOS)
// 注意: 105 (Web) 使用 WebSocket，不在此范围内
func IsGhostDevModel(devModel byte) bool {
	return devModel >= DraARLDevModelAndroid && devModel <= DraARLDevModelMacOS
}

// IsGhostDevModelOrWeb 判断是否为幽灵设备型号（包括 Web）
// 用于 JWT 认证的所有幽灵设备: 101-105
func IsGhostDevModelOrWeb(devModel byte) bool {
	return devModel >= DraARLDevModelAndroid && devModel <= DraARLDevModelBrowser
}

// GetGhostSSID 获取幽灵设备的 SSID (等于 DevModel)
// 如果不是有效的幽灵设备型号，返回 0
func GetGhostSSID(devModel byte) byte {
	if IsGhostDevModelOrWeb(devModel) {
		return devModel
	}
	return 0
}

// ==========================================
// SSID 验证函数
// ==========================================

// IsValidNormalSSID 检查是否为有效的普通设备 SSID
// 普通设备可用: 1-99 或 106-235
func IsValidNormalSSID(ssid byte) bool {
	return (ssid >= SSIDRangeNormal1Min && ssid <= SSIDRangeNormal1Max) ||
		(ssid >= SSIDRangeNormal2Min && ssid <= SSIDRangeNormal2Max)
}

// IsGhostSSID 检查是否为幽灵设备保留 SSID (100-105)
func IsGhostSSID(ssid byte) bool {
	return ssid >= SSIDRangeGhostMin && ssid <= SSIDRangeGhostMax
}

// IsInterconnectSSID 检查是否为服务器互联保留 SSID (236-255)
func IsInterconnectSSID(ssid byte) bool {
	return ssid >= SSIDRangeInterconnectMin && ssid <= SSIDRangeInterconnectMax
}

// IsReservedSSID 检查是否为保留 SSID (用户不可分配)
// 保留范围: 100-105 (幽灵设备) 和 236-255 (服务器互联)
func IsReservedSSID(ssid byte) bool {
	return IsGhostSSID(ssid) || IsInterconnectSSID(ssid)
}

// GetDevModelName 获取设备型号名称
func GetDevModelName(devModel byte) string {
	switch devModel {
	case DraARLDevModelUnknown:
		return "Unknown"
	case DraARLDevModelWeChatMini:
		return "WeChat Mini"
	case DraARLDevModelAndroid:
		return "Android"
	case DraARLDevModelIOS:
		return "iOS"
	case DraARLDevModelWindows:
		return "Windows"
	case DraARLDevModelMacOS:
		return "macOS"
	case DraARLDevModelBrowser:
		return "Web Browser"
	case DraARLDevModelInterconnect:
		return "Interconnect"
	case DraARLDevModelESP32:
		return "ESP32"
	case DraARLDevModelNSBridge:
		return "Nanshan Bridge"
	case DraARLDevModelHTBridge:
		return "HT Bridge"
	case DraARLDevModelTTBridge:
		return "Taotao Bridge"
	default:
		return fmt.Sprintf("Unknown(%d)", devModel)
	}
}
