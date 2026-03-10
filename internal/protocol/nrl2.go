package protocol

import (
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"time"

	"nrllink/internal/models"
)

var zero6 = make([]byte, 9)

// NewNRL2Packet 创建新的NRL2数据包
func NewNRL2Packet(remoteAddr *net.UDPAddr, data []byte) (*models.NRL2Packet, error) {
	packet := &models.NRL2Packet{
		UDPAddr:    remoteAddr,
		UDPAddrStr: remoteAddr.String(),
		TimeStamp:  time.Now(), // 设置接收时间戳
	}

	err := packet.Decode(data)
	if err != nil {
		return nil, err
	}

	return packet, nil
}

// Encode 编码NRL2数据包
func Encode(callsign string, ssid, packetType, devModel uint8, dmrid uint32, data []byte) []byte {
	const fixedBufferSize = 48
	totalSize := fixedBufferSize + len(data)
	packet := make([]byte, totalSize)

	// 写入固定头部
	copy(packet[0:4], []byte("NRL2"))

	// 写入长度
	binary.BigEndian.PutUint16(packet[4:6], uint16(totalSize))

	// 写入 DMRID
	SetDevDMRID(dmrid, packet)

	// 写入 Type
	packet[20] = packetType

	// 写入 Status
	packet[21] = 1

	// 写入 CallSign
	copy(packet[24:30], callsign)

	// 写入 SSID
	packet[30] = ssid

	// 写入 DevMode
	packet[31] = devModel

	// 写入 DATA
	if len(data) > 0 {
		copy(packet[48:], data)
	}

	return packet
}

// SetDevDMRID 设置设备DMRID
func SetDevDMRID(dmrid uint32, packet []byte) {
	packet[6] = byte(dmrid >> 16)
	packet[7] = byte(dmrid >> 8)
	packet[8] = byte(dmrid)
}

// SetCallsignSSID 设置呼号和SSID
func SetCallsignSSID(callsign string, ssid byte, packet []byte) []byte {
	copy(packet[24:30], zero6)
	copy(packet[24:30], callsign)
	packet[30] = ssid
	return packet
}

// Replace200and255Dev 替换200/255设备信息
func Replace200and255Dev(callsign string, ssid, packetType, devModel uint8, originalCallsign string, originalSSID uint8, originalIP net.IP, dmrid uint32, data []byte) []byte {
	packet := make([]byte, len(data))
	copy(packet, data)

	// 写入 DMRID
	SetDevDMRID(dmrid, packet)

	// 写入 Type
	packet[20] = packetType

	// 写入 CallSign
	copy(packet[24:30], callsign)
	if len(callsign) == 5 {
		packet[29] = 0
	}

	// 写入 SSID
	packet[30] = ssid

	// 写入 DevMode
	packet[31] = devModel

	// 协议原始呼号
	copy(packet[32:38], originalCallsign)
	if len(originalCallsign) == 5 {
		packet[37] = 0
	}

	// 写入原始SSID
	packet[38] = originalSSID

	// 写入 IP 地址
	copy(packet[39:43], originalIP)

	return packet
}

// GetCallSignSSID 获取组合呼号SSID
func GetCallSignSSID(callsign string, ssid byte) string {
	var builder strings.Builder
	builder.WriteString(callsign)
	builder.WriteString("-")
	builder.WriteString(strconv.Itoa(int(ssid)))
	return builder.String()
}
