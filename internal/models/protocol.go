package models

import (
	"fmt"
	"time"
)

// DeviceModel 设备型号
const (
	DevModelUnknown      byte = 0
	DevModelBoxV1RF      byte = 1 // BoxV1-RF（内置 RF）
	DevModelBoxV1EX      byte = 2 // BoxV1-EX（RF 系统外置）
	DevModelBox32ANA     byte = 3 // Box32-ANA（模拟音频）
	DevModelBox32DIG     byte = 4 // Box32-DIG（MMDVM 数字）
	DevModelBox32MIX     byte = 5 // Box32-MIX（模拟音频与 MMDVM）
	DevModelWeChatMini   byte = 100
	DevModelAndroid      byte = 101
	DevModelIOS          byte = 102
	DevModelWindows      byte = 103
	DevModelBrowser      byte = 105
	DevModelLegacyBridge byte = 106
	DevModelLegacyESP32  byte = 107
	DevModelNSBridge     byte = 236
	DevModelTTBridge     byte = 237
	DevModelHTBridge     byte = 238
	DevModelNRL2Bridge   byte = 239
	DevModelBM           byte = 201
	DevModelNanny        byte = 250
	DevModelFullNet      byte = 255
)

const (
	// Legacy names retain the existing wire values for downstream callers.
	DevModelESP32Radio   = DevModelBoxV1RF
	DevModelESP32NoRadio = DevModelBoxV1EX
	DevModelRescue       = DevModelLegacyBridge
	DevModelESP32        = DevModelLegacyESP32
)

// SSIDRange SSID 范围定义
const (
	SSIDReserved         byte = 0
	SSIDNormalMin1       byte = 1
	SSIDNormalMax1       byte = 99
	SSIDGhostReservedMin byte = 100
	SSIDGhostReservedMax byte = 105
	SSIDNormalMin2       byte = 106
	SSIDNormalMax2       byte = 254
	SSIDInterconnectMin  byte = 255
	SSIDInterconnectMax  byte = 255
	SSIDHardwareMin           = SSIDNormalMin1
	SSIDHardwareMax           = SSIDNormalMax2
	SSIDSoftwareMin           = SSIDGhostReservedMin
	SSIDSoftwareMax           = SSIDGhostReservedMax
	SSIDServerMin             = SSIDInterconnectMin
	SSIDServerMax             = SSIDInterconnectMax
)

// GroupType 群组类型
const (
	GroupTypeNormal   int = 0 // 普通群组（历史兼容）
	GroupTypeRelay    int = 1 // 公开群组（历史命名）
	GroupTypeReserved int = 2 // 私有群组（历史命名）
)

// GroupID 预定义群组ID
const (
	GroupIDDefault   int = 0   // 默认测试组
	GroupIDPrivate1  int = 1   // 私有群组1
	GroupIDPrivate2  int = 2   // 私有群组2
	GroupIDPrivate3  int = 3   // 私有群组3
	GroupIDPublicMin int = 999 // 公共群组起始
)

// DeviceStatus 设备状态位
const (
	DevStatusTxDisable byte = 1 << 0 // 禁止发射
	DevStatusRxDisable byte = 1 << 1 // 禁止接收
	DevStatusNoRelay   byte = 1 << 2 // 不参与转发
)

// ServerStats 服务器统计信息
type ServerStats struct {
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

// Relay 中继台信息
type Relay struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	UpFreq       string `json:"up_freq"`
	DownFreq     string `json:"down_freq"`
	SendCTSS     string `json:"send_ctss"`
	ReceiveCTSS  string `json:"recive_ctss"`
	OwerCallSign string `json:"ower_callsign"`
	CreateTime   string `json:"create_time"`
	UpdateTime   string `json:"update_time"`
	Status       int    `json:"status"`
	Note         string `json:"note"`
}

// String 返回中继台的字符串表示
func (r *Relay) String() string {
	return fmt.Sprintf("中继台[%s] 上行:%s 下行:%s 所有者:%s", r.Name, r.UpFreq, r.DownFreq, r.OwerCallSign)
}

// String 返回服务器的字符串表示
