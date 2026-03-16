package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "nrllink/internal/gormdb"
)

// AssetCache 资源缓存管理器
type AssetCache struct {
	cache *ThreeLevelCache
}

// AssetCacheConfig 资源缓存配置
type AssetCacheConfig struct {
	// L1 本地缓存配置
	LocalTTL time.Duration // 默认 1 分钟
	MaxSize  int           // 默认 5000

	// L2 Redis 缓存配置
	RedisTTL time.Duration // 默认 10 分钟
}

// NewAssetCache 创建资源缓存管理器
func NewAssetCache(config AssetCacheConfig) (*AssetCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = time.Minute
	}
	if config.RedisTTL == 0 {
		config.RedisTTL = 10 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 5000
	}

	cache, err := NewThreeLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
		RedisTTL: config.RedisTTL,
	})
	if err != nil {
		return nil, err
	}

	return &AssetCache{cache: cache}, nil
}

// 缓存键生成函数

// assetTreeKey 资源目录树缓存键
func assetTreeKey() string {
	return "asset:tree"
}

// assetFolderKey 文件夹内容缓存键
func assetFolderKey(folderID uint) string {
	return fmt.Sprintf("asset:folder:%d", folderID)
}

// AssetTreeItem 资源目录树项（用于缓存）
type AssetTreeItem struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Remark    string `json:"remark"`
	SortOrder int    `json:"sort_order"`
	FileCount int64  `json:"file_count"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// AssetFolderItem 文件夹内容项（用于缓存）
type AssetFolderItem struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"` // 文件路径（仅文件类型有）
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	Remark    string `json:"remark"`
	SortOrder int    `json:"sort_order"`
	FileCount int64  `json:"file_count,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// GetAssetTree 获取资源目录树（带缓存）
func (c *AssetCache) GetAssetTree(ctx context.Context) ([]AssetTreeItem, error) {
	key := assetTreeKey()

	var items []AssetTreeItem
	if err := c.cache.Get(ctx, key, &items); err == nil {
		return items, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetAssetRepo()
	folders, err := repo.GetRootFolders()
	if err != nil {
		return nil, err
	}

	// 转换为缓存格式
	items = make([]AssetTreeItem, 0, len(folders))
	for _, folder := range folders {
		fileCount, _ := repo.GetFileCountRecursive(folder.ID)
		items = append(items, AssetTreeItem{
			ID:        folder.ID,
			Name:      folder.Name,
			Type:      folder.Type,
			Remark:    folder.Remark,
			SortOrder: folder.SortOrder,
			FileCount: fileCount,
			CreatedAt: folder.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: folder.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, items, 0)

	return items, nil
}

// GetFolderFiles 获取文件夹内容（带缓存）
func (c *AssetCache) GetFolderFiles(ctx context.Context, folderID uint) ([]AssetFolderItem, error) {
	key := assetFolderKey(folderID)

	var items []AssetFolderItem
	if err := c.cache.Get(ctx, key, &items); err == nil {
		return items, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetAssetRepo()
	children, err := repo.GetChildrenByParentID(folderID)
	if err != nil {
		return nil, err
	}

	// 转换为缓存格式
	items = make([]AssetFolderItem, 0, len(children))
	for _, child := range children {
		item := AssetFolderItem{
			ID:        child.ID,
			Name:      child.Name,
			Type:      child.Type,
			Path:      child.Path,
			Size:      child.Size,
			MimeType:  child.MimeType,
			Remark:    child.Remark,
			SortOrder: child.SortOrder,
			CreatedAt: child.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: child.UpdatedAt.Format("2006-01-02 15:04:05"),
		}

		// 如果是文件夹，递归获取子文件数量
		if child.IsFolder() {
			fileCount, _ := repo.GetFileCountRecursive(child.ID)
			item.FileCount = fileCount
		}

		items = append(items, item)
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, items, 0)

	return items, nil
}

// InvalidateTree 使资源目录树缓存失效
func (c *AssetCache) InvalidateTree(ctx context.Context) error {
	return c.cache.Delete(ctx, assetTreeKey())
}

// InvalidateFolder 使文件夹缓存失效
func (c *AssetCache) InvalidateFolder(ctx context.Context, folderID uint) error {
	return c.cache.Delete(ctx, assetFolderKey(folderID))
}

// InvalidateAll 使所有资源缓存失效
func (c *AssetCache) InvalidateAll(ctx context.Context) error {
	// 删除目录树缓存
	if err := c.cache.Delete(ctx, assetTreeKey()); err != nil {
		return err
	}
	// 删除所有文件夹缓存（使用前缀删除）
	return c.cache.DeletePrefix(ctx, "asset:folder:")
}

// GetCache 获取底层缓存接口（用于特殊操作）
func (c *AssetCache) GetCache() *ThreeLevelCache {
	return c.cache
}
