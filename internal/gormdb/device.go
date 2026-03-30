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

// ListDevicesByGroupID 获取指定群组的设备列表
func (r *DeviceRepository) ListDevicesByGroupID(groupID int) ([]*Device, error) {
	var devices []*Device
	err := r.db.Where("group_id = ?", groupID).Find(&devices).Error
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

// GetDeviceByOwnerSSID 根据 owner_id + ssid 查询设备（设备唯一性）
func (r *DeviceRepository) GetDeviceByOwnerSSID(ownerID int, ssid uint8) (*Device, error) {
	var device Device
	err := r.db.Where("owner_id = ? AND ssid = ?", ownerID, ssid).First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// ListDevicesByOwnerID 根据所有者ID查询设备
func (r *DeviceRepository) ListDevicesByOwnerID(ownerID int) ([]*Device, error) {
	var devices []*Device
	err := r.db.Where("owner_id = ?", ownerID).Find(&devices).Error
	return devices, err
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

// DeleteDeviceByID 删除设备（仅删除设备记录，不清理关联数据）
// 注意： 请使用 DeleteDeviceWithCascade 进行完整的级联删除
func (r *DeviceRepository) DeleteDeviceByID(id int) error {
	return r.db.Delete(&Device{}, id).Error
}

// DeleteDeviceWithCascade 删除设备及其所有关联数据（事务级联删除）
// 包括： device_configs, group_members 中的设备引用, comm_records 中的设备引用
func (r *DeviceRepository) DeleteDeviceWithCascade(id int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 1. 删除设备的配置
		if err := tx.Where("device_id = ?", id).Delete(&DeviceConfig{}).Error; err != nil {
			return err
		}

		// 2. 清除群组成员中的设备引用（将 device_id 设为 NULL）
		if err := tx.Model(&GroupMember{}).Where("device_id = ?", id).Update("device_id", nil).Error; err != nil {
			return err
		}

		// 3. 清除通信记录中的设备引用（将 device_id 设为 NULL，保留记录用于统计）
		if err := tx.Model(&CommRecord{}).Where("device_id = ?", id).Update("device_id", nil).Error; err != nil {
			return err
		}

		// 4. 最后删除设备本身
		if err := tx.Delete(&Device{}, id).Error; err != nil {
			return err
		}

		return nil
	})
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

// UpdateDeviceOnlineStatus 更新设备在线状态（通过 owner_id）
func (r *DeviceRepository) UpdateDeviceOnlineStatus(ownerID int, ssid uint8, isOnline bool, onlineTime string) error {
	updates := map[string]interface{}{
		"is_online": isOnline,
	}
	if onlineTime != "" {
		updates["online_time"] = onlineTime
	}
	return r.db.Model(&Device{}).
		Where("owner_id = ? AND ssid = ?", ownerID, ssid).
		Updates(updates).Error
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

// ============================================================
// 以下方法通过联表查询 users 表获取设备（呼号存储在 users 表）
// ============================================================

// GetDeviceByCallSignSSID 通过呼号和SSID获取设备（联表查询 users 表）
func (r *DeviceRepository) GetDeviceByCallSignSSID(callsign string, ssid uint8) (*Device, error) {
	var device Device
	// 通过联表查询：devices.owner_id = users.id 且 users.callsign = ?
	err := r.db.Model(&Device{}).
		Select("devices.*").
		Joins("JOIN users ON devices.owner_id = users.id").
		Where("users.callsign = ? AND devices.ssid = ?", callsign, ssid).
		First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &device, nil
}

// ListDevicesByCallSign 按呼号搜索设备（联表查询）
// 通过 users 表关联查询，呼号存储在 users 表中
func (r *DeviceRepository) ListDevicesByCallSign(callsign string) ([]*Device, error) {
	var devices []*Device

	// 使用 Joins 引入 users 表进行内连接
	// 关联条件：devices.owner_id = users.id
	// 过滤条件：users.callsign 匹配传入的呼号
	err := r.db.Model(&Device{}).
		Select("devices.*").
		Joins("JOIN users ON devices.owner_id = users.id").
		Where("users.callsign = ?", callsign).
		Find(&devices).Error

	if err != nil {
		return nil, err
	}
	return devices, nil
}

// ChangeDeviceGroup 修改设备群组（通过 owner_id）
func (r *DeviceRepository) ChangeDeviceGroup(ownerID int, ssid uint8, groupID int) error {
	return r.db.Model(&Device{}).
		Where("owner_id = ? AND ssid = ?", ownerID, ssid).
		Update("group_id", groupID).Error
}

// ============================================================
// 以下方法支持数据库层面分页（解决内存分页性能问题）
// ============================================================

// ListDevicesByCallSignPaginated 按呼号搜索设备并分页（数据库层分页）
func (r *DeviceRepository) ListDevicesByCallSignPaginated(callsign string, ownerID int, limit, page int) ([]*Device, int64, error) {
	var devices []*Device
	var total int64

	offset := (page - 1) * limit

	query := r.db.Model(&Device{}).
		Select("devices.*").
		Joins("JOIN users ON devices.owner_id = users.id").
		Where("users.callsign = ?", callsign)

	// 如果指定了 ownerID，则只查询该用户的设备
	if ownerID > 0 {
		query = query.Where("devices.owner_id = ?", ownerID)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("devices.id DESC").Limit(limit).Offset(offset).Find(&devices).Error; err != nil {
		return nil, 0, err
	}

	return devices, total, nil
}

// ListDevicesByGroupIDPaginated 按群组过滤设备并分页（数据库层分页）
func (r *DeviceRepository) ListDevicesByGroupIDPaginated(groupID, ownerID int, limit, page int) ([]*Device, int64, error) {
	var devices []*Device
	var total int64

	offset := (page - 1) * limit

	query := r.db.Model(&Device{}).Where("group_id = ?", groupID)

	// 如果指定了 ownerID，则只查询该用户的设备
	if ownerID > 0 {
		query = query.Where("owner_id = ?", ownerID)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&devices).Error; err != nil {
		return nil, 0, err
	}

	return devices, total, nil
}

// ListDevicesByOwnerIDPaginated 按所有者查询设备并分页（数据库层分页）
func (r *DeviceRepository) ListDevicesByOwnerIDPaginated(ownerID int, limit, page int) ([]*Device, int64, error) {
	var devices []*Device
	var total int64

	offset := (page - 1) * limit

	query := r.db.Model(&Device{}).Where("owner_id = ?", ownerID)

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&devices).Error; err != nil {
		return nil, 0, err
	}

	return devices, total, nil
}
