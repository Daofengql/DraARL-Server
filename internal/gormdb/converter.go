package gormdb

import (
	"draarl/internal/models"
	"math"
)

// ToModelDevice 将 GORM Device 转换为 models.Device
func (d *Device) ToModelDevice() *models.Device {
	if d == nil {
		return nil
	}
	return &models.Device{
		ID:                    d.ID,
		Name:                  d.Name,
		DMRID:                 protocolDMRID(d.DMRID),
		SSID:                  uint8(d.SSID),
		OwnerID:               d.OwnerID,
		QTH:                   d.QTH,
		LastOnlineIP:          d.LastOnlineIP,
		DevModel:              protocolByte(d.DevModel),
		GroupID:               d.GroupIDValue(),
		Status:                protocolByte(int(d.Status)),
		IsCerted:              d.IsCerted,
		Priority:              d.Priority,
		OnlineTime:            d.OnlineTime,
		CreateTime:            d.CreateTime,
		UpdateTime:            d.UpdateTime,
		Note:                  d.Note,
		DisableSend:           d.DisableSend,
		DisableRecv:           d.DisableRecv,
		ISOnline:              d.ISOnline,
		CurrentEntryNodeID:    d.CurrentEntryNodeID,
		CurrentEntrySessionID: d.CurrentEntrySessionID,
		LastEntryNodeID:       d.LastEntryNodeID,
		LastEntryAt:           d.LastEntryAt,
		EntryMode:             d.EntryMode,
	}
}

// ToModelGroup 将 GORM Group 转换为 models.Group
func (g *Group) ToModelGroup() *models.Group {
	if g == nil {
		return nil
	}
	return &models.Group{
		ID:           g.ID,
		Name:         g.Name,
		Type:         g.Type,
		Password:     g.Password,
		OwerID:       g.OwerID,
		MasterServer: g.MasterServer,
		SlaveServer:  g.SlaveServer,
		Status:       g.Status,
		IsVirtual:    g.IsVirtual,
		Note:         g.Note,
		CreateTime:   g.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:   g.UpdateTime.Format("2006-01-02 15:04:05"),
		DevMap:       make(map[int]*models.Device),
	}
}

// FromModelDevice 从 models.Device 转换为 GORM Device
func FromModelDevice(d *models.Device) *Device {
	if d == nil {
		return nil
	}
	return &Device{
		ID:                    d.ID,
		Name:                  d.Name,
		DMRID:                 int64(d.DMRID),
		SSID:                  uint8(d.SSID),
		OwnerID:               d.OwnerID,
		QTH:                   d.QTH,
		LastOnlineIP:          d.LastOnlineIP,
		DevModel:              int(d.DevModel),
		GroupID:               nullableGroupID(d.GroupID),
		Status:                protocolStatus(d.Status),
		IsCerted:              d.IsCerted,
		Priority:              d.Priority,
		OnlineTime:            d.OnlineTime,
		CreateTime:            d.CreateTime,
		UpdateTime:            d.UpdateTime,
		Note:                  d.Note,
		DisableSend:           d.DisableSend,
		DisableRecv:           d.DisableRecv,
		ISOnline:              d.ISOnline,
		CurrentEntryNodeID:    d.CurrentEntryNodeID,
		CurrentEntrySessionID: d.CurrentEntrySessionID,
		LastEntryNodeID:       d.LastEntryNodeID,
		LastEntryAt:           d.LastEntryAt,
		EntryMode:             d.EntryMode,
	}
}

func nullableGroupID(groupID int) *int {
	if groupID <= 0 {
		return nil
	}
	return &groupID
}

// FromModelGroup 从 models.Group 转换为 GORM Group
func FromModelGroup(g *models.Group) *Group {
	if g == nil {
		return nil
	}
	return &Group{
		ID:           g.ID,
		Name:         g.Name,
		Type:         g.Type,
		Password:     g.Password,
		OwerID:       g.OwerID,
		MasterServer: g.MasterServer,
		SlaveServer:  g.SlaveServer,
		Status:       g.Status,
		IsVirtual:    g.IsVirtual,
		Note:         g.Note,
	}
}

// ToModelDevices 批量转换设备
func ToModelDevices(devices []*Device) []*models.Device {
	result := make([]*models.Device, len(devices))
	for i, d := range devices {
		result[i] = d.ToModelDevice()
	}
	return result
}

// ToModelGroups 批量转换群组
func ToModelGroups(groups []*Group) []*models.Group {
	result := make([]*models.Group, len(groups))
	for i, g := range groups {
		result[i] = g.ToModelGroup()
	}
	return result
}

func protocolDMRID(value int64) uint32 {
	if value < 0 || value > math.MaxUint32 {
		return 0
	}
	return uint32(value)
}
func protocolByte(value int) byte {
	if value < 0 || value > math.MaxUint8 {
		return 0
	}
	return byte(value)
}
func protocolStatus(value byte) int8 {
	if value > math.MaxInt8 {
		return 0
	}
	return int8(value)
}
