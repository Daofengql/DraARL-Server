package db

import (
	"database/sql"
	"fmt"
	"log"
	"sync"

	"nrllink/internal/models"
)

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
	query := `INSERT INTO devices (name, dmrid, callsign, ssid, password, dev_model, group_id, status, priority, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`

	result, err := r.db.Exec(query, device.Name, device.DMRID, device.CallSign, device.SSID,
		device.Password, device.DevModel, device.GroupID, device.Status, device.Priority)
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

// GetDevice 获取设备
func (r *DeviceRepository) GetDevice(callsign string, ssid byte) (*models.Device, error) {
	query := `SELECT * FROM devices WHERE callsign = ? AND ssid = ?`
	row := r.db.QueryRow(query, callsign, ssid)

	device := &models.Device{}
	var create_time, update_time, online_time sql.NullString

	err := row.Scan(&device.ID, &device.Name, &device.DMRID, &device.CallSign, &device.SSID,
		&device.Password, new(string), &device.DevModel, &device.GroupID, &device.Status,
		new(bool), &device.ChanName, &online_time, &create_time, &update_time, &device.Note, &device.Priority)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, err
	}

	device.CallSignSSID = device.CallSign + "-" + string(rune(device.SSID))
	return device, nil
}

// UpdateDevice 更新设备
func (r *DeviceRepository) UpdateDevice(device *models.Device) error {
	query := `UPDATE devices SET name = ?, group_id = ?, status = ?, priority = ?, note = ?, update_time = datetime('now')
		WHERE callsign = ? AND ssid = ?`

	_, err := r.db.Exec(query, device.Name, device.GroupID, device.Status, device.Priority,
		device.Note, device.CallSign, device.SSID)
	return err
}

// DeleteDevice 删除设备
func (r *DeviceRepository) DeleteDevice(callsign string, ssid byte) error {
	query := `DELETE FROM devices WHERE callsign = ? AND ssid = ?`
	_, err := r.db.Exec(query, callsign, ssid)
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

	// 获取分页数据
	query := `SELECT * FROM devices ORDER BY id LIMIT ? OFFSET ?`
	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	devices := make([]*models.Device, 0)
	for rows.Next() {
		device := &models.Device{}
		var create_time, update_time, online_time sql.NullString

		err := rows.Scan(&device.ID, &device.Name, &device.DMRID, &device.CallSign, &device.SSID,
			&device.Password, new(string), &device.DevModel, &device.GroupID, &device.Status,
			new(bool), &device.ChanName, &online_time, &create_time, &update_time, &device.Note, &device.Priority)
		if err != nil {
			log.Printf("Error scanning device: %v", err)
			continue
		}
		device.CallSignSSID = device.CallSign + "-" + string(rune(device.SSID))
		devices = append(devices, device)
	}

	return devices, total, nil
}

// ChangeDeviceGroup 更改设备群组
func (r *DeviceRepository) ChangeDeviceGroup(callsign string, ssid byte, groupID int) error {
	query := `UPDATE devices SET group_id = ?, update_time = datetime('now') WHERE callsign = ? AND ssid = ?`
	_, err := r.db.Exec(query, groupID, callsign, ssid)
	return err
}

// GetDeviceByDMRID 通过DMRID获取设备
func (r *DeviceRepository) GetDeviceByDMRID(dmrid uint32) (*models.Device, error) {
	query := `SELECT * FROM devices WHERE dmrid = ?`
	row := r.db.QueryRow(query, dmrid)

	device := &models.Device{}
	var create_time, update_time, online_time sql.NullString

	err := row.Scan(&device.ID, &device.Name, &device.DMRID, &device.CallSign, &device.SSID,
		&device.Password, new(string), &device.DevModel, &device.GroupID, &device.Status,
		new(bool), &device.ChanName, &online_time, &create_time, &update_time, &device.Note, &device.Priority)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found")
	}
	if err != nil {
		return nil, err
	}

	device.CallSignSSID = device.CallSign + "-" + string(rune(device.SSID))
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
