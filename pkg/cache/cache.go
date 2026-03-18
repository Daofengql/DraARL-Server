package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	redispkg "nrllink/pkg/redis"
)

// 性能优化：TTL 抖动因子（防止缓存雪崩）
const ttlJitterFactor = 0.1 // ±10% 抖动

// 性能优化：JSON 编码缓冲区池，减少内存分配
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// jitterTTL 为 TTL 添加随机抖动，防止大量缓存同时失效导致雪崩
func jitterTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	// 生成 [-jitterFactor, +jitterFactor] 范围内的随机抖动
	jitter := time.Duration(rand.Float64() * 2 * ttlJitterFactor * float64(base))
	return base - time.Duration(ttlJitterFactor*float64(base)) + jitter
}

// Cache 三级缓存接口
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Clear(ctx context.Context) error
	// DeletePrefix 按前缀删除缓存，用于列表等动态Key的批量失效
	DeletePrefix(ctx context.Context, prefix string) error
}

// ThreeLevelCache 三级缓存
// L1: 本地内存缓存
// L2: Redis缓存
// L3: 数据库
type ThreeLevelCache struct {
	local *localCache
	redis *redis.Client
	config CacheConfig
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// 本地缓存配置
	LocalTTL time.Duration
	MaxSize  int

	// Redis缓存配置
	RedisTTL time.Duration
}

// NewThreeLevelCache 创建三级缓存
// Redis 是必需的，会自动使用全局 Redis 客户端
func NewThreeLevelCache(config CacheConfig) (*ThreeLevelCache, error) {
	cache := &ThreeLevelCache{
		config: config,
		local:  newLocalCache(config.MaxSize),
		redis:  redispkg.GetClient(),
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
	val, err := c.redis.Get(ctx, key).Bytes()
	if err == nil && len(val) > 0 {
		// 反序列化
		if err := json.Unmarshal(val, dest); err == nil {
			// 回写本地缓存
			c.local.Set(key, val, c.config.LocalTTL)
			return nil
		}
	}

	// L3: 缓存未命中，需要从数据库加载
	return ErrCacheMiss
}

// Set 设置缓存 (同时写入L1和L2)
// 性能优化：添加 TTL 抖动防止缓存雪崩，使用缓冲区池减少内存分配
func (c *ThreeLevelCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// 性能优化：使用缓冲区池进行 JSON 序列化
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	data := buf.Bytes()

	// 写入本地缓存 (L1) - 使用抖动后的 TTL
	localTTL := ttl
	if localTTL == 0 {
		localTTL = c.config.LocalTTL
	}
	localTTLWithJitter := jitterTTL(localTTL)
	c.local.Set(key, data, localTTLWithJitter)

	// 写入Redis (L2) - 使用抖动后的 TTL
	redisTTL := ttl
	if redisTTL == 0 {
		redisTTL = c.config.RedisTTL
	}
	redisTTLWithJitter := jitterTTL(redisTTL)
	return c.redis.Set(ctx, key, data, redisTTLWithJitter).Err()
}

// Delete 删除缓存 (同时删除L1和L2)
func (c *ThreeLevelCache) Delete(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		c.local.Delete(key)
	}

	return c.redis.Del(ctx, keys...).Err()
}

// Clear 清空所有缓存
func (c *ThreeLevelCache) Clear(ctx context.Context) error {
	c.local.Clear()
	// 注意：这会清除整个Redis DB，慎用
	return c.redis.FlushDB(ctx).Err()
}

// DeletePrefix 按前缀删除缓存 (同时清理L1和L2)
// 使用 Redis SCAN 命令迭代匹配前缀的 Key，避免 KEYS 命令阻塞 Redis
func (c *ThreeLevelCache) DeletePrefix(ctx context.Context, prefix string) error {
	// L1: 清理本地缓存
	c.local.DeletePrefix(prefix)

	// L2: 清理 Redis 缓存
	// 使用 SCAN 命令迭代，每次获取一批 keys 进行删除
	var cursor uint64
	for {
		var keys []string
		var err error
		// 每次扫描 1000 个元素
		keys, cursor, err = c.redis.Scan(ctx, cursor, prefix+"*", 1000).Result()
		if err != nil {
			return err
		}

		// 如果匹配到 key，则批量删除
		if len(keys) > 0 {
			if err := c.redis.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		// 游标返回 0 说明迭代结束
		if cursor == 0 {
			break
		}
	}
	return nil
}

// GetRedis 获取Redis客户端 (用于特殊操作)
func (c *ThreeLevelCache) GetRedis() *redis.Client {
	return c.redis
}

// localCache 本地内存缓存
type localCache struct {
	mu      sync.RWMutex
	items   map[string]*cacheItem
	lru     *lruList
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

// DeletePrefix 本地缓存按前缀删除
// 遍历 Map，发现前缀匹配的直接 delete
func (lc *localCache) DeletePrefix(prefix string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for key := range lc.items {
		if strings.HasPrefix(key, prefix) {
			delete(lc.items, key)
		}
	}
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
