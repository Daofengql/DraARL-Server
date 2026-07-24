package gormdb

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *DeviceRepository) UpdateDeviceEntry(deviceID int, nodeID, mode string, sessionID uint64, online bool, now time.Time) error {
	updates := map[string]interface{}{
		"current_entry_node_id":    nodeID,
		"current_entry_session_id": sessionID,
		"last_entry_node_id":       nodeID,
		"last_entry_at":            now,
		"entry_mode":               mode,
		"is_online":                online,
	}
	if online {
		updates["online_time"] = now
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var device Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&device, deviceID).Error; err != nil {
			return err
		}
		return tx.Model(&Device{}).Where("id = ?", deviceID).Updates(updates).Error
	})
}

// ClearDeviceEntryIfSession prevents a delayed centre timeout from clearing a
// newer edge assignment for the same persistent device.
func (r *DeviceRepository) ClearDeviceEntryIfSession(deviceID int, nodeID string, sessionID uint64) (bool, error) {
	result := r.db.Model(&Device{}).
		Where("id = ? AND current_entry_node_id = ? AND current_entry_session_id = ?", deviceID, nodeID, sessionID).
		Updates(map[string]interface{}{
			"current_entry_node_id": "", "current_entry_session_id": 0, "is_online": false,
		})
	return result.RowsAffected > 0, result.Error
}

var ErrDeviceNotInGroup = errors.New("device is no longer in the expected group")

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

// ListAllDevices 返回运行时路由缓存需要的完整设备集合，不设置分页硬上限。
func (r *DeviceRepository) ListAllDevices() ([]*Device, error) {
	var devices []*Device
	if err := r.db.Order("id DESC").Find(&devices).Error; err != nil {
		return nil, err
	}
	return devices, nil
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

// ListDevicesByIDsWithOwner loads one bounded confirmation batch and its
// owners without issuing one query per device.
func (r *DeviceRepository) ListDevicesByIDsWithOwner(ids []int) ([]*Device, error) {
	if len(ids) == 0 {
		return []*Device{}, nil
	}
	var devices []*Device
	err := r.db.Preload("Owner").Where("id IN ?", ids).Find(&devices).Error
	return devices, err
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
	err := r.db.Create(device).Error
	if IsDuplicateKeyError(err) {
		return ErrOwnerSSIDConflict
	}
	return err
}

// UpdateDevice 更新设备
func (r *DeviceRepository) UpdateDevice(device *Device) error {
	return r.db.Save(device).Error
}

// UpdateDeviceFields 更新设备指定字段
func (r *DeviceRepository) UpdateDeviceFields(id int, fields map[string]interface{}) error {
	return r.db.Model(&Device{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateDeviceCommControlInGroup 原子更新当前仍属于指定群组的设备收发状态。
// 行锁保证群主校验与更新期间设备不能并发切换群组。
func (r *DeviceRepository) UpdateDeviceCommControlInGroup(
	deviceID, groupID int,
	disableSend, disableRecv *bool,
) (before, after *Device, err error) {
	err = r.db.Transaction(func(tx *gorm.DB) error {
		var device Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&device, deviceID).Error; err != nil {
			return err
		}
		if device.GroupID != groupID {
			return ErrDeviceNotInGroup
		}

		original := device
		updates := make(map[string]interface{}, 2)
		if disableSend != nil {
			updates["disable_send"] = *disableSend
			device.DisableSend = *disableSend
		}
		if disableRecv != nil {
			updates["disable_recv"] = *disableRecv
			device.DisableRecv = *disableRecv
		}
		if len(updates) > 0 {
			result := tx.Model(&Device{}).
				Where("id = ? AND group_id = ?", deviceID, groupID).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
		}

		before = &original
		after = &device
		return nil
	})
	return before, after, err
}

// DeleteDeviceByID 删除设备（仅删除设备记录，不清理关联数据）
// 注意： 请使用 DeleteDeviceWithCascade 进行完整的级联删除
func (r *DeviceRepository) DeleteDeviceByID(id int) error {
	return r.db.Delete(&Device{}, id).Error
}

// DeleteDeviceWithCascade 删除设备及其所有关联数据（事务级联删除）
// 包括：device_configs, comm_records 中的设备引用
func (r *DeviceRepository) DeleteDeviceWithCascade(id int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 1. 删除设备的配置
		if err := tx.Where("device_id = ?", id).Delete(&DeviceConfig{}).Error; err != nil {
			return err
		}

		// 2. 清除通信记录中的设备引用。CommRecord.DeviceID 为非空字段，
		// 协议约定 0 表示无普通设备引用（幽灵/历史记录），因此不能写 NULL。
		if err := tx.Model(&CommRecord{}).Where("device_id = ?", id).Update("device_id", 0).Error; err != nil {
			return err
		}

		// 3. 最后删除设备本身
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

// PrepareDevicesForStartup marks every device offline. When interconnect is
// enabled, remote ownership is retained briefly so a still-running edge can
// prove its previous node/control-session assignment after a centre restart.
func (r *DeviceRepository) PrepareDevicesForStartup(preserveRemoteEntries bool) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Device{}).Where("is_online = ?", true).Update("is_online", false).Error; err != nil {
			return err
		}
		query := tx.Model(&Device{})
		if preserveRemoteEntries {
			query = query.Where("current_entry_node_id = ?", "center")
		} else {
			query = query.Where("current_entry_node_id <> ? OR current_entry_session_id <> ?", "", 0)
		}
		return query.Updates(map[string]interface{}{"current_entry_node_id": "", "current_entry_session_id": 0}).Error
	})
}

// MarkAllDevicesOffline preserves the historical single-node behaviour.
func (r *DeviceRepository) MarkAllDevicesOffline() error {
	return r.PrepareDevicesForStartup(false)
}

type DeviceEntrySession struct {
	NodeID    string `gorm:"column:current_entry_node_id"`
	SessionID uint64 `gorm:"column:current_entry_session_id"`
}

// ListOfflineRemoteEntrySessions captures only ownership left by the previous
// centre process. The control listener is started after this query, so later
// disconnects cannot be swept by the startup recovery timer.
func (r *DeviceRepository) ListOfflineRemoteEntrySessions() ([]DeviceEntrySession, error) {
	var sessions []DeviceEntrySession
	err := r.db.Model(&Device{}).
		Select("DISTINCT current_entry_node_id, current_entry_session_id").
		Where("is_online = ? AND current_entry_node_id <> ? AND current_entry_session_id <> ?", false, "", 0).
		Find(&sessions).Error
	return sessions, err
}

// UpdateDeviceOnlineStatus 更新设备在线状态（通过 owner_id）
func (r *DeviceRepository) UpdateDeviceOnlineStatus(ownerID int, ssid uint8, isOnline bool, onlineTime, lastOnlineIP string) error {
	updates := map[string]interface{}{
		"is_online": isOnline,
	}
	if !isOnline {
		updates["current_entry_node_id"] = ""
		updates["current_entry_session_id"] = 0
	}
	if onlineTime != "" {
		updates["online_time"] = onlineTime
	}
	if lastOnlineIP != "" {
		updates["last_online_ip"] = lastOnlineIP
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

// ListDevicesByKeywordPaginated 按设备名称或所有者呼号模糊搜索并分页。
// LEFT JOIN 保证历史上没有有效所有者关联的设备仍可按设备名称检索。
func (r *DeviceRepository) ListDevicesByKeywordPaginated(keyword string, ownerID int, limit, page int) ([]*Device, int64, error) {
	var devices []*Device
	var total int64

	offset := (page - 1) * limit
	like := "%" + keyword + "%"
	query := r.db.Model(&Device{}).
		Joins("LEFT JOIN users ON devices.owner_id = users.id").
		Where("devices.name LIKE ? OR users.callsign LIKE ?", like, like)

	if ownerID > 0 {
		query = query.Where("devices.owner_id = ?", ownerID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Select("devices.*").Order("devices.id DESC").Limit(limit).Offset(offset).Find(&devices).Error; err != nil {
		return nil, 0, err
	}

	return devices, total, nil
}

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
