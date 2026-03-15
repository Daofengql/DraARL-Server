package db

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"nrllink/internal/models"
)

// 设备表列顺序（重要！修改表结构时需要同步更新）
// 0: id, 1: name, 2: dmrid, 3: ssid, 4: owner_id, 5: qth,
// 6: dev_model, 7: group_id, 8: status, 9: is_certed, 10: priority,
// 11: disable_send, 12: disable_recv, 13: is_online, 14: online_time,
// 15: create_time, 16: update_time, 17: note
//
// 注意：所有查询必须明确指定列名（不要用 SELECT *），确保列顺序与上述定义一致

var (
	deviceMap     = make(map[string]*models.Device)
	deviceMapMutex sync.RWMutex
)

// DeviceRepository 设备数据访问层
type DeviceRepository struct {
	db *sql.DB
}

// NewDeviceRepository 创建设备仓库
func NewDeviceRepository() *DeviceRepository {
	return &DeviceRepository{db: Get()}
}

// AddDevice 添加设备
func (r *DeviceRepository) AddDevice(device *models.Device) error {
	query := `INSERT INTO devices (name, dmrid, ssid, owner_id, qth, dev_model, group_id, status, priority, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.Exec(query, device.Name, device.DMRID, device.SSID, device.OwnerID, device.QTH,
		device.DevModel, device.GroupID, device.Status, device.Priority)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	device.ID = int(id)
	return nil
}

// scanDevice 扫描设备行数据的通用函数
func scanDevice(row *sql.Row) (*models.Device, error) {
	device := &models.Device{}
	var qth, note sql.NullString
	var isCerted sql.NullBool
	var createTime, updateTime, onlineTime sql.NullTime

	err := row.Scan(
		&device.ID, &device.Name, &device.DMRID, &device.SSID, &device.OwnerID, &qth,
		&device.DevModel, &device.GroupID, &device.Status, &isCerted, &device.Priority,
		&device.DisableSend, &device.DisableRecv, &device.ISOnline, &onlineTime, &createTime, &updateTime, &note,
	)
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字段
	if qth.Valid {
		device.QTH = qth.String
	}
	if isCerted.Valid {
		device.IsCerted = isCerted.Bool
	}
	if note.Valid {
		device.Note = note.String
	}
	if createTime.Valid {
		device.CreateTime = createTime.Time
	} else {
		device.CreateTime = time.Time{}
	}
	if updateTime.Valid {
		device.UpdateTime = updateTime.Time
	} else {
		device.UpdateTime = time.Time{}
	}
	if onlineTime.Valid {
		device.OnlineTime = onlineTime.Time
	} else {
		device.OnlineTime = time.Time{}
	}

	return device, nil
}

// scanDeviceFromRows 从 Rows 扫描设备
func scanDeviceFromRows(rows *sql.Rows) (*models.Device, error) {
	device := &models.Device{}
	var qth, note sql.NullString
	var isCerted sql.NullBool
	var createTime, updateTime, onlineTime sql.NullTime

	err := rows.Scan(
		&device.ID, &device.Name, &device.DMRID, &device.SSID, &device.OwnerID, &qth,
		&device.DevModel, &device.GroupID, &device.Status, &isCerted, &device.Priority,
		&device.DisableSend, &device.DisableRecv, &device.ISOnline, &onlineTime, &createTime, &updateTime, &note,
	)
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字段
	if qth.Valid {
		device.QTH = qth.String
	}
	if isCerted.Valid {
		device.IsCerted = isCerted.Bool
	}
	if note.Valid {
		device.Note = note.String
	}
	if createTime.Valid {
		device.CreateTime = createTime.Time
	} else {
		device.CreateTime = time.Time{}
	}
	if updateTime.Valid {
		device.UpdateTime = updateTime.Time
	} else {
		device.UpdateTime = time.Time{}
	}
	if onlineTime.Valid {
		device.OnlineTime = onlineTime.Time
	} else {
		device.OnlineTime = time.Time{}
	}

	return device, nil
}

// GetDevice 获取设备（通过 owner_id 和 ssid）
func (r *DeviceRepository) GetDevice(ownerID int, ssid byte) (*models.Device, error) {
	// 明确指定列名，确保顺序与 scanDevice 函数一致
	query := `SELECT id, name, dmrid, ssid, owner_id, qth, dev_model,
	              group_id, status, is_certed, priority, disable_send, disable_recv, is_online,
	              online_time, create_time, update_time, note
	              FROM devices WHERE owner_id = ? AND ssid = ?`
	row := r.db.QueryRow(query, ownerID, ssid)

	device, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, err
	}

	return device, nil
}

// UpdateDevice 更新设备
func (r *DeviceRepository) UpdateDevice(device *models.Device) error {
	query := `UPDATE devices SET name = ?, group_id = ?, status = ?, priority = ?, note = ?, update_time = NOW()
		WHERE owner_id = ? AND ssid = ?`

	_, err := r.db.Exec(query, device.Name, device.GroupID, device.Status, device.Priority,
		device.Note, device.OwnerID, device.SSID)
	return err
}

// DeleteDevice 删除设备
func (r *DeviceRepository) DeleteDevice(ownerID int, ssid byte) error {
	query := `DELETE FROM devices WHERE owner_id = ? AND ssid = ?`
	_, err := r.db.Exec(query, ownerID, ssid)
	return err
}

// ListDevices 列出所有设备
func (r *DeviceRepository) ListDevices(limit, page int) ([]*models.Device, int, error) {
	offset := (page - 1) * limit

	// 获取总数
	var total int
	err := r.db.QueryRow("SELECT COUNT(*) FROM devices").Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 获取分页数据，明确指定列名
	query := `SELECT id, name, dmrid, ssid, owner_id, qth, dev_model,
	              group_id, status, is_certed, priority, disable_send, disable_recv, is_online,
	              online_time, create_time, update_time, note
	              FROM devices ORDER BY id LIMIT ? OFFSET ?`
	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	devices := make([]*models.Device, 0)
	for rows.Next() {
		device, err := scanDeviceFromRows(rows)
		if err != nil {
			log.Printf("Error scanning device: %v", err)
			continue
		}
		devices = append(devices, device)
	}

	return devices, total, nil
}

// ChangeDeviceGroup 更改设备群组
func (r *DeviceRepository) ChangeDeviceGroup(ownerID int, ssid byte, groupID int) error {
	query := `UPDATE devices SET group_id = ?, update_time = NOW() WHERE owner_id = ? AND ssid = ?`
	_, err := r.db.Exec(query, groupID, ownerID, ssid)
	return err
}

// GetDeviceByDMRID 通过DMRID获取设备
func (r *DeviceRepository) GetDeviceByDMRID(dmrid uint32) (*models.Device, error) {
	// 明确指定列名，确保顺序与 scanDevice 函数一致
	query := `SELECT id, name, dmrid, ssid, owner_id, qth, dev_model,
	              group_id, status, is_certed, priority, disable_send, disable_recv, is_online,
	              online_time, create_time, update_time, note
	              FROM devices WHERE dmrid = ?`
	row := r.db.QueryRow(query, dmrid)

	device, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, err
	}

	return device, nil
}

// AddToMap 将设备添加到内存映射
func AddToMap(key string, device *models.Device) {
	deviceMapMutex.Lock()
	defer deviceMapMutex.Unlock()
	deviceMap[key] = device
}

// GetFromMap 从内存映射获取设备
func GetFromMap(key string) (*models.Device, bool) {
	deviceMapMutex.RLock()
	defer deviceMapMutex.RUnlock()
	device, ok := deviceMap[key]
	return device, ok
}

// DeleteFromMap 从内存映射删除设备
func DeleteFromMap(key string) {
	deviceMapMutex.Lock()
	defer deviceMapMutex.Unlock()
	delete(deviceMap, key)
}

// RangeMap 遍历内存映射
func RangeMap(fn func(key string, value *models.Device) bool) {
	deviceMapMutex.RLock()
	defer deviceMapMutex.RUnlock()
	for k, v := range deviceMap {
		if !fn(k, v) {
			break
		}
	}
}

// MapLen 获取映射长度
func MapLen() int {
	deviceMapMutex.RLock()
	defer deviceMapMutex.RUnlock()
	return len(deviceMap)
}
