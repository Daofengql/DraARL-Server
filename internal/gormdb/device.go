package gormdb

import (
	"errors"
	"gorm.io/gorm"
)

// DeviceRepository 设备仓库
type DeviceRepository struct {
	db *gorm.DB
}

// NewDeviceRepository 创建设备仓库
func NewDeviceRepository() *DeviceRepository {
	return &DeviceRepository{db: Get()}
}

// ListDevices 获取设备列表
func (r *DeviceRepository) ListDevices(limit, page int) ([]*Device, int64, error) {
	var devices []*Device
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&Device{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&devices).Error; err != nil {
		return nil, 0, err
	}

	return devices, total, nil
}

// ListDevicesByGroupID 获取指定��组的设备列表
func (r *DeviceRepository) ListDevicesByGroupID(groupID int) ([]*Device, error) {
	var devices []*Device
	err := r.db.Where("group_id = ?", groupID).Find(&devices).Error
	return devices, err
}

// ListDevicesByCallSign 按呼号搜索设备
func (r *DeviceRepository) ListDevicesByCallSign(callsign string) ([]*Device, error) {
	var devices []*Device
	err := r.db.Where("callsign LIKE ?", "%"+callsign+"%").Find(&devices).Error
	return devices, err
}

// ListDevicesByUsername 按用户名搜索设备
func (r *DeviceRepository) ListDevicesByUsername(username string) ([]*Device, error) {
	var devices []*Device
	err := r.db.Where("username = ?", username).Find(&devices).Error
	return devices, err
}

// GetDeviceByID 通过ID获取设备
func (r *DeviceRepository) GetDeviceByID(id int) (*Device, error) {
	var device Device
	err := r.db.First(&device, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// GetDeviceByCallSignSSID 通过呼号和SSID获取设备
func (r *DeviceRepository) GetDeviceByCallSignSSID(callsign string, ssid uint8) (*Device, error) {
	var device Device
	err := r.db.Where("callsign = ? AND ssid = ?", callsign, ssid).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// CreateDevice 创建设备
func (r *DeviceRepository) CreateDevice(device *Device) error {
	return r.db.Create(device).Error
}

// UpdateDevice 更新设备
func (r *DeviceRepository) UpdateDevice(device *Device) error {
	return r.db.Save(device).Error
}

// UpdateDeviceFields 更新设备指定字段
func (r *DeviceRepository) UpdateDeviceFields(id int, fields map[string]interface{}) error {
	return r.db.Model(&Device{}).Where("id = ?", id).Updates(fields).Error
}

// ChangeDeviceGroup 修改设备群组
func (r *DeviceRepository) ChangeDeviceGroup(callsign string, ssid uint8, groupID int) error {
	return r.db.Model(&Device{}).
		Where("callsign = ? AND ssid = ?", callsign, ssid).
		Update("group_id", groupID).Error
}

// DeleteDevice 删除设备（通过呼号和SSID）
func (r *DeviceRepository) DeleteDevice(callsign string, ssid uint8) error {
	return r.db.Where("callsign = ? AND ssid = ?", callsign, ssid).Delete(&Device{}).Error
}

// DeleteDeviceByID 删除设备（通过ID）
func (r *DeviceRepository) DeleteDeviceByID(id int) error {
	return r.db.Delete(&Device{}, id).Error
}

// DeviceCount 获取设备总数
func (r *DeviceRepository) DeviceCount() (int64, error) {
	var count int64
	err := r.db.Model(&Device{}).Count(&count).Error
	return count, err
}

// OnlineDeviceCount 获取在线设备数（从数据库查询 is_online = true 的记录）
func (r *DeviceRepository) OnlineDeviceCount() (int64, error) {
	var count int64
	err := r.db.Model(&Device{}).Where("is_online = ?", true).Count(&count).Error
	return count, err
}

// UpdateDeviceOnlineStatus 更新设备在线状态
func (r *DeviceRepository) UpdateDeviceOnlineStatus(callsign string, ssid uint8, isOnline bool, onlineTime string) error {
	updates := map[string]interface{}{
		"is_online": isOnline,
	}
	if onlineTime != "" {
		updates["online_time"] = onlineTime
	}
	return r.db.Model(&Device{}).
		Where("callsign = ? AND ssid = ?", callsign, ssid).
		Updates(updates).Error
}

// UpdateDeviceOnlineStatusByUsername 通过 username 更新设备在线状态
func (r *DeviceRepository) UpdateDeviceOnlineStatusByUsername(username string, ssid uint8, isOnline bool, onlineTime string) error {
	updates := map[string]interface{}{
		"is_online": isOnline,
	}
	if onlineTime != "" {
		updates["online_time"] = onlineTime
	}
	return r.db.Model(&Device{}).
		Where("username = ? AND ssid = ?", username, ssid).
		Updates(updates).Error
}

// UpdateDeviceOnlineTime 更新设备上线时间
func (r *DeviceRepository) UpdateDeviceOnlineTime(callsign string, ssid uint8) error {
	return r.db.Model(&Device{}).
		Where("callsign = ? AND ssid = ?", callsign, ssid).
		Update("online_time", gorm.Expr("NOW()")).Error
}

// GetDeviceByDMRID 通过DMRID获取设备
func (r *DeviceRepository) GetDeviceByDMRID(dmrid int64) (*Device, error) {
	var device Device
	err := r.db.Where("dmrid = ?", dmrid).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}
