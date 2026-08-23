package models

import (
	"net"
	"sync"
	"time"
)

// DeviceRuntimeSnapshot is an immutable view of fields read by routing and
// receiver-cache code. The live Device remains mutable for compatibility with
// existing callers, while snapshots provide a consistent boundary for
// background work.
type DeviceRuntimeSnapshot struct {
	ID                       int
	DMRID                    uint32
	OwnerID                  int
	SSID                     byte
	GroupID                  int
	Username                 string
	CallSign                 string
	Nickname                 string
	CallSignSSID             string
	MAC                      string
	Name                     string
	QTH                      string
	LastOnlineIP             string
	DevModel                 byte
	Status                   byte
	IsCerted                 bool
	Priority                 int
	CreateTime               time.Time
	UpdateTime               time.Time
	Note                     string
	ISOnline                 bool
	OnlineTime               time.Time
	UDPAddr                  *net.UDPAddr
	RealUDPAddr              *net.UDPAddr
	LastPacketTime           time.Time
	LastVoiceTime            int64
	LastCtlTime              int64
	Traffic                  int64
	DeviceParm               map[string]string
	Loged                    bool
	LastVoiceEndTime         time.Time
	LastCtlEndTime           time.Time
	VoiceTime                int64
	CtlTime                  int64
	LastVoiceBeginTime       time.Time
	LastCtlBeginTime         time.Time
	LastVoiceDuration        int
	LastCtlDuration          int
	DisableSend              bool
	DisableRecv              bool
	GhostSessionID           string
	GhostSessionTag          uint32
	ClientInstanceID         string
	GhostRxGroupIDs          []int
	GhostProtocolVersion     uint16
	GhostCapabilities        []string
	LastDisconnectTime       time.Time
	ReconnectCount           int
	PreviousUDPAddr          string
	IsReconnecting           bool
	CurrentEntryNodeID       string
	CurrentEntrySessionID    uint64
	LastEntryNodeID          string
	LastEntryAt              *time.Time
	EntryMode                string
	InterconnectSessionID    uint64
	InterconnectSessionEpoch uint64
}

// Device 设备信息
type Device struct {
	runtimeMu sync.RWMutex

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
	RealUDPAddr        *net.UDPAddr      `json:"-"` // PROXY v2 source; UDPAddr remains the response route
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
	Username           string            `json:"username"` // 运行时字段：从认证结果获取，用于索引
	Nickname           string            `json:"nickname"` // 运行时字段：发送者昵称快照
	MAC                string            `json:"mac"`      // 运行时字段：设备上报的 MAC，用于快速重连判定

	// UDP ghost session state. These fields are runtime-only and are never
	// persisted to the physical devices table.
	GhostSessionID       string   `json:"ghost_session_id,omitempty"`
	GhostSessionTag      uint32   `json:"-"`
	ClientInstanceID     string   `json:"client_instance_id,omitempty"`
	GhostRxGroupIDs      []int    `json:"rx_group_ids,omitempty"`
	GhostProtocolVersion uint16   `json:"ghost_protocol_version,omitempty"`
	GhostCapabilities    []string `json:"ghost_capabilities,omitempty"`

	// Connection state tracking
	LastDisconnectTime       time.Time  `json:"last_disconnect_time"` // Last time device went offline
	ReconnectCount           int        `json:"reconnect_count"`      // Number of reconnections
	PreviousUDPAddr          string     `json:"previous_udp_addr"`    // Previous connection address
	IsReconnecting           bool       `json:"is_reconnecting"`      // Currently in reconnection grace period
	CurrentEntryNodeID       string     `json:"current_entry_node_id,omitempty"`
	CurrentEntrySessionID    uint64     `json:"-"`
	LastEntryNodeID          string     `json:"last_entry_node_id,omitempty"`
	LastEntryAt              *time.Time `json:"last_entry_at,omitempty"`
	EntryMode                string     `json:"entry_mode,omitempty"`
	InterconnectSessionID    uint64     `json:"-"`
	InterconnectSessionEpoch uint64     `json:"-"`
}

// RuntimeSnapshot returns a copy safe to use after the caller releases the
// live device pointer. Network addresses and slices are copied as well.
func (d *Device) RuntimeSnapshot() DeviceRuntimeSnapshot {
	if d == nil {
		return DeviceRuntimeSnapshot{}
	}
	d.runtimeMu.RLock()
	defer d.runtimeMu.RUnlock()
	addr := cloneUDPAddr(d.UDPAddr)
	realAddr := cloneUDPAddr(d.RealUDPAddr)
	return DeviceRuntimeSnapshot{
		ID: d.ID, DMRID: d.DMRID, OwnerID: d.OwnerID, SSID: d.SSID, GroupID: d.GroupID,
		Username: d.Username, CallSign: d.CallSign, Nickname: d.Nickname,
		CallSignSSID: d.CallSignSSID, MAC: d.MAC, Name: d.Name, QTH: d.QTH,
		LastOnlineIP: d.LastOnlineIP, DevModel: d.DevModel, Status: d.Status,
		IsCerted: d.IsCerted, Priority: d.Priority, CreateTime: d.CreateTime,
		UpdateTime: d.UpdateTime, Note: d.Note, ISOnline: d.ISOnline,
		OnlineTime: d.OnlineTime, UDPAddr: addr, RealUDPAddr: realAddr, LastPacketTime: d.LastPacketTime,
		LastVoiceTime: d.LastVoiceTime, LastCtlTime: d.LastCtlTime,
		Traffic: d.Traffic, DeviceParm: cloneStringMap(d.DeviceParm), Loged: d.Loged,
		LastVoiceEndTime: d.LastVoiceEndTime, LastCtlEndTime: d.LastCtlEndTime,
		VoiceTime: d.VoiceTime, CtlTime: d.CtlTime,
		LastVoiceBeginTime: d.LastVoiceBeginTime, LastCtlBeginTime: d.LastCtlBeginTime,
		LastVoiceDuration: d.LastVoiceDuration, LastCtlDuration: d.LastCtlDuration,
		DisableSend: d.DisableSend, DisableRecv: d.DisableRecv,
		GhostSessionID: d.GhostSessionID, GhostSessionTag: d.GhostSessionTag,
		ClientInstanceID:     d.ClientInstanceID,
		GhostRxGroupIDs:      append([]int(nil), d.GhostRxGroupIDs...),
		GhostProtocolVersion: d.GhostProtocolVersion,
		GhostCapabilities:    append([]string(nil), d.GhostCapabilities...),
		LastDisconnectTime:   d.LastDisconnectTime, ReconnectCount: d.ReconnectCount,
		PreviousUDPAddr: d.PreviousUDPAddr, IsReconnecting: d.IsReconnecting,
		CurrentEntryNodeID:    d.CurrentEntryNodeID,
		CurrentEntrySessionID: d.CurrentEntrySessionID, LastEntryNodeID: d.LastEntryNodeID,
		LastEntryAt: cloneTimePtr(d.LastEntryAt), EntryMode: d.EntryMode,
		InterconnectSessionID:    d.InterconnectSessionID,
		InterconnectSessionEpoch: d.InterconnectSessionEpoch,
	}
}

func cloneUDPAddr(value *net.UDPAddr) *net.UDPAddr {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.IP = append(net.IP(nil), value.IP...)
	return &copyValue
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

// UpdateRuntime serializes a mutation of live runtime fields. Callers should
// keep the callback short and avoid invoking code that may re-enter the device.
func (d *Device) UpdateRuntime(update func(*Device)) {
	if d == nil || update == nil {
		return
	}
	d.runtimeMu.Lock()
	update(d)
	d.runtimeMu.Unlock()
}

// GetCallSignSSID returns the combined callsign and SSID
func (d *Device) GetCallSignSSID() string {
	state := d.RuntimeSnapshot()
	return state.CallSign + "-" + string(rune(state.SSID))
}
