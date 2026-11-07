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
	if len(data) > protocol.DraARLv1MaxPacketSize {
		return nil, fmt.Errorf("packet too large: maximum %d bytes", protocol.DraARLv1MaxPacketSize)
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
	// 【长度校验】Length 必须与报文实际长度一致，防止声明长度与内容不符
	if int(packet.Length) != len(data) {
		return nil, fmt.Errorf("invalid packet length: header=%d actual=%d", packet.Length, len(data))
	}

	// 解析 Username (6-37)
	packet.Username = string(bytes.TrimRight(data[6:38], "\x00"))

	// 解析 DevicePassword (38-47)
	packet.DevicePassword = string(bytes.TrimRight(data[38:48], "\x00"))

	// 解析 Type (48)
	packet.Type = data[48]
	if !protocol.IsSupportedDraARLType(packet.Type) {
		return nil, fmt.Errorf("unsupported packet type: %d", packet.Type)
	}

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
	if packet == nil {
		return nil
	}
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
	if req == nil {
		return nil
	}
	return protocol.EncodeHeartbeatResponse(&protocol.DraARLv1Packet{
		Username: req.Username, DevicePassword: req.DevicePassword,
		Type: req.Type, DevModel: req.DevModel, SSID: req.SSID, DMRID: req.DMRID, DATA: req.DATA,
	}, callsign)
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
	b[0] = byte((v >> 16) & 0xff)
	b[1] = byte((v >> 8) & 0xff)
	b[2] = byte(v & 0xff)
}

// GetPacketTypeName 获取数据包类型名称
func GetPacketTypeName(packetType byte) string {
	switch packetType {
	case protocol.DraARLTypeJWTAuth:
		return "JWTAuth"
	case protocol.DraARLTypeHeartbeat:
		return "Heartbeat"
	case protocol.DraARLTypeConfig:
		return "Config"
	case protocol.DraARLTypeTextMessage:
		return "TextMessage"
	case protocol.DraARLTypeOpus16K:
		return "Voice"
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
