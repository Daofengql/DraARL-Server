package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "draarl/internal/gormdb"
)

// ConfigCache 系统配置缓存管理器
type ConfigCache struct {
	cache *TwoLevelCache
}

// ConfigCacheConfig 配置缓存配置
type ConfigCacheConfig struct {
	LocalTTL time.Duration // 默认 5 分钟
	MaxSize  int           // 默认 1000
}

// NewConfigCache 创建配置缓存管理器
func NewConfigCache(config ConfigCacheConfig) (*ConfigCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = 5 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 1000
	}

	cache, err := NewTwoLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
	})
	if err != nil {
		return nil, err
	}

	return &ConfigCache{cache: cache}, nil
}

// 缓存键生成函数

// configKey 通用配置缓存键
func configKey(key string) string {
	return fmt.Sprintf("config:system:%s", key)
}

// configByCategoryKey 按分类的配置缓存键
func configByCategoryKey(category string) string {
	return fmt.Sprintf("config:category:%s", category)
}

// icpConfigKey ICP配置缓存键
func icpConfigKey() string {
	return "config:system:icp"
}

// systemInfoConfigKey 系统信息配置缓存键
func systemInfoConfigKey() string {
	return "config:system:info"
}

// aprsConfigKey APRS配置缓存键
func aprsConfigKey() string {
	return "config:system:aprs"
}

// openAIConfigKey OpenAI配置缓存键
func openAIConfigKey() string {
	return "config:system:openai"
}

// allConfigsKey 所有配置缓存键
func allConfigsKey() string {
	return "config:all"
}

// GetConfigByKey 根据key获取单个配置（带缓存）
func (c *ConfigCache) GetConfigByKey(ctx context.Context, key string) (*gormdb.SiteConfig, error) {
	cacheKey := configKey(key)

	var config gormdb.SiteConfig
	if err := c.cache.Get(ctx, cacheKey, &config); err == nil {
		return &config, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfig, err := repo.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if dbConfig == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, cacheKey, dbConfig, 0)

	return dbConfig, nil
}

// GetConfigsByCategory 按分类获取配置（带缓存）
func (c *ConfigCache) GetConfigsByCategory(ctx context.Context, category string) ([]gormdb.SiteConfig, error) {
	key := configByCategoryKey(category)

	var configs []gormdb.SiteConfig
	if err := c.cache.Get(ctx, key, &configs); err == nil {
		return configs, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfigs, err := repo.GetByCategory(category)
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbConfigs == nil {
		dbConfigs = make([]gormdb.SiteConfig, 0)
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfigs, 0)

	return dbConfigs, nil
}

// GetICPConfig 获取ICP配置（带缓存）
func (c *ConfigCache) GetICPConfig(ctx context.Context) (*gormdb.ICPConfig, error) {
	key := icpConfigKey()

	var config gormdb.ICPConfig
	if err := c.cache.Get(ctx, key, &config); err == nil {
		return &config, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfig, err := repo.GetICPConfig()
	if err != nil {
		return nil, err
	}
	if dbConfig == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfig, 0)

	return dbConfig, nil
}

// GetSystemInfoConfig 获取系统信息配置（带缓存）
func (c *ConfigCache) GetSystemInfoConfig(ctx context.Context) (*gormdb.SystemInfoConfig, error) {
	key := systemInfoConfigKey()

	var config gormdb.SystemInfoConfig
	if err := c.cache.Get(ctx, key, &config); err == nil {
		return &config, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfig, err := repo.GetSystemInfoConfig()
	if err != nil {
		return nil, err
	}
	if dbConfig == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfig, 0)

	return dbConfig, nil
}

// GetAPRSConfig 获取APRS配置（带缓存）
func (c *ConfigCache) GetAPRSConfig(ctx context.Context) (*gormdb.APRSConfig, error) {
	key := aprsConfigKey()

	var config gormdb.APRSConfig
	if err := c.cache.Get(ctx, key, &config); err == nil {
		return &config, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfig, err := repo.GetAPRSConfig()
	if err != nil {
		return nil, err
	}
	if dbConfig == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfig, 0)

	return dbConfig, nil
}

// GetOpenAIConfig 获取OpenAI配置（带缓存）
func (c *ConfigCache) GetOpenAIConfig(ctx context.Context) (*gormdb.OpenAIConfig, error) {
	key := openAIConfigKey()

	var config gormdb.OpenAIConfig
	if err := c.cache.Get(ctx, key, &config); err == nil {
		return &config, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfig, err := repo.GetOpenAIConfig()
	if err != nil {
		return nil, err
	}
	if dbConfig == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfig, 0)

	return dbConfig, nil
}

// GetAllConfigs 获取所有配置（带缓存）
func (c *ConfigCache) GetAllConfigs(ctx context.Context) ([]gormdb.SiteConfig, error) {
	key := allConfigsKey()

	var configs []gormdb.SiteConfig
	if err := c.cache.Get(ctx, key, &configs); err == nil {
		return configs, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.GetSiteConfigRepo()
	dbConfigs, err := repo.GetAll()
	if err != nil {
		return nil, err
	}

	// 缓存穿透保护
	if dbConfigs == nil {
		dbConfigs = make([]gormdb.SiteConfig, 0)
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbConfigs, 0)

	return dbConfigs, nil
}

// InvalidateConfig 使单个配置缓存失效
func (c *ConfigCache) InvalidateConfig(ctx context.Context, key string) error {
	return c.cache.Delete(ctx, configKey(key))
}

// InvalidateCategory 使分类配置缓存失效
func (c *ConfigCache) InvalidateCategory(ctx context.Context, category string) error {
	return c.cache.Delete(ctx, configByCategoryKey(category))
}

// InvalidateICPConfig 使ICP配置缓存失效
func (c *ConfigCache) InvalidateICPConfig(ctx context.Context) error {
	return c.cache.Delete(ctx, icpConfigKey())
}

// InvalidateSystemInfoConfig 使系统信息配置缓存失效
func (c *ConfigCache) InvalidateSystemInfoConfig(ctx context.Context) error {
	return c.cache.Delete(ctx, systemInfoConfigKey())
}

// InvalidateAPRSConfig 使APRS配置缓存失效
func (c *ConfigCache) InvalidateAPRSConfig(ctx context.Context) error {
	return c.cache.Delete(ctx, aprsConfigKey())
}

// InvalidateOpenAIConfig 使OpenAI配置缓存失效
func (c *ConfigCache) InvalidateOpenAIConfig(ctx context.Context) error {
	return c.cache.Delete(ctx, openAIConfigKey())
}

// InvalidateAll 使所有配置缓存失效
func (c *ConfigCache) InvalidateAll(ctx context.Context) error {
	return c.cache.Delete(ctx,
		allConfigsKey(),
		icpConfigKey(),
		systemInfoConfigKey(),
		aprsConfigKey(),
		openAIConfigKey(),
	)
}

// GetCache 获取底层缓存接口（用于特殊操作）
func (c *ConfigCache) GetCache() *TwoLevelCache {
	return c.cache
}
