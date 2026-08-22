package udphub

import (
	"net/netip"
	"sync"
	"testing"
	"time"
)

func resetRateLimitTestState() {
	for i := range rateLimitShards {
		shard := &rateLimitShards[i]
		shard.mu.Lock()
		shard.entries = make(map[rateLimitKey]*rateLimitEntry)
		shard.order = nil
		shard.orderIndex = make(map[rateLimitKey]int)
		shard.cleanupCursor = 0
		shard.mu.Unlock()
	}
}

func TestRateLimitTokenBucketRejectsCrossBoundaryBurst(t *testing.T) {
	resetRateLimitTestState()
	t.Cleanup(resetRateLimitTestState)
	key := rateLimitKey{addr: netip.MustParseAddr("192.0.2.10"), port: 1234}
	start := time.Unix(100, 900_000_000)
	for i := 0; i < 10; i++ {
		if !checkRateLimitKeyAt(key, 10, start) {
			t.Fatalf("initial token %d was rejected", i+1)
		}
	}
	// A Unix-second counter would reset here and admit ten more packets in
	// 100ms. The token bucket has accumulated only one token.
	if !checkRateLimitKeyAt(key, 10, start.Add(100*time.Millisecond)) {
		t.Fatal("the one-token refill was rejected")
	}
	if checkRateLimitKeyAt(key, 10, start.Add(100*time.Millisecond)) {
		t.Fatal("cross-boundary burst admitted more than the refill")
	}
}

func TestRateLimitTokenBucketRefillsAtConfiguredRate(t *testing.T) {
	resetRateLimitTestState()
	t.Cleanup(resetRateLimitTestState)
	key := rateLimitKey{addr: netip.MustParseAddr("192.0.2.11"), port: 1235}
	start := time.Unix(200, 0)
	for i := 0; i < 10; i++ {
		if !checkRateLimitKeyAt(key, 10, start) {
			t.Fatalf("initial token %d was rejected", i+1)
		}
	}
	refillAt := start.Add(500 * time.Millisecond)
	for i := 0; i < 5; i++ {
		if !checkRateLimitKeyAt(key, 10, refillAt) {
			t.Fatalf("refilled token %d was rejected", i+1)
		}
	}
	if checkRateLimitKeyAt(key, 10, refillAt) {
		t.Fatal("more tokens than the half-second refill were admitted")
	}
	if !checkRateLimitKeyAt(key, 10, start.Add(time.Second)) {
		t.Fatal("full one-second refill was rejected")
	}
}

func TestRateLimitTokenBucketConcurrentAccess(t *testing.T) {
	resetRateLimitTestState()
	t.Cleanup(resetRateLimitTestState)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(port uint16) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				checkRateLimitKey(rateLimitKey{addr: netip.MustParseAddr("192.0.2.12"), port: port}, rateLimitMaxPps)
			}
		}(uint16(2000 + i))
	}
	wg.Wait()
}

func TestRateLimitCleanupAdvancesAcrossBoundedBatches(t *testing.T) {
	resetRateLimitTestState()
	t.Cleanup(resetRateLimitTestState)
	shard := &rateLimitShards[0]
	staleAt := time.Now().Add(-6 * time.Second)
	shard.mu.Lock()
	for i := 0; i < rateLimitCleanupMaxPerShard+1; i++ {
		key := rateLimitKey{addr: netip.MustParseAddr("198.51.100.1"), port: uint16(1000 + i)}
		shard.entries[key] = &rateLimitEntry{tokens: 1, lastRefill: staleAt}
		shard.orderIndex[key] = len(shard.order)
		shard.order = append(shard.order, key)
	}
	shard.mu.Unlock()

	cleanupRateLimiter()
	shard.mu.Lock()
	remainingAfterFirst := len(shard.entries)
	shard.mu.Unlock()
	if remainingAfterFirst != 1 {
		t.Fatalf("first bounded cleanup left %d entries, want 1", remainingAfterFirst)
	}
	cleanupRateLimiter()
	shard.mu.Lock()
	remainingAfterSecond := len(shard.entries)
	shard.mu.Unlock()
	if remainingAfterSecond != 0 {
		t.Fatalf("second cleanup left %d entries, want 0", remainingAfterSecond)
	}
}
