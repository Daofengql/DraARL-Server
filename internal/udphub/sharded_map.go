package udphub

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

// ==========================================
// 性能优化：分片锁实现
// 将全局锁拆分为多个分片，减少锁竞争
// ==========================================

// ShardCount 分片数量（必须是 2 的幂次）
const ShardCount = 16

const defaultShardedAuthMapMaxEntries = 100_000
const maxAuthMapEvictionScan = 128

type shardedAuthMapShard struct {
	sync.RWMutex
	m map[string]AuthFailure
}

// ShardedAuthMap 分片认证失败记录 Map
type ShardedAuthMap struct {
	shards     [ShardCount]shardedAuthMapShard
	maxEntries int64
	entryCount atomic.Int64
}

// NewShardedAuthMap 创建分片认证失败记录 map
func NewShardedAuthMap() *ShardedAuthMap {
	return NewShardedAuthMapWithLimit(defaultShardedAuthMapMaxEntries)
}

// NewShardedAuthMapWithLimit creates a bounded map. The limit applies to the
// sum of all shards, so an attacker cannot grow one hot shard without also
// consuming the global admission budget.
func NewShardedAuthMapWithLimit(maxEntries int) *ShardedAuthMap {
	if maxEntries < 1 {
		maxEntries = 1
	}
	m := &ShardedAuthMap{}
	m.maxEntries = int64(maxEntries)
	for i := 0; i < ShardCount; i++ {
		m.shards[i].m = make(map[string]AuthFailure)
	}
	return m
}

// getShard 根据 key 计算分片索引
func (m *ShardedAuthMap) getShard(key string) int {
	h := fnv32String(key)
	return int(h) % ShardCount
}

// Get returns an immutable snapshot of an authentication-failure record.
func (m *ShardedAuthMap) Get(key string) (AuthFailure, bool) {
	shard := m.getShard(key)
	m.shards[shard].RLock()
	defer m.shards[shard].RUnlock()

	failure, exists := m.shards[shard].m[key]
	return failure, exists
}

// Update applies a mutation while the owning shard is locked and returns a
// value snapshot. Callers must not retain mutable map-owned pointers.
func (m *ShardedAuthMap) Update(key string, mutate func(AuthFailure) AuthFailure) AuthFailure {
	failure, _ := m.UpdateBounded(key, mutate)
	return failure
}

// UpdateBounded applies a mutation while the owning shard is locked. A new
// key is admitted only while the global capacity budget has room; when full,
// an expired or no-longer-blocked oldest entry in a bounded scan is evicted
// first. Existing active entries are never evicted, so a full table cannot
// silently un-block a previously throttled identity; if no safe candidate is
// found within the scan budget, the new identity is rejected.
func (m *ShardedAuthMap) UpdateBounded(key string, mutate func(AuthFailure) AuthFailure) (AuthFailure, bool) {
	if mutate == nil {
		return AuthFailure{}, false
	}
	shard := m.getShard(key)
	m.shards[shard].Lock()
	defer m.shards[shard].Unlock()

	failure, exists := m.shards[shard].m[key]
	if !exists {
		if !m.admitNewLocked(shard, time.Now()) {
			return AuthFailure{}, false
		}
	}
	failure = mutate(failure)
	m.shards[shard].m[key] = failure
	return failure, true
}

// Set stores a value copy.
func (m *ShardedAuthMap) Set(key string, value AuthFailure) {
	shard := m.getShard(key)
	m.shards[shard].Lock()
	defer m.shards[shard].Unlock()
	if _, exists := m.shards[shard].m[key]; !exists && !m.admitNewLocked(shard, time.Now()) {
		return
	}
	m.shards[shard].m[key] = value
}

// Delete 删除认证失败记录
func (m *ShardedAuthMap) Delete(key string) {
	shard := m.getShard(key)
	m.shards[shard].Lock()
	defer m.shards[shard].Unlock()
	if _, exists := m.shards[shard].m[key]; exists {
		delete(m.shards[shard].m, key)
		m.entryCount.Add(-1)
	}
}

// admitNewLocked reserves one capacity slot for a new key. The caller must
// hold the target shard lock; eviction is restricted to that shard so the
// admission path never takes two shard locks in an order that could deadlock.
func (m *ShardedAuthMap) admitNewLocked(shard int, now time.Time) bool {
	for {
		count := m.entryCount.Load()
		if count < m.maxEntries && m.entryCount.CompareAndSwap(count, count+1) {
			return true
		}
		if count < m.maxEntries {
			continue
		}
		if !m.evictOneLocked(shard, now) {
			return false
		}
	}
}

func (m *ShardedAuthMap) evictOneLocked(shard int, now time.Time) bool {
	entries := m.shards[shard].m
	oldestKey := ""
	var oldestAt time.Time
	scanned := 0
	for key, failure := range entries {
		if scanned >= maxAuthMapEvictionScan {
			break
		}
		scanned++
		lastFailure := failure.LastFailureAt
		if lastFailure.IsZero() {
			lastFailure = failure.BlockedUntil
		}
		if !lastFailure.IsZero() && now.After(lastFailure.Add(10*time.Minute)) {
			delete(entries, key)
			m.entryCount.Add(-1)
			return true
		}
		if !failure.BlockedUntil.IsZero() && now.Before(failure.BlockedUntil) {
			continue
		}
		if oldestKey == "" || lastFailure.Before(oldestAt) {
			oldestKey, oldestAt = key, lastFailure
		}
	}
	if oldestKey == "" {
		return false
	}
	delete(entries, oldestKey)
	m.entryCount.Add(-1)
	return true
}

// Range iterates value snapshots while holding each shard read lock.
func (m *ShardedAuthMap) Range(f func(key string, value AuthFailure) bool) {
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
			// Both blocked and unblocked failures must expire. Otherwise an
			// attacker can retain one or two failures for unbounded usernames.
			lastFailure := failure.LastFailureAt
			if lastFailure.IsZero() {
				lastFailure = failure.BlockedUntil
			}
			if !lastFailure.IsZero() && now.After(lastFailure.Add(10*time.Minute)) {
				delete(m.shards[i].m, key)
				m.entryCount.Add(-1)
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
