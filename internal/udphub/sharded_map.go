package udphub

import (
	"hash/fnv"
	"sync"
	"time"
)

// ==========================================
// 性能优化：分片锁实现
// 将全局锁拆分为多个分片，减少锁竞争
// ==========================================

// ShardCount 分片数量（必须是 2 的幂次）
const ShardCount = 16

// ShardedAuthMap 分片认证失败记录 Map
type ShardedAuthMap struct {
	shards [ShardCount]struct {
		sync.RWMutex
		m map[string]*AuthFailure
	}
}

// NewShardedAuthMap 创建分片认证失败记录 map
func NewShardedAuthMap() *ShardedAuthMap {
	m := &ShardedAuthMap{}
	for i := 0; i < ShardCount; i++ {
		m.shards[i].m = make(map[string]*AuthFailure)
	}
	return m
}

// getShard 根据 key 计算分片索引
func (m *ShardedAuthMap) getShard(key string) int {
	h := fnv32String(key)
	return int(h) % ShardCount
}

// Get 获取认证失败记录
func (m *ShardedAuthMap) Get(key string) (*AuthFailure, bool) {
	shard := m.getShard(key)
	m.shards[shard].RLock()
	defer m.shards[shard].RUnlock()

	failure, exists := m.shards[shard].m[key]
	if !exists {
		return nil, false
	}
	return failure, true
}

// Set 设置认证失败记录
func (m *ShardedAuthMap) Set(key string, value *AuthFailure) {
	shard := m.getShard(key)
	m.shards[shard].Lock()
	defer m.shards[shard].Unlock()
	m.shards[shard].m[key] = value
}

// Delete 删除认证失败记录
func (m *ShardedAuthMap) Delete(key string) {
	shard := m.getShard(key)
	m.shards[shard].Lock()
	defer m.shards[shard].Unlock()
	delete(m.shards[shard].m, key)
}

// Range 遍历所有记录
func (m *ShardedAuthMap) Range(f func(key string, value *AuthFailure) bool) {
	for i := 0; i < ShardCount; i++ {
		m.shards[i].RLock()
		for k, v := range m.shards[i].m {
			if !f(k, v) {
				m.shards[i].RUnlock()
				return
			}
		}
		m.shards[i].RUnlock()
	}
}

// CleanExpired 清理过期记录
func (m *ShardedAuthMap) CleanExpired(now time.Time) int {
	count := 0
	for i := 0; i < ShardCount; i++ {
		m.shards[i].Lock()
		for key, failure := range m.shards[i].m {
			// 如果封禁已过期且超过 5 分钟没有新的失败，删除记录
			if !failure.BlockedUntil.IsZero() && now.After(failure.BlockedUntil.Add(5*time.Minute)) {
				delete(m.shards[i].m, key)
				count++
			}
		}
		m.shards[i].Unlock()
	}
	return count
}

// Len 获取记录数量
func (m *ShardedAuthMap) Len() int {
	count := 0
	for i := 0; i < ShardCount; i++ {
		m.shards[i].RLock()
		count += len(m.shards[i].m)
		m.shards[i].RUnlock()
	}
	return count
}

// fnv32String FNV-32 字符串哈希函数
func fnv32String(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}
