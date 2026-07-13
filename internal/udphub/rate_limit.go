package udphub

import (
	"sync"
	"time"
)

// 分片限速器：降低每包全局 Mutex 竞争。
const rateLimitShardCount = 32

type rateLimitShard struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}

var rateLimitShards [rateLimitShardCount]rateLimitShard

func init() {
	for i := 0; i < rateLimitShardCount; i++ {
		rateLimitShards[i].entries = make(map[string]*rateLimitEntry)
	}
}

func rateLimitShardIndex(addr string) int {
	return int(fnv32String(addr) & (rateLimitShardCount - 1))
}

// checkRateLimit 检查 IP+Port 的包速率。
// 返回 true 表示允许通过，false 表示超限应丢弃。
func checkRateLimit(addr string) bool {
	now := time.Now().Unix()
	shard := &rateLimitShards[rateLimitShardIndex(addr)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.entries[addr]
	if !exists || entry.timestamp != now {
		shard.entries[addr] = &rateLimitEntry{count: 1, timestamp: now}
		return true
	}
	if entry.count >= rateLimitMaxPps {
		return false
	}
	entry.count++
	return true
}

// cleanupRateLimiter 定期清理过期条目。
func cleanupRateLimiter() {
	now := time.Now().Unix()
	for i := 0; i < rateLimitShardCount; i++ {
		shard := &rateLimitShards[i]
		shard.mu.Lock()
		for addr, entry := range shard.entries {
			if now-entry.timestamp > 5 {
				delete(shard.entries, addr)
			}
		}
		shard.mu.Unlock()
	}
}
