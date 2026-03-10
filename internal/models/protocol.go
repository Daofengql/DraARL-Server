package models

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// NRL2Packet NRL2协议数据包
type NRL2Packet struct {
	TimeStamp       time.Time
	UDPAddrStr       string
	UDPAddr         *net.UDPAddr
	Version         string
	Length          uint16
	DMRID           uint32
	Password        string
	Type            byte
	Status          byte
	Count           uint16
	CallSign        string
	SSID            byte
	DevModel        byte

	OriginalCallsign string
	OriginalSSID     uint8
	OriginalIP       net.IP

	DATA            []byte
}

// PacketType 数据包类型
const (
	TypeControl         byte = 0
	TypeG711Voice      byte = 1
	TypeHeartbeat      byte = 2
	TypeConfig         byte = 3
	TypeReserved       byte = 4
	TypeTextMessage    byte = 5
	TypeDeviceControl  byte = 6
	TypeGroupCommand   byte = 7
	TypeOpus16K        byte = 8
	TypeServerVoice    byte = 9
	TypeReserved2      byte = 10
	TypeATPassThrough  byte = 11
)

// DeviceModel 设备型号
const (
	DevModelUnknown    byte = 0
	DevModelWeChatMini byte = 100
	DevModelAndroid    byte = 101
	DevModelIOS        byte = 102
	DevModelWindows    byte = 103
	DevModelBrowser    byte = 105
	DevModelRescue     byte = 106
	DevModelServer     byte = 200
	DevModelBM         byte = 201
	DevModelNanny      byte = 250
	DevModelFullNet    byte = 255
)

// SSIDRange SSID 范围定义
const (
	SSIDReserved     byte = 0
	SSIDHardwareMin  byte = 1
	SSIDHardwareMax  byte = 99
	SSIDSoftwareMin  byte = 100
	SSIDSoftwareMax  byte = 199
	SSIDServerMin    byte = 200
	SSIDServerMax    byte = 255
)

// GroupType 群组类型
const (
	GroupTypeNormal   int = 0  // 普通群组
	GroupTypeRelay    int = 1  // 中继互联
	GroupTypeReserved int = 2
	GroupTypeMeeting  int = 7  // 会议模式
)

// GroupID 预定义群组ID
const (
	GroupIDDefault    int = 0   // 默认测试组
	GroupIDPrivate1   int = 1   // 私有群组1
	GroupIDPrivate2   int = 2   // 私有群组2
	GroupIDPrivate3   int = 3   // 私有群组3
	GroupIDPublicMin  int = 999 // 公共群组起始
)

// DeviceStatus 设备状态位
const (
	DevStatusTxDisable byte = 1 << 0 // 禁止发射
	DevStatusRxDisable byte = 1 << 1 // 禁止接收
	DevStatusNoRelay   byte = 1 << 2 // 不参与转发
)

// TotalStats 统计信息
type TotalStats struct {
	PacketNumber    int64
	VoiceTime       int64
	Traffic         int64
	OnlineDevNumber int
}

// QTH QTH信息
type QTH struct {
	QTH          string    `json:"qth"`
	CallSignSSID string    `json:"callsign_ssid"`
	JoinTime     time.Time `json:"join_time"`
	Name         string    `json:"name"`
}

// Server 服务器信息
type Server struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Online      int       `json:"online"`
	Total       int       `json:"total"`
	UDPAddr     *net.UDPAddr `json:"-"`
	CreateTime  string    `json:"create_time"`
	UpdateTime  string    `json:"update_time"`
}

// Relay 中继台信息
type Relay struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	UpFreq        string `json:"up_freq"`
	DownFreq      string `json:"down_freq"`
	SendCTSS      string `json:"send_ctss"`
	ReceiveCTSS   string `json:"recive_ctss"`
	OwerCallSign  string `json:"ower_callsign"`
	CreateTime    string `json:"create_time"`
	UpdateTime    string `json:"update_time"`
	Status        int    `json:"status"`
	Note          string `json:"note"`
}

// Decode 解码 NRL2 报文
func (n *NRL2Packet) Decode(data []byte) error {
	if len(data) < 48 {
		return errors.New("packet too short")
	}

	n.Version = string(data[0:4])
	if n.Version != "NRL2" {
		return errors.New("not NRL packet")
	}

	n.Length = binary.BigEndian.Uint16(data[4:6])
	n.DMRID = bytesToUint24(data[6:9])
	n.Password = string(data[9:20])
	n.Type = data[20]
	n.Status = data[21]
	n.Count = binary.BigEndian.Uint16(data[22:24])
	n.CallSign = string(bytes.TrimRight(data[24:30], string([]byte{13, 0})))

	if !IsValidCallSign(n.CallSign) {
		return errors.New("callsign error")
	}

	n.SSID = data[30]
	n.DevModel = data[31]

	if n.Type == TypeServerVoice || n.DevModel == DevModelServer || n.DevModel == DevModelFullNet {
		n.OriginalCallsign = string(bytes.TrimRight(data[32:38], string([]byte{13, 0})))
		n.OriginalSSID = data[38]
		n.OriginalIP = data[39:43]
	}

	n.DATA = data[48:]

	return nil
}

// String 返回报文的字符串表示
func (n *NRL2Packet) String() string {
	return fmt.Sprintf("ver:%v len:%v DMRID:%v CallSign:%v-%v type:%v len:%v Count:%v %02X",
		n.Version, n.Length, n.DMRID, n.CallSign, n.SSID, n.Type, len(n.DATA), n.Count, n.DATA)
}

// bytesToUint24 将 3 字节转换为 uint32
func bytesToUint24(b []byte) uint32 {
	if len(b) < 3 {
		return 0
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// IsValidCallSign 验证呼号格式
func IsValidCallSign(s string) bool {
	if len(s) < 3 || len(s) > 6 {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
