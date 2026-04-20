package gormdb

import (
	"sync"

	"gorm.io/gorm"
)

// FirmwareRepository 固件管理仓储
type FirmwareRepository struct {
	db *gorm.DB
}

var firmwareRepoInstance *FirmwareRepository
var firmwareRepoOnce sync.Once

// GetFirmwareRepo 获取固件管理仓储实例
func GetFirmwareRepo() *FirmwareRepository {
	firmwareRepoOnce.Do(func() {
		db := Get()
		firmwareRepoInstance = &FirmwareRepository{db: db}
	})
	return firmwareRepoInstance
}

// ListByDevModel 分页查询固件列表，devModel=0 时查全部
func (r *FirmwareRepository) ListByDevModel(devModel int, page, pageSize int) ([]*FirmwareRelease, int64, error) {
	var list []*FirmwareRelease
	var total int64

	query := r.db.Model(&FirmwareRelease{})
	if devModel > 0 {
		query = query.Where("dev_model = ?", devModel)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("create_time DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}

	return list, total, nil
}

// GetByID 根据 ID 获取固件记录
func (r *FirmwareRepository) GetByID(id int) (*FirmwareRelease, error) {
	var fw FirmwareRelease
	if err := r.db.First(&fw, id).Error; err != nil {
		return nil, err
	}
	return &fw, nil
}

// GetLatestByDevModel 获取指定型号的最新固件（is_latest = true）
func (r *FirmwareRepository) GetLatestByDevModel(devModel int) (*FirmwareRelease, error) {
	var fw FirmwareRelease
	if err := r.db.Where("dev_model = ? AND is_latest = ?", devModel, true).First(&fw).Error; err != nil {
		return nil, err
	}
	return &fw, nil
}

// GetAllLatest 获取所有型号的最新固件
func (r *FirmwareRepository) GetAllLatest() ([]*FirmwareRelease, error) {
	var list []*FirmwareRelease
	if err := r.db.Where("is_latest = ?", true).Order("dev_model ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ExistsVersion 检查指定型号的版本号是否已存在
func (r *FirmwareRepository) ExistsVersion(devModel int, version string) (bool, error) {
	var count int64
	if err := r.db.Model(&FirmwareRelease{}).Where("dev_model = ? AND version = ?", devModel, version).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// Create 创建固件记录（事务内维护 is_latest 一致性）
func (r *FirmwareRepository) Create(fw *FirmwareRelease) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 清除同型号的 is_latest 标记
		if err := tx.Model(&FirmwareRelease{}).
			Where("dev_model = ? AND is_latest = ?", fw.DevModel, true).
			Update("is_latest", false).Error; err != nil {
			return err
		}
		// 新记录设为 is_latest
		fw.IsLatest = true
		return tx.Create(fw).Error
	})
}

// Delete 删除固件记录（事务内维护 is_latest 一致性）
func (r *FirmwareRepository) Delete(id int) (*FirmwareRelease, error) {
	var fw FirmwareRelease
	if err := r.db.First(&fw, id).Error; err != nil {
		return nil, err
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&FirmwareRelease{}, id).Error; err != nil {
			return err
		}

		// 如果被删除的是最新版本，提升次新版本
		if fw.IsLatest {
			var next FirmwareRelease
			if err := tx.Where("dev_model = ?", fw.DevModel).
				Order("create_time DESC").First(&next).Error; err == nil {
				// 找到次新版本，设为最新
				return tx.Model(&next).Update("is_latest", true).Error
			}
			// 没有更多记录，无需操作
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &fw, nil
}
