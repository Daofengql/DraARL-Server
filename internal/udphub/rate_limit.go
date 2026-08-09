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
	mu      sync.Mutex
	entries map[rateLimitKey]*rateLimitEntry
}

type rateLimitKey struct {
	addr netip.Addr
	port uint16
}

var rateLimitShards [rateLimitShardCount]rateLimitShard

func init() {
	for i := 0; i < rateLimitShardCount; i++ {
		rateLimitShards[i].entries = make(map[rateLimitKey]*rateLimitEntry)
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
	now := time.Now().Unix()
	shard := &rateLimitShards[rateLimitShardIndex(key)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.entries[key]
	if !exists || entry.timestamp != now {
		shard.entries[key] = &rateLimitEntry{count: 1, timestamp: now}
		return true
	}
	if entry.count >= maxPPS {
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
