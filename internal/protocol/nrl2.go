package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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
		TimeStamp:  time.Now(),
	}

	err := packet.Decode(data)
	if err != nil {
		return nil, err
	}

	return packet, nil
}

// Decode 解码NRL2数据包
func (p *models.NRL2Packet) Decode(data []byte) error {
	if len(data) < 48 {
		return errors.New("packet too short")
	}

	p.Version = string(data[0:4])
	if p.Version != "NRL2" {
		return errors.New("not NRL packet")
	}

	p.Length = binary.BigEndian.Uint16(data[4:6])
	p.DMRID = bytesToUint24(data[6:9])
	p.Password = string(data[9:20])
	p.Type = data[20]
	p.Status = data[21]
	p.Count = binary.BigEndian.Uint16(data[22:24])
	p.CallSign = string(bytes.TrimRight(data[24:30], string([]byte{13, 0})))

	if !IsCallSign(p.CallSign) {
		return errors.New("callsign error")
	}

	p.SSID = data[30]
	p.DevModel = data[31]

	if p.Type == models.TypeServerVoice || p.DevModel == models.DevModelServer || p.DevModel == models.DevModelFullNet {
		p.OriginalCallsign = string(bytes.TrimRight(data[32:38], string([]byte{13, 0})))
		p.OriginalSSID = data[38]
		p.OriginalIP = data[39:43]
	}

	p.DATA = data[48:]

	return nil
}

// String 返回数据包的字符串表示
func (p *models.NRL2Packet) String() string {
	return fmt.Sprintf("ver:%v len:%v DMRID:%v CallSign:%v-%v type:%v len:%v Count:%v %02X",
		p.Version, p.Length, p.DMRID, p.CallSign, p.SSID, p.Type, len(p.DATA), p.Count, p.DATA)
}

// bytesToUint24 将3字节转换为uint32
func bytesToUint24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// IsCallSign 验证呼号格式
func IsCallSign(callsign string) bool {
	if len(callsign) < 3 || len(callsign) > 6 {
		return false
	}
	for _, c := range callsign {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
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
