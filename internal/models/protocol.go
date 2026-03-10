package models

import (
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
