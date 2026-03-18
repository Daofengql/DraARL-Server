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

// Cache 两级缓存接口
type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Clear(ctx context.Context) error
	// DeletePrefix 按前缀删除缓存，用于列表等动态Key的批量失效
	DeletePrefix(ctx context.Context, prefix string) error
}

// TwoLevelCache 两级缓存
// L1: 本地内存缓存
// L2: 数据库
type TwoLevelCache struct {
	local  *localCache
	config CacheConfig
}

// CacheConfig 缓存配置
type CacheConfig struct {
	// 本地缓存配置
	LocalTTL time.Duration
	MaxSize  int
}

// NewTwoLevelCache 创建两级缓存
func NewTwoLevelCache(config CacheConfig) (*TwoLevelCache, error) {
	cache := &TwoLevelCache{
		config: config,
		local:  newLocalCache(config.MaxSize),
	}

	return cache, nil
}

// Get 获取缓存 (两级缓存查找)
func (c *TwoLevelCache) Get(ctx context.Context, key string, dest interface{}) error {
	// L1: 本地缓存
	if c.local.Get(key, dest) {
		return nil
	}

	// L2: 缓存未命中，需要从数据库加载
	return ErrCacheMiss
}

// Set 设置缓存 (写入L1)
// 性能优化：添加 TTL 抖动防止缓存雪崩，使用缓冲区池减少内存分配
func (c *TwoLevelCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
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

	return nil
}

// Delete 删除缓存
func (c *TwoLevelCache) Delete(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		c.local.Delete(key)
	}
	return nil
}

// Clear 清空所有缓存
func (c *TwoLevelCache) Clear(ctx context.Context) error {
	c.local.Clear()
	return nil
}

// DeletePrefix 按前缀删除缓存
func (c *TwoLevelCache) DeletePrefix(ctx context.Context, prefix string) error {
	c.local.DeletePrefix(prefix)
	return nil
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
