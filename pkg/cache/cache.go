package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache 三级缓存接口
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Clear(ctx context.Context) error
}

// ThreeLevelCache 三级缓存
// L1: 本地内存缓存
// L2: Redis缓存
// L3: 数据库
type ThreeLevelCache struct {
	local  *localCache
	redis  *redis.Client
	config CacheConfig
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// 本地缓存配置
	LocalTTL  time.Duration
	MaxSize    int

	// Redis缓存配置
	RedisTTL   time.Duration
	RedisAddr  string
	RedisPass  string
	RedisDB    int
	PoolSize   int
	MinIdleCon int

	// 是否启用Redis
	RedisEnabled bool
}

// NewThreeLevelCache 创建三级缓存
func NewThreeLevelCache(config CacheConfig) (*ThreeLevelCache, error) {
	cache := &ThreeLevelCache{
		config: config,
		local:  newLocalCache(config.MaxSize),
	}

	// 初始化Redis客户端
	if config.RedisEnabled {
		cache.redis = redis.NewClient(&redis.Options{
			Addr:         config.RedisAddr,
			Password:     config.RedisPass,
			DB:           config.RedisDB,
			PoolSize:     config.PoolSize,
			MinIdleConns: config.MinIdleConn,
		})

		// 测试连接
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.redis.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("redis连接失败: %w", err)
		}
	}

	return cache, nil
}

// Get 获取缓存 (三级缓存查找)
func (c *ThreeLevelCache) Get(ctx context.Context, key string, dest interface{}) error {
	// L1: 本地缓存
	if c.local.Get(key, dest) {
		return nil
	}

	// L2: Redis缓存
	if c.redis != nil {
		val, err := c.redis.Get(ctx, key).Bytes()
		if err == nil && len(val) > 0 {
			// 反序列化
			if err := json.Unmarshal(val, dest); err == nil {
				// 回写本地缓存
				c.local.Set(key, dest, c.config.LocalTTL)
				return nil
			}
		}
	}

	// L3: 缓存未命中，需要从数据库加载
	return ErrCacheMiss
}

// Set 设置缓存 (同时写入L1和L2)
func (c *ThreeLevelCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// 序列化
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	// 写入本地缓存 (L1)
	localTTL := ttl
	if localTTL == 0 {
		localTTL = c.config.LocalTTL
	}
	c.local.Set(key, data, localTTL)

	// 写入Redis (L2)
	if c.redis != nil {
		redisTTL := ttl
		if redisTTL == 0 {
			redisTTL = c.config.RedisTTL
		}
		return c.redis.Set(ctx, key, data, redisTTL).Err()
	}

	return nil
}

// Delete 删除缓存 (同时删除L1和L2)
func (c *ThreeLevelCache) Delete(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		c.local.Delete(key)
	}

	if c.redis != nil {
		return c.redis.Del(ctx, keys...).Err()
	}

	return nil
}

// Clear 清空所有缓存
func (c *ThreeLevelCache) Clear(ctx context.Context) error {
	c.local.Clear()
	if c.redis != nil {
		// 注意：这会清除整个Redis DB，慎用
		return c.redis.FlushDB(ctx).Err()
	}
	return nil
}

// Close 关闭缓存连接
func (c *ThreeLevelCache) Close() error {
	if c.redis != nil {
		return c.redis.Close()
	}
	return nil
}

// GetRedis 获取Redis客户端 (用于特殊操作)
func (c *ThreeLevelCache) GetRedis() *redis.Client {
	return c.redis
}

// localCache 本地内存缓存
type localCache struct {
	mu    sync.RWMutex
	items map[string]*cacheItem
	lru   *lruList
	maxSize int
}

type cacheItem struct {
	data      interface{}
	expiredAt time.Time
}

type lruList struct {
	items []string
}

func newLocalCache(maxSize int) *localCache {
	return &localCache{
		items:   make(map[string]*cacheItem),
		lru:     &lruList{items: make([]string, 0, maxSize)},
		maxSize: maxSize,
	}
}

func (lc *localCache) Get(key string, dest interface{}) bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	item, ok := lc.items[key]
	if !ok {
		return false
	}

	// 检查是否过期
	if time.Now().After(item.expiredAt) {
		return false
	}

	// 类型断言
	switch v := dest.(type) {
	case *[]byte:
		if data, ok := item.data.([]byte); ok {
			*v = data
			return true
		}
	case *string:
		if str, ok := item.data.(string); ok {
			*v = str
			return true
		}
	default:
		// 尝试JSON反序列化
		if data, ok := item.data.([]byte); ok {
			return json.Unmarshal(data, dest) == nil
		}
	}

	return false
}

func (lc *localCache) Set(key string, value interface{}, ttl time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	expiredAt := time.Now().Add(ttl)
	if ttl == 0 {
		expiredAt = time.Now().Add(5 * time.Minute)
	}

	// 检查是否需要淘汰
	if len(lc.items) >= lc.maxSize {
		lc.evict()
	}

	lc.items[key] = &cacheItem{
		data:      value,
		expiredAt: expiredAt,
	}
}

func (lc *localCache) Delete(key string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.items, key)
}

func (lc *localCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.items = make(map[string]*cacheItem)
	lc.lru.items = make([]string, 0, lc.maxSize)
}

func (lc *localCache) evict() {
	if len(lc.lru.items) == 0 {
		return
	}
	// 简单的FIFO淘汰
	key := lc.lru.items[0]
	lc.lru.items = lc.lru.items[1:]
	delete(lc.items, key)
}

// ErrCacheMiss 缓存未命中错误
var ErrCacheMiss = fmt.Errorf("cache miss")
