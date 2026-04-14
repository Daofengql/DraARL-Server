package websocket

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"draarl/internal/protocol"
)

// WSPacket WebSocket 数据包（基于 DraARLv1 协议）
type WSPacket struct {
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

	// Metadata
	Timestamp time.Time
}

// DecodeWSPacket 解码 WebSocket 二进制数据为 WSPacket
func DecodeWSPacket(data []byte) (*WSPacket, error) {
	if len(data) < protocol.DraARLv1HeaderSize {
		return nil, errors.New("packet too short, minimum 90 bytes required")
	}

	packet := &WSPacket{
		Timestamp: time.Now(),
	}

	// 解析 Version (0-3)
	packet.Version = string(data[0:4])
	if packet.Version != protocol.DraARLVersion {
		return nil, fmt.Errorf("invalid protocol version: expected %s, got %s", protocol.DraARLVersion, packet.Version)
	}

	// 解析 Length (4-5)
	packet.Length = binary.BigEndian.Uint16(data[4:6])

	// 解析 Username (6-37)
	packet.Username = string(bytes.TrimRight(data[6:38], "\x00"))

	// 解析 DevicePassword (38-47)
	packet.DevicePassword = string(bytes.TrimRight(data[38:48], "\x00"))

	// 解析 Type (48)
	packet.Type = data[48]

	// 解析 DevModel (49)
	packet.DevModel = data[49]

	// 解析 SSID (50)
	packet.SSID = data[50]

	// 解析 DMRID (51-53) - uint24 big-endian
	packet.DMRID = bytesToUint24(data[51:54])

	// 解析 CallSign (54-85)
	packet.CallSign = string(bytes.TrimRight(data[54:86], "\x00"))

	// 解析 Reserved (86-89)
	packet.Reserved = data[86:90]

	// 解析 DATA (90+)
	if len(data) > protocol.DraARLv1HeaderSize {
		packet.DATA = data[protocol.DraARLv1HeaderSize:]
	}

	return packet, nil
}

// EncodeWSPacket 编码 WSPacket 为 WebSocket 二进制数据
func EncodeWSPacket(packet *WSPacket) []byte {
	return protocol.EncodeDraARLv1(
		packet.Username,
		packet.DevicePassword,
		packet.SSID,
		packet.Type,
		packet.DevModel,
		packet.DMRID,
		packet.CallSign,
		packet.DATA,
	)
}

// EncodeHeartbeatResponse 编码心跳响应包（填充 CallSign）
func EncodeHeartbeatResponse(req *WSPacket, callsign string) []byte {
	totalSize := protocol.DraARLv1HeaderSize + len(req.DATA)
	packet := make([]byte, totalSize)

	// 复制原始请求的大部分字段
	copy(packet[0:4], []byte(protocol.DraARLVersion))
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
		copy(packet[protocol.DraARLv1HeaderSize:], req.DATA)
	}

	return packet
}

// BuildHeartbeatPacket 构建心跳包
func BuildHeartbeatPacket(username string, ssid byte, devModel byte, gpsData []byte) []byte {
	return protocol.EncodeDraARLv1(
		username,
		"", // 心跳包不需要密码
		ssid,
		protocol.DraARLTypeHeartbeat,
		devModel,
		0,  // DMRID
		"", // CallSign 由服务器填充
		gpsData,
	)
}

// BuildVoicePacket 构建语音包
func BuildVoicePacket(username string, ssid byte, devModel byte, opusData []byte) []byte {
	return protocol.EncodeDraARLv1(
		username,
		"", // 语音包使用已认证的连接
		ssid,
		protocol.DraARLTypeOpus16K,
		devModel,
		0,
		"",
		opusData,
	)
}

// BuildTextMessagePacket 构建文本消息包
func BuildTextMessagePacket(username string, ssid byte, devModel byte, message string) []byte {
	return protocol.EncodeDraARLv1(
		username,
		"",
		ssid,
		protocol.DraARLTypeTextMessage,
		devModel,
		0,
		"",
		[]byte(message),
	)
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

// GetPacketTypeName 获取数据包类型名称
func GetPacketTypeName(packetType byte) string {
	switch packetType {
	case protocol.DraARLTypeControl:
		return "Control"
	case protocol.DraARLTypeHeartbeat:
		return "Heartbeat"
	case protocol.DraARLTypeConfig:
		return "Config"
	case protocol.DraARLTypeTextMessage:
		return "TextMessage"
	case protocol.DraARLTypeOpus16K:
		return "Voice"
	case protocol.DraARLTypeServerVoice:
		return "ServerVoice"
	case protocol.DraARLTypeATPassThrough:
		return "ATPassThrough"
	default:
		return fmt.Sprintf("Unknown(%d)", packetType)
	}
}

// GetDeviceModelName 获取设备型号名称
func GetDeviceModelName(devModel byte) string {
	switch devModel {
	case protocol.DraARLDevModelUnknown:
		return "Unknown"
	case protocol.DraARLDevModelWeChatMini:
		return "WeChatMini"
	case protocol.DraARLDevModelAndroid:
		return "Android"
	case protocol.DraARLDevModelIOS:
		return "iOS"
	case protocol.DraARLDevModelWindows:
		return "Windows"
	case protocol.DraARLDevModelBrowser:
		return "Browser"
	case protocol.DraARLDevModelInterconnect:
		return "Interconnect"
	case protocol.DraARLDevModelESP32:
		return "ESP32"
	case protocol.DraARLDevModelNSBridge:
		return "Nanshan Soft Bridge"
	case protocol.DraARLDevModelHTBridge:
		return "HT Soft Bridge"
	case protocol.DraARLDevModelTTBridge:
		return "Taotao Soft Bridge"
	case protocol.DraARLDevModelNRL2Bridge:
		return "NRL2 Soft Bridge"
	default:
		return fmt.Sprintf("Unknown(%d)", devModel)
	}
}

// String 返回数据包的字符串表示
func (p *WSPacket) String() string {
	return fmt.Sprintf("WSPacket[type=%s, model=%s, ssid=%d, user=%s, callsign=%s, data_len=%d]",
		GetPacketTypeName(p.Type),
		GetDeviceModelName(p.DevModel),
		p.SSID,
		p.Username,
		p.CallSign,
		len(p.DATA),
	)
}
