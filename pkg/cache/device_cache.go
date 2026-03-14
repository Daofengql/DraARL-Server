package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "nrllink/internal/gormdb"
)

// DeviceCache 设备信息缓存管理器
type DeviceCache struct {
	cache *ThreeLevelCache
}

// DeviceCacheConfig 设备缓存配置
type DeviceCacheConfig struct {
	// L1 本地缓存配置
	LocalTTL time.Duration // 详情默认 1 分钟，列表 30 秒
	MaxSize  int           // 默认 10000

	// L2 Redis 缓存配置
	RedisTTL time.Duration // 详情默认 5 分钟，列表 1 分钟
}

// NewDeviceCache 创建设备缓存管理器
func NewDeviceCache(config DeviceCacheConfig) (*DeviceCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = time.Minute
	}
	if config.RedisTTL == 0 {
		config.RedisTTL = 5 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 10000
	}

	cache, err := NewThreeLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
		RedisTTL: config.RedisTTL,
	})
	if err != nil {
		return nil, err
	}

	return &DeviceCache{cache: cache}, nil
}

// 缓存键生成函数

// deviceKey 设备详情缓存键
func deviceKey(deviceID int) string {
	return fmt.Sprintf("device:info:%d", deviceID)
}

// deviceByCallSignKey 通过呼号和SSID查询的缓存键
func deviceByCallSignKey(callsign string, ssid uint8) string {
	return fmt.Sprintf("device:callsign:%s:%d", callsign, ssid)
}

// deviceListKey 设备列表缓存键（分页）
func deviceListKey(page, pageSize int) string {
	return fmt.Sprintf("device:list:page:%d:size:%d", page, pageSize)
}

// deviceListTotalKey 设备总数缓存键
func deviceListTotalKey() string {
	return "device:list:total"
}

// deviceListByUserKey 用户设备列表缓存键
func deviceListByUserKey(username string, page, pageSize int) string {
	return fmt.Sprintf("device:user:%s:page:%d:size:%d", username, page, pageSize)
}

// deviceListByGroupKey 群组设备列表缓存键
func deviceListByGroupKey(groupID int) string {
	return fmt.Sprintf("device:group:%d", groupID)
}

// GetDeviceByID 通过ID获取设备详情（带缓存）
func (c *DeviceCache) GetDeviceByID(ctx context.Context, id int) (*gormdb.Device, error) {
	key := deviceKey(id)

	var device gormdb.Device
	if err := c.cache.Get(ctx, key, &device); err == nil {
		return &device, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewDeviceRepository()
	dbDevice, err := repo.GetDeviceByID(id)
	if err != nil {
		return nil, err
	}
	if dbDevice == nil {
		return nil, nil
	}

	// 写入缓存（详情缓存 5 分钟）
	_ = c.cache.Set(ctx, key, dbDevice, 5*time.Minute)

	return dbDevice, nil
}

// GetDeviceByCallSignSSID 通过呼号和SSID获取设备详情（带缓存）
func (c *DeviceCache) GetDeviceByCallSignSSID(ctx context.Context, callsign string, ssid uint8) (*gormdb.Device, error) {
	key := deviceByCallSignKey(callsign, ssid)

	var device gormdb.Device
	if err := c.cache.Get(ctx, key, &device); err == nil {
		return &device, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewDeviceRepository()
	dbDevice, err := repo.GetDeviceByCallSignSSID(callsign, ssid)
	if err != nil {
		return nil, err
	}
	if dbDevice == nil {
		return nil, nil
	}

	// 写入缓存（详情缓存 5 分钟，同时写入按ID的键）
	_ = c.cache.Set(ctx, key, dbDevice, 5*time.Minute)
	_ = c.cache.Set(ctx, deviceKey(dbDevice.ID), dbDevice, 5*time.Minute)

	return dbDevice, nil
}

// GetDeviceList 获取设备列表（带缓存，列表使用短TTL被动过期）
func (c *DeviceCache) GetDeviceList(ctx context.Context, page, pageSize int) ([]*gormdb.Device, int64, error) {
	itemsKey := deviceListKey(page, pageSize)
	totalKey := deviceListTotalKey()

	var devices []*gormdb.Device
	var total int64

	// 尝试从缓存获取列表和总数
	itemsHit := c.cache.Get(ctx, itemsKey, &devices) == nil
	totalHit := c.cache.Get(ctx, totalKey, &total) == nil

	// 完全命中缓存
	if itemsHit && totalHit {
		return devices, total, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewDeviceRepository()
	dbDevices, dbTotal, err := repo.ListDevices(pageSize, page)
	if err != nil {
		return nil, 0, err
	}

	// 缓存穿透保护：确保空集也被缓存
	if dbDevices == nil {
		dbDevices = make([]*gormdb.Device, 0)
	}

	// 写入缓存（列表 1 分钟，总数 2 分钟）
	_ = c.cache.Set(ctx, itemsKey, dbDevices, time.Minute)
	_ = c.cache.Set(ctx, totalKey, dbTotal, 2*time.Minute)

	return dbDevices, dbTotal, nil
}

// InvalidateDevice 使设备详情缓存失效（更新/删除设备时调用）
// 注意：列表缓存不主动失效，依赖TTL自然过期
func (c *DeviceCache) InvalidateDevice(ctx context.Context, deviceID int, callsign string, ssid uint8) error {
	keys := []string{
		deviceKey(deviceID),
	}
	if callsign != "" {
		keys = append(keys, deviceByCallSignKey(callsign, ssid))
	}
	return c.cache.Delete(ctx, keys...)
}

// InvalidateDeviceList 使设备列表缓存失效（批量操作、新增、删除时调用）
// 使用前缀匹配，将所有分页列表一并主动删除
func (c *DeviceCache) InvalidateDeviceList(ctx context.Context) error {
	// 1. 删除总数缓存
	if err := c.cache.Delete(ctx, deviceListTotalKey()); err != nil {
		return err
	}
	// 2. 主动删除所有全局分页列表缓存 (形如 device:list:page:*)
	if err := c.cache.DeletePrefix(ctx, "device:list:page:"); err != nil {
		return err
	}
	// 3. 主动删除所有用户维度的分页列表缓存 (形如 device:user:*)
	if err := c.cache.DeletePrefix(ctx, "device:user:"); err != nil {
		return err
	}
	return nil
}

// GetDevice 获取底层缓存接口（用于特殊操作）
func (c *DeviceCache) GetDevice() *ThreeLevelCache {
	return c.cache
}

// SetDeviceOnlineStatus 更新设备在线状态缓存（实时更新）
func (c *DeviceCache) SetDeviceOnlineStatus(ctx context.Context, deviceID int, isOnline bool) error {
	// 获取当前设备缓存
	key := deviceKey(deviceID)
	var device gormdb.Device
	if err := c.cache.Get(ctx, key, &device); err != nil {
		// 缓存未命中，不需要更新
		return nil
	}

	// 更新在线状态
	device.ISOnline = isOnline
	return c.cache.Set(ctx, key, &device, 5*time.Minute)
}

// GetDevicesByGroupID 获取群组设备列表（带缓存）
func (c *DeviceCache) GetDevicesByGroupID(ctx context.Context, groupID int) ([]*gormdb.Device, error) {
	key := deviceListByGroupKey(groupID)

	var devices []*gormdb.Device
	if err := c.cache.Get(ctx, key, &devices); err == nil {
		return devices, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewDeviceRepository()
	dbDevices, err := repo.ListDevicesByGroupID(groupID)
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbDevices == nil {
		dbDevices = make([]*gormdb.Device, 0)
	}

	// 写入缓存（群组设备列表 1 分钟）
	_ = c.cache.Set(ctx, key, dbDevices, time.Minute)

	return dbDevices, nil
}

// InvalidateDevicesByGroup 使群组设备列表缓存失效
func (c *DeviceCache) InvalidateDevicesByGroup(ctx context.Context, groupID int) error {
	return c.cache.Delete(ctx, deviceListByGroupKey(groupID))
}
