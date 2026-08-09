package gormdb

import (
	"sort"
	"sync"

	"draarl/internal/firmwareversion"

	"gorm.io/gorm"
)

// FirmwareRepository 固件管理仓储
type FirmwareRepository struct {
	db *gorm.DB
}

var firmwareRepoInstance *FirmwareRepository
var firmwareRepoOnce sync.Once

func selectLatestFirmwareRelease(list []FirmwareRelease) *FirmwareRelease {
	if len(list) == 0 {
		return nil
	}

	latestIndex := 0
	for i := 1; i < len(list); i++ {
		cmp := firmwareversion.CompareVersions(list[i].Version, list[latestIndex].Version)
		if cmp > 0 || (cmp == 0 && list[i].CreateTime.After(list[latestIndex].CreateTime)) {
			latestIndex = i
		}
	}

	return &list[latestIndex]
}

func (r *FirmwareRepository) syncLatestFlagTx(tx *gorm.DB, devModel int) error {
	var list []FirmwareRelease
	if err := tx.Where("dev_model = ?", devModel).Find(&list).Error; err != nil {
		return err
	}

	latest := selectLatestFirmwareRelease(list)
	if latest == nil {
		return nil
	}

	if err := tx.Model(&FirmwareRelease{}).
		Where("dev_model = ?", devModel).
		Update("is_latest", false).Error; err != nil {
		return err
	}

	return tx.Model(&FirmwareRelease{}).
		Where("id = ?", latest.ID).
		Update("is_latest", true).Error
}

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
	var list []FirmwareRelease
	if err := r.db.Where("dev_model = ?", devModel).Find(&list).Error; err != nil {
		return nil, err
	}

	latest := selectLatestFirmwareRelease(list)
	if latest == nil {
		return nil, gorm.ErrRecordNotFound
	}

	fw := *latest
	return &fw, nil
}

// GetAllLatest 获取所有型号的最新固件
func (r *FirmwareRepository) GetAllLatest() ([]*FirmwareRelease, error) {
	var list []FirmwareRelease
	if err := r.db.Order("dev_model ASC, create_time DESC").Find(&list).Error; err != nil {
		return nil, err
	}

	latestByModel := make(map[int]FirmwareRelease)
	models := make([]int, 0)

	for i := range list {
		fw := list[i]
		existing, exists := latestByModel[fw.DevModel]
		if !exists {
			latestByModel[fw.DevModel] = fw
			models = append(models, fw.DevModel)
			continue
		}

		cmp := firmwareversion.CompareVersions(fw.Version, existing.Version)
		if cmp > 0 || (cmp == 0 && fw.CreateTime.After(existing.CreateTime)) {
			latestByModel[fw.DevModel] = fw
		}
	}

	sort.Ints(models)

	result := make([]*FirmwareRelease, 0, len(models))
	for _, devModel := range models {
		fw := latestByModel[devModel]
		fwCopy := fw
		result = append(result, &fwCopy)
	}

	return result, nil
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
		if err := tx.Create(fw).Error; err != nil {
			return err
		}
		return r.syncLatestFlagTx(tx, fw.DevModel)
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

		return r.syncLatestFlagTx(tx, fw.DevModel)
	})

	if err != nil {
		return nil, err
	}
	return &fw, nil
}
