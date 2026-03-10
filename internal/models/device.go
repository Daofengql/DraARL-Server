package models

import (
	"net"
	"time"
)

// Device 设备信息
type Device struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	DMRID          uint32    `json:"dmrid"`
	CallSign       string    `json:"callsign"`
	SSID           byte      `json:"ssid"`
	Password       string    `json:"password"`
	DevModel       byte      `json:"dev_model"`
	GroupID        int       `json:"group_id"`
	Status         byte      `json:"status"`
	Priority       int       `json:"priority"`
	ChanName       []string  `json:"chan_name"`
	OnlineTime     time.Time `json:"online_time"`
	CreateTime     time.Time `json:"create_time"`
	UpdateTime     time.Time `json:"update_time"`
	Note           string    `json:"note"`

	// Runtime fields (not stored in DB)
	ISOnline         bool              `json:"is_online"`
	UDPAddr          *net.UDPAddr      `json:"-"`
	LastPacketTime   time.Time         `json:"last_packet_time"`
	LastVoiceTime    int64             `json:"last_voice_time"`
	LastCtlTime      int64             `json:"last_ctl_time"`
	Traffic          int64             `json:"traffic"`
	QTH              string            `json:"qth"`
	DeviceParm       map[string]string `json:"device_parm,omitempty"`
	Loged            bool              `json:"-"`
	LastVoiceEndTime time.Time         `json:"last_voice_end_time"`
	LastCtlEndTime   time.Time         `json:"last_ctl_end_time"`
	VoiceTime        int64             `json:"voice_time"`
	CtlTime          int64             `json:"ctl_time"`
	LastVoiceBeginTime time.Time       `json:"last_voice_begin_time"`
	LastCtlBeginTime   time.Time       `json:"last_ctl_begin_time"`
	LastVoiceDuration  int             `json:"last_voice_duration"`
	LastCtlDuration    int             `json:"last_ctl_duration"`
	UDPSocket          *net.UDPConn    `json:"-"`
	CallSignSSID       string          `json:"callsign_ssid"`
	PcmG711Chan        chan [][]byte   `json:"-"`        // Exported for use in udphub package
	PcmBuffer          []int           `json:"-"`        // Exported for use in udphub package
	LastATcommand      *ATCommand      `json:"last_at_command,omitempty"`
	Speaking           *bool           `json:"-"`        // Exported for use in udphub package (meeting mode)
}

// GetCallSignSSID returns the combined callsign and SSID
func (d *Device) GetCallSignSSID() string {
	return d.CallSign + "-" + string(rune(d.SSID))
}

// ATCommand AT指令
type ATCommand struct {
	CallSign string `json:"callsign"`
	SSID     byte   `json:"ssid"`
	Type     byte   `json:"type"`
	ATcommand string `json:"at_command"`
	Data     string `json:"data"`
}

// ControlPacket 控制数据包
type ControlPacket struct {
	CallSign string `json:"callsign"`
	SSID     byte   `json:"ssid"`
	Data     []byte `json:"data"`
}
