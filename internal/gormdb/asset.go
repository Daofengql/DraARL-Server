package gormdb

import (
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// AssetRepository 资源管理仓储
type AssetRepository struct {
	db *gorm.DB
}

var assetRepoInstance *AssetRepository
var assetRepoOnce sync.Once

// GetAssetRepo 获取资源管理仓储实例
func GetAssetRepo() *AssetRepository {
	assetRepoOnce.Do(func() {
		db := Get()
		assetRepoInstance = &AssetRepository{db: db}
	})
	return assetRepoInstance
}

// NewAssetRepository 创建新的资源管理仓储
func NewAssetRepository() *AssetRepository {
	return &AssetRepository{db: Get()}
}

// GetByID 根据ID获取资源
func (r *AssetRepository) GetByID(id uint) (*Asset, error) {
	var asset Asset
	err := r.db.First(&asset, id).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

// GetByParentID 根据父目录ID获取子资源列表
func (r *AssetRepository) GetByParentID(parentID *uint) ([]Asset, error) {
	var assets []Asset
	var err error
	if parentID == nil {
		// 根目录
		err = r.db.Where("parent_id IS NULL").Order("sort_order ASC, created_at DESC").Find(&assets).Error
	} else {
		err = r.db.Where("parent_id = ?", *parentID).Order("sort_order ASC, created_at DESC").Find(&assets).Error
	}
	return assets, err
}

// GetRootFolders 获取根目录下的所有文件夹（用于前台下载中心展示）
func (r *AssetRepository) GetRootFolders() ([]Asset, error) {
	var folders []Asset
	err := r.db.Where("parent_id IS NULL AND type = ?", "folder").
		Order("sort_order ASC, created_at DESC").
		Find(&folders).Error
	return folders, err
}

// GetFilesByParentID 获取指定文件夹下的所有文件
func (r *AssetRepository) GetFilesByParentID(parentID uint) ([]Asset, error) {
	var files []Asset
	err := r.db.Where("parent_id = ? AND type = ?", parentID, "file").
		Order("sort_order ASC, created_at DESC").
		Find(&files).Error
	return files, err
}

// GetChildrenByParentID 获取文件夹下的所有子项（包括子文件夹和文件）
func (r *AssetRepository) GetChildrenByParentID(parentID uint) ([]Asset, error) {
	var children []Asset
	err := r.db.Where("parent_id = ?", parentID).
		Order("type DESC, sort_order ASC, created_at DESC"). // 文件夹优先
		Find(&children).Error
	return children, err
}

// Create 创建资源
func (r *AssetRepository) Create(asset *Asset) error {
	return r.db.Create(asset).Error
}

// Update 更新资源
func (r *AssetRepository) Update(asset *Asset) error {
	return r.db.Save(asset).Error
}

// UpdatePartial 部分更新资源
func (r *AssetRepository) UpdatePartial(id uint, updates map[string]interface{}) error {
	return r.db.Model(&Asset{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 删除资源
func (r *AssetRepository) Delete(id uint) error {
	return r.db.Delete(&Asset{}, id).Error
}

// DeleteWithChildren 递归删除资源及其子资源
func (r *AssetRepository) DeleteWithChildren(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 获取要删除的资源
		var asset Asset
		if err := tx.First(&asset, id).Error; err != nil {
			return err
		}

		// 如果是文件夹，递归删除子资源
		if asset.IsFolder() {
			var children []Asset
			if err := tx.Where("parent_id = ?", id).Find(&children).Error; err != nil {
				return err
			}
			for _, child := range children {
				if err := deleteAssetRecursive(tx, child.ID); err != nil {
					return err
				}
			}
		}

		// 删除当前资源
		return tx.Delete(&Asset{}, id).Error
	})
}

// deleteAssetRecursive 递归删除资源的辅助函数
func deleteAssetRecursive(tx *gorm.DB, id uint) error {
	// 获取要删除的资源
	var asset Asset
	if err := tx.First(&asset, id).Error; err != nil {
		return err
	}

	// 如果是文件夹，递归删除子资源
	if asset.IsFolder() {
		var children []Asset
		if err := tx.Where("parent_id = ?", id).Find(&children).Error; err != nil {
			return err
		}
		for _, child := range children {
			if err := deleteAssetRecursive(tx, child.ID); err != nil {
				return err
			}
		}
	}

	// 删除当前资源
	return tx.Delete(&Asset{}, id).Error
}

// GetChildren 获取资源的所有子资源（包括文件和文件夹）
func (r *AssetRepository) GetChildren(id uint) ([]Asset, error) {
	var children []Asset
	err := r.db.Where("parent_id = ?", id).Order("sort_order ASC, created_at DESC").Find(&children).Error
	return children, err
}

// GetAllChildrenRecursive 递归获取所有子资源
func (r *AssetRepository) GetAllChildrenRecursive(id uint) ([]Asset, error) {
	var result []Asset
	err := r.db.Transaction(func(tx *gorm.DB) error {
		return getAllChildrenRecursive(tx, id, &result)
	})
	return result, err
}

// getAllChildrenRecursive 递归获取所有子资源的辅助函数
func getAllChildrenRecursive(tx *gorm.DB, parentID uint, result *[]Asset) error {
	var children []Asset
	if err := tx.Where("parent_id = ?", parentID).Order("sort_order ASC, created_at DESC").Find(&children).Error; err != nil {
		return err
	}

	for _, child := range children {
		*result = append(*result, child)
		if child.IsFolder() {
			if err := getAllChildrenRecursive(tx, child.ID, result); err != nil {
				return err
			}
		}
	}
	return nil
}

// Move 移动资源到新目录
func (r *AssetRepository) Move(id uint, newParentID *uint) error {
	return r.db.Model(&Asset{}).Where("id = ?", id).Update("parent_id", newParentID).Error
}

// ExistsByName 检查同一目录下是否存在同名资源
func (r *AssetRepository) ExistsByName(name string, parentID *uint) (bool, error) {
	var count int64
	var err error
	if parentID == nil {
		err = r.db.Model(&Asset{}).Where("name = ? AND parent_id IS NULL", name).Count(&count).Error
	} else {
		err = r.db.Model(&Asset{}).Where("name = ? AND parent_id = ?", name, *parentID).Count(&count).Error
	}
	return count > 0, err
}

// GetPath 获取资源的完整路径（虚拟路径）
func (r *AssetRepository) GetPath(id uint) (string, error) {
	var pathSegments []string
	currentID := id

	for {
		var asset Asset
		if err := r.db.First(&asset, currentID).Error; err != nil {
			return "", err
		}
		pathSegments = append([]string{asset.Name}, pathSegments...)
		if asset.ParentID == nil {
			break
		}
		currentID = *asset.ParentID
	}

	path := ""
	for i, seg := range pathSegments {
		if i > 0 {
			path += "/"
		}
		path += seg
	}
	return path, nil
}

// GetFileCount 获取文件夹下的直接子文件数量
func (r *AssetRepository) GetFileCount(folderID uint) (int64, error) {
	var count int64
	err := r.db.Model(&Asset{}).Where("parent_id = ? AND type = ?", folderID, "file").Count(&count).Error
	return count, err
}

// GetFileCountRecursive 递归获取文件夹下的所有文件数量���包括子文件夹中的文件）
func (r *AssetRepository) GetFileCountRecursive(folderID uint) (int64, error) {
	var count int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		return getFileCountRecursive(tx, folderID, &count)
	})
	return count, err
}

// getFileCountRecursive 递归统计文件数量的辅助函数
func getFileCountRecursive(tx *gorm.DB, folderID uint, count *int64) error {
	// 统计当前文件夹下的直接子文件数量
	var fileCount int64
	if err := tx.Model(&Asset{}).Where("parent_id = ? AND type = ?", folderID, "file").Count(&fileCount).Error; err != nil {
		return err
	}
	*count += fileCount

	// 获取子文件夹并递归统计
	var subFolders []Asset
	if err := tx.Where("parent_id = ? AND type = ?", folderID, "folder").Find(&subFolders).Error; err != nil {
		return err
	}

	for _, folder := range subFolders {
		if err := getFileCountRecursive(tx, folder.ID, count); err != nil {
			return err
		}
	}

	return nil
}

// GetSubFolderCount 获取文件夹下的子文件夹数量
func (r *AssetRepository) GetSubFolderCount(folderID uint) (int64, error) {
	var count int64
	err := r.db.Model(&Asset{}).Where("parent_id = ? AND type = ?", folderID, "folder").Count(&count).Error
	return count, err
}

// ValidateParent 验证父目录是否有效（不能将文件夹移动到自己或自己的子目录下）
func (r *AssetRepository) ValidateParent(assetID uint, newParentID *uint) error {
	if newParentID == nil {
		return nil // 移动到根目录总是有效的
	}

	// 不能移动到自己
	if assetID == *newParentID {
		return fmt.Errorf("不能将资源移动到自身")
	}

	// 检查新父目录是否是当前资源的子资源
	children, err := r.GetAllChildrenRecursive(assetID)
	if err != nil {
		return err
	}

	for _, child := range children {
		if child.ID == *newParentID {
			return fmt.Errorf("不能将资源移动到其子目录中")
		}
	}

	return nil
}

// UpdateSortOrder 更新排序顺序
func (r *AssetRepository) UpdateSortOrder(id uint, sortOrder int) error {
	return r.db.Model(&Asset{}).Where("id = ?", id).Update("sort_order", sortOrder).Error
}

// BatchUpdateSortOrder 批量更新排序顺序
func (r *AssetRepository) BatchUpdateSortOrder(orders map[uint]int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for id, order := range orders {
			if err := tx.Model(&Asset{}).Where("id = ?", id).Update("sort_order", order).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
