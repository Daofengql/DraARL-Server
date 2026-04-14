package models

import (
	"net"
	"time"
)

// Device 设备信息
type Device struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	DMRID        uint32    `json:"dmrid"`
	SSID         byte      `json:"ssid"`
	OwnerID      int       `json:"owner_id"`       // 所有者用户ID (外键关联 users.id)
	CallSign     string    `json:"callsign"`       // 运行时字段：从用户缓存获取
	QTH          string    `json:"qth"`            // 位置信息 (原 gird 字段)
	LastOnlineIP string    `json:"last_online_ip"` // 最近一次上线的客户端 IP（与 QTH 并行保留）
	DevModel     byte      `json:"dev_model"`
	GroupID      int       `json:"group_id"`
	Status       byte      `json:"status"`
	IsCerted     bool      `json:"is_certed"`
	Priority     int       `json:"priority"`
	OnlineTime   time.Time `json:"online_time"`
	CreateTime   time.Time `json:"create_time"`
	UpdateTime   time.Time `json:"update_time"`
	Note         string    `json:"note"`

	// 设备级别的收发控制（优先级高于群组设置）
	DisableSend bool `json:"disable_send"` // 设备级禁发
	DisableRecv bool `json:"disable_recv"` // 设备级禁收

	// Runtime fields (not stored in DB)
	ISOnline           bool              `json:"is_online"`
	UDPAddr            *net.UDPAddr      `json:"-"`
	LastPacketTime     time.Time         `json:"last_packet_time"`
	LastVoiceTime      int64             `json:"last_voice_time"`
	LastCtlTime        int64             `json:"last_ctl_time"`
	Traffic            int64             `json:"traffic"`
	DeviceParm         map[string]string `json:"device_parm,omitempty"`
	Loged              bool              `json:"-"`
	LastVoiceEndTime   time.Time         `json:"last_voice_end_time"`
	LastCtlEndTime     time.Time         `json:"last_ctl_end_time"`
	VoiceTime          int64             `json:"voice_time"`
	CtlTime            int64             `json:"ctl_time"`
	LastVoiceBeginTime time.Time         `json:"last_voice_begin_time"`
	LastCtlBeginTime   time.Time         `json:"last_ctl_begin_time"`
	LastVoiceDuration  int               `json:"last_voice_duration"`
	LastCtlDuration    int               `json:"last_ctl_duration"`
	UDPSocket          *net.UDPConn      `json:"-"`
	CallSignSSID       string            `json:"callsign_ssid"`
	LastATcommand      *ATCommand        `json:"last_at_command,omitempty"`
	Username           string            `json:"username"` // 运行时字段：从认证结果获取，用于索引
	MAC                string            `json:"mac"`      // 运行时字段：设备上报的 MAC，用于快速重连判定

	// Connection state tracking
	LastDisconnectTime time.Time `json:"last_disconnect_time"` // Last time device went offline
	ReconnectCount     int       `json:"reconnect_count"`      // Number of reconnections
	PreviousUDPAddr    string    `json:"previous_udp_addr"`    // Previous connection address
	IsReconnecting     bool      `json:"is_reconnecting"`      // Currently in reconnection grace period
}

// GetCallSignSSID returns the combined callsign and SSID
func (d *Device) GetCallSignSSID() string {
	return d.CallSign + "-" + string(rune(d.SSID))
}

// ATCommand AT指令
type ATCommand struct {
	CallSign  string `json:"callsign"`
	SSID      byte   `json:"ssid"`
	Type      byte   `json:"type"`
	ATcommand string `json:"at_command"`
	Data      string `json:"data"`
}

// ControlPacket 控制数据包
type ControlPacket struct {
	CallSign string `json:"callsign"`
	SSID     byte   `json:"ssid"`
	Data     []byte `json:"data"`
}
