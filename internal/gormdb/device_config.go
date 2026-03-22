package gormdb

import (
	"errors"

	"gorm.io/gorm"
)

// DeviceConfigRepository 设备配置仓库
type DeviceConfigRepository struct {
	db *gorm.DB
}

// NewDeviceConfigRepository 创建设备配置仓库
func NewDeviceConfigRepository() *DeviceConfigRepository {
	return &DeviceConfigRepository{db: Get()}
}

// GetDeviceConfig 获取单个配置项
func (r *DeviceConfigRepository) GetDeviceConfig(deviceID uint, key string) (*DeviceConfig, error) {
	var config DeviceConfig
	err := r.db.Where("device_id = ? AND config_key = ?", deviceID, key).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// GetDeviceConfigs 获取设备的所有配置
// 返回 map[config_key]config_value
func (r *DeviceConfigRepository) GetDeviceConfigs(deviceID uint) (map[string]string, error) {
	var configs []DeviceConfig
	err := r.db.Where("device_id = ?", deviceID).Find(&configs).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(configs))
	for _, cfg := range configs {
		result[cfg.ConfigKey] = cfg.ConfigValue
	}
	return result, nil
}

// SetDeviceConfig 设置单个配置项（存在则更新，不存在则创建）
func (r *DeviceConfigRepository) SetDeviceConfig(deviceID uint, key, value string) error {
	var config DeviceConfig
	err := r.db.Where("device_id = ? AND config_key = ?", deviceID, key).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 创建新记录
			config = DeviceConfig{
				DeviceID:    deviceID,
				ConfigKey:   key,
				ConfigValue: value,
			}
			return r.db.Create(&config).Error
		}
		return err
	}

	// 更新现有记录
	config.ConfigValue = value
	return r.db.Save(&config).Error
}

// SetDeviceConfigs 批量设置配置项
// 会更新存在的配置项，创建不存在的配置项
func (r *DeviceConfigRepository) SetDeviceConfigs(deviceID uint, configs map[string]string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for key, value := range configs {
			var config DeviceConfig
			err := tx.Where("device_id = ? AND config_key = ?", deviceID, key).First(&config).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// 创建新记录
					config = DeviceConfig{
						DeviceID:    deviceID,
						ConfigKey:   key,
						ConfigValue: value,
					}
					if err := tx.Create(&config).Error; err != nil {
						return err
					}
					continue
				}
				return err
			}

			// 更新现有记录
			config.ConfigValue = value
			if err := tx.Save(&config).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteDeviceConfig 删除单个配置项
func (r *DeviceConfigRepository) DeleteDeviceConfig(deviceID uint, key string) error {
	return r.db.Where("device_id = ? AND config_key = ?", deviceID, key).Delete(&DeviceConfig{}).Error
}

// DeleteDeviceConfigs 删除设备的所有配置
func (r *DeviceConfigRepository) DeleteDeviceConfigs(deviceID uint) error {
	return r.db.Where("device_id = ?", deviceID).Delete(&DeviceConfig{}).Error
}

// HasDeviceConfigs 检查设备是否有配置记录
func (r *DeviceConfigRepository) HasDeviceConfigs(deviceID uint) (bool, error) {
	var count int64
	err := r.db.Model(&DeviceConfig{}).Where("device_id = ?", deviceID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateDeviceConfigIfChanged 仅当配置值发生变化时更新
// 返回是否发生了更新
func (r *DeviceConfigRepository) UpdateDeviceConfigIfChanged(deviceID uint, key, value string) (bool, error) {
	var config DeviceConfig
	err := r.db.Where("device_id = ? AND config_key = ?", deviceID, key).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 创建新记录
			config = DeviceConfig{
				DeviceID:    deviceID,
				ConfigKey:   key,
				ConfigValue: value,
			}
			if err := r.db.Create(&config).Error; err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	// 检查值是否相同
	if config.ConfigValue == value {
		return false, nil
	}

	// 更新现有记录
	config.ConfigValue = value
	if err := r.db.Save(&config).Error; err != nil {
		return false, err
	}
	return true, nil
}

// UpdateDeviceConfigsIfChanged 批量更新配置项（仅更新变化的配置）
// 返回实际更新的配置项数量
func (r *DeviceConfigRepository) UpdateDeviceConfigsIfChanged(deviceID uint, configs map[string]string) (int, error) {
	updatedCount := 0
	err := r.db.Transaction(func(tx *gorm.DB) error {
		for key, value := range configs {
			var config DeviceConfig
			err := tx.Where("device_id = ? AND config_key = ?", deviceID, key).First(&config).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// 创建新记录
					config = DeviceConfig{
						DeviceID:    deviceID,
						ConfigKey:   key,
						ConfigValue: value,
					}
					if err := tx.Create(&config).Error; err != nil {
						return err
					}
					updatedCount++
					continue
				}
				return err
			}

			// 检查值是否相同
			if config.ConfigValue == value {
				continue
			}

			// 更新现有记录
			config.ConfigValue = value
			if err := tx.Save(&config).Error; err != nil {
				return err
			}
			updatedCount++
		}
		return nil
	})
	return updatedCount, err
}
