package gormdb

import (
	"nrllink/internal/models"
)

// ToModelDevice 将 GORM Device 转换为 models.Device
func (d *Device) ToModelDevice() *models.Device {
	return &models.Device{
		ID:           d.ID,
		Name:         d.Name,
		DMRID:        uint32(d.DMRID),
		SSID:         uint8(d.SSID),
		OwnerID:      d.OwnerID,
		QTH:          d.QTH,
		DevModel:     byte(d.DevModel),
		GroupID:      d.GroupID,
		Status:       byte(d.Status),
		IsCerted:     d.IsCerted,
		Priority:     d.Priority,
		OnlineTime:   d.OnlineTime,
		CreateTime:   d.CreateTime,
		UpdateTime:   d.UpdateTime,
		Note:         d.Note,
		DisableSend:  d.DisableSend,
		DisableRecv:  d.DisableRecv,
		ISOnline:     d.ISOnline,
	}
}

// ToModelGroup 将 GORM Group 转换为 models.Group
func (g *Group) ToModelGroup() *models.Group {
	return &models.Group{
		ID:                g.ID,
		Name:              g.Name,
		Type:              g.Type,
		CallSign:          g.CallSign,
		Password:          g.Password,
		AllowCallSignSSID: g.AllowCallSignSSID,
		OwerID:            g.OwerID,
		MasterServer:      g.MasterServer,
		SlaveServer:       g.SlaveServer,
		Status:            g.Status,
		Note:              g.Note,
		CreateTime:        g.CreateTime.Format("2006-01-02 15:04:05"),
		UpdateTime:        g.UpdateTime.Format("2006-01-02 15:04:05"),
		DevMap:            make(map[int]*models.Device),
	}
}

// FromModelDevice 从 models.Device 转换为 GORM Device
func FromModelDevice(d *models.Device) *Device {
	return &Device{
		ID:           d.ID,
		Name:         d.Name,
		DMRID:        int64(d.DMRID),
		SSID:         uint8(d.SSID),
		OwnerID:      d.OwnerID,
		QTH:          d.QTH,
		DevModel:     int(d.DevModel),
		GroupID:      d.GroupID,
		Status:       int8(d.Status),
		IsCerted:     d.IsCerted,
		Priority:     d.Priority,
		OnlineTime:   d.OnlineTime,
		CreateTime:   d.CreateTime,
		UpdateTime:   d.UpdateTime,
		Note:         d.Note,
		DisableSend:  d.DisableSend,
		DisableRecv:  d.DisableRecv,
		ISOnline:     d.ISOnline,
	}
}

// FromModelGroup 从 models.Group 转换为 GORM Group
func FromModelGroup(g *models.Group) *Group {
	return &Group{
		ID:                g.ID,
		Name:              g.Name,
		Type:              g.Type,
		CallSign:          g.CallSign,
		Password:          g.Password,
		AllowCallSignSSID: g.AllowCallSignSSID,
		OwerID:            g.OwerID,
		MasterServer:      g.MasterServer,
		SlaveServer:       g.SlaveServer,
		Status:            g.Status,
		Note:              g.Note,
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
