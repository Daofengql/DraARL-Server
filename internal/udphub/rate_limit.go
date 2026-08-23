package udphub

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// 分片限速器：降低每包全局 Mutex 竞争。
const rateLimitShardCount = 32

type rateLimitShard struct {
	mu            sync.Mutex
	entries       map[rateLimitKey]*rateLimitEntry
	order         []rateLimitKey
	orderIndex    map[rateLimitKey]int
	cleanupCursor int
}

type rateLimitKey struct {
	addr netip.Addr
	port uint16
}

var rateLimitShards [rateLimitShardCount]rateLimitShard

func init() {
	for i := 0; i < rateLimitShardCount; i++ {
		rateLimitShards[i].entries = make(map[rateLimitKey]*rateLimitEntry)
		rateLimitShards[i].orderIndex = make(map[rateLimitKey]int)
	}
}

func rateLimitShardIndex(key rateLimitKey) int {
	return int(hashAddrPort(netip.AddrPortFrom(key.addr, key.port)) & (rateLimitShardCount - 1))
}

// checkRateLimit 检查 IP 粗限速和 IP+Port 细限速。
// 返回 true 表示允许通过，false 表示超限应丢弃。
func checkRateLimit(addr *net.UDPAddr) bool {
	ap, ok := udpAddrPort(addr)
	if !ok {
		return true
	}
	if !checkRateLimitKey(rateLimitKey{addr: ap.Addr()}, rateLimitMaxPps*4) {
		return false
	}
	return checkRateLimitKey(rateLimitKey{addr: ap.Addr(), port: ap.Port()}, rateLimitMaxPps)
}

func checkRateLimitKey(key rateLimitKey, maxPPS int) bool {
	return checkRateLimitKeyAt(key, maxPPS, time.Now())
}

// checkRateLimitKeyAt applies a token bucket with a one-second burst capacity.
// Unlike a Unix-second counter, traffic cannot reset at a wall-clock boundary
// and immediately consume a second full quota again.
func checkRateLimitKeyAt(key rateLimitKey, maxPPS int, now time.Time) bool {
	if maxPPS <= 0 {
		return false
	}
	shard := &rateLimitShards[rateLimitShardIndex(key)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.entries[key]
	capacity := float64(maxPPS)
	if !exists {
		shard.entries[key] = &rateLimitEntry{tokens: capacity - 1, lastRefill: now}
		shard.orderIndex[key] = len(shard.order)
		shard.order = append(shard.order, key)
		return true
	}
	elapsed := now.Sub(entry.lastRefill).Seconds()
	if elapsed > 0 {
		entry.tokens += elapsed * capacity
		if entry.tokens > capacity {
			entry.tokens = capacity
		}
		entry.lastRefill = now
	}
	if entry.tokens < 1 {
		return false
	}
	entry.tokens--
	return true
}

// rateLimitCleanupMaxPerShard 每次清理每分片最多扫描的条目数。
// 【性能修复】有界清理避免大表时整表遍历长时间持锁阻塞数据面热路径；
// 多次 10s tick 自然收敛，条目 TTL 5s 保证在稳态下 map 有界。
const rateLimitCleanupMaxPerShard = 1024

// deleteRateLimitEntryLocked removes a key from the ordered cleanup index in
// O(1). The caller must hold shard.mu. Swapping with the last key keeps the
// index compact without making packet admission pay for a slice shift.
func deleteRateLimitEntryLocked(shard *rateLimitShard, key rateLimitKey) {
	delete(shard.entries, key)
	idx, ok := shard.orderIndex[key]
	if !ok {
		return
	}
	last := len(shard.order) - 1
	if idx != last {
		lastKey := shard.order[last]
		shard.order[idx] = lastKey
		shard.orderIndex[lastKey] = idx
	}
	shard.order = shard.order[:last]
	delete(shard.orderIndex, key)
	if shard.cleanupCursor > idx {
		shard.cleanupCursor--
	}
	if shard.cleanupCursor >= len(shard.order) {
		shard.cleanupCursor = 0
	}
}

// cleanupRateLimiter 定期清理过期条目（每次持锁扫描有界且可持续推进）。
func cleanupRateLimiter() {
	now := time.Now()
	for i := 0; i < rateLimitShardCount; i++ {
		shard := &rateLimitShards[i]
		scanned := 0
		shard.mu.Lock()
		for scanned < rateLimitCleanupMaxPerShard && len(shard.order) > 0 {
			if shard.cleanupCursor >= len(shard.order) {
				shard.cleanupCursor = 0
			}
			key := shard.order[shard.cleanupCursor]
			entry, exists := shard.entries[key]
			if !exists {
				deleteRateLimitEntryLocked(shard, key)
				continue
			}
			scanned++
			if now.Sub(entry.lastRefill) > 5*time.Second {
				deleteRateLimitEntryLocked(shard, key)
				continue
			}
			shard.cleanupCursor++
		}
		shard.mu.Unlock()
	}
}
