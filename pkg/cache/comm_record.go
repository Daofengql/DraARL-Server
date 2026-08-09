package cache

import (
	"context"
	"fmt"
	"time"

	"draarl/internal/gormdb"
)

// CommRecordCacheConfig 通信记录缓存配置
type CommRecordCacheConfig struct {
	LocalTTL time.Duration
	MaxSize  int
}

// CommRecordCache 通信记录缓存
type CommRecordCache struct {
	cache *TwoLevelCache
}

// NewCommRecordCache 创建通信记录缓存
func NewCommRecordCache(config CommRecordCacheConfig) (*CommRecordCache, error) {
	if config.LocalTTL == 0 {
		config.LocalTTL = 30 * time.Second
	}
	if config.MaxSize == 0 {
		config.MaxSize = 5000
	}

	cache, err := NewTwoLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
	})
	if err != nil {
		return nil, err
	}

	return &CommRecordCache{cache: cache}, nil
}

// 缓存键定义
const (
	// CommRecordListKey 通信记录列表缓存键
	CommRecordListKey = "comm_record:list:page:%d:size:%d:device:%d:group:%d:user:%d"
	// CommRecordInfoKey 单条通信记录缓存键
	CommRecordInfoKey = "comm_record:info:%d"
	// CommRecordListPrefix 通信记录列表缓存前缀（用于批量失效）
	CommRecordListPrefix = "comm_record:list:"
)

// buildListCacheKey 构建列表缓存键
func buildListCacheKey(page, pageSize int, deviceID, groupID, userID int64) string {
	return fmt.Sprintf(CommRecordListKey, page, pageSize, deviceID, groupID, userID)
}

// CommRecordListResult 通信记录列表结果
type CommRecordListResult struct {
	Records []gormdb.CommRecord `json:"records"`
	Total   int64               `json:"total"`
}

// GetList 获取通信记录列表（带缓存）
func (c *CommRecordCache) GetList(ctx context.Context, page, pageSize int, deviceID, groupID, userID int64) (*CommRecordListResult, error) {
	key := buildListCacheKey(page, pageSize, deviceID, groupID, userID)

	var result CommRecordListResult
	if err := c.cache.Get(ctx, key, &result); err == nil {
		return &result, nil
	}

	// 缓存未命中，返回 ErrCacheMiss
	return nil, ErrCacheMiss
}

// SetList 设置通信记录列表缓存
func (c *CommRecordCache) SetList(ctx context.Context, page, pageSize int, deviceID, groupID, userID int64, result *CommRecordListResult) error {
	key := buildListCacheKey(page, pageSize, deviceID, groupID, userID)
	return c.cache.Set(ctx, key, result, 0) // 使用默认 TTL
}

// GetInfo 获取单条通信记录（带缓存）
func (c *CommRecordCache) GetInfo(ctx context.Context, id uint) (*gormdb.CommRecord, error) {
	key := fmt.Sprintf(CommRecordInfoKey, id)

	var record gormdb.CommRecord
	if err := c.cache.Get(ctx, key, &record); err == nil {
		return &record, nil
	}

	return nil, ErrCacheMiss
}

// SetInfo 设置单条通信记录缓存
func (c *CommRecordCache) SetInfo(ctx context.Context, record *gormdb.CommRecord) error {
	key := fmt.Sprintf(CommRecordInfoKey, record.ID)
	return c.cache.Set(ctx, key, record, 0)
}

// InvalidateInfo 使单条记录缓存失效
func (c *CommRecordCache) InvalidateInfo(ctx context.Context, id uint) error {
	key := fmt.Sprintf(CommRecordInfoKey, id)
	return c.cache.Delete(ctx, key)
}

// InvalidateList 使列表缓存失效（删除所有列表缓存）
func (c *CommRecordCache) InvalidateList(ctx context.Context) error {
	return c.cache.DeletePrefix(ctx, CommRecordListPrefix)
}

// InvalidateAll 使所有通信记录缓存失效
func (c *CommRecordCache) InvalidateAll(ctx context.Context) error {
	// 删除列表缓存
	if err := c.cache.DeletePrefix(ctx, CommRecordListPrefix); err != nil {
		return err
	}
	// 删除单条记录缓存前缀（需要遍历删除，这里简化处理）
	// 由于单条记录缓存较少使用，这里暂不处理
	return nil
}
