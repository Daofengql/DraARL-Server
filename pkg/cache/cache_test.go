package cache

import (
	"context"
	"testing"
	"time"
)

func TestTwoLevelCacheDeepCopy(t *testing.T) {
	c, err := NewTwoLevelCache(CacheConfig{LocalTTL: time.Minute, MaxSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// 写入大量条目，使 bufferPool 复用同一底层缓冲
	payload := map[string]interface{}{"n": 1, "s": "first"}
	if err := c.Set(ctx, "k1", payload, time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if err := c.Set(ctx, "fill", map[string]interface{}{"i": i}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	// k1 必须仍为首次写入的内容，而不是被后续 Set 覆盖
	var got map[string]interface{}
	if err := c.Get(ctx, "k1", &got); err != nil {
		t.Fatalf("k1 lost: %v", err)
	}
	if int(got["n"].(float64)) != 1 || got["s"] != "first" {
		t.Fatalf("k1 polluted: %#v", got)
	}
}

func TestLocalCacheLRUEviction(t *testing.T) {
	lc := newLocalCache(3)
	lc.Set("a", []byte("1"), time.Minute)
	lc.Set("b", []byte("2"), time.Minute)
	lc.Set("c", []byte("3"), time.Minute)

	// 访问 a 使其成为最新
	var v []byte
	if !lc.Get("a", &v) {
		t.Fatal("a should be cached")
	}

	// 插入 d 触发淘汰：最久未使用的是 b
	lc.Set("d", []byte("4"), time.Minute)
	if lc.Get("b", &v) {
		t.Fatal("b should have been evicted")
	}
	for _, key := range []string{"a", "c", "d"} {
		if !lc.Get(key, &v) {
			t.Fatalf("%s should be cached", key)
		}
	}
	if len(lc.items) != 3 {
		t.Fatalf("items len = %d, want 3", len(lc.items))
	}
}

func TestLocalCacheLazyExpiry(t *testing.T) {
	lc := newLocalCache(10)
	lc.Set("expired", []byte("1"), 1*time.Millisecond)
	lc.Set("fresh", []byte("2"), time.Minute)
	time.Sleep(5 * time.Millisecond)

	var v []byte
	if lc.Get("expired", &v) {
		t.Fatal("expired item should miss")
	}
	if _, exists := lc.items["expired"]; exists {
		t.Fatal("expired item should be lazily deleted")
	}
	if !lc.Get("fresh", &v) {
		t.Fatal("fresh item should hit")
	}
}

func TestLocalCacheDeletePrefixKeepsLRUConsistent(t *testing.T) {
	lc := newLocalCache(10)
	for i := 0; i < 5; i++ {
		key := "user:info:" + string(rune('a'+i))
		lc.Set(key, []byte("x"), time.Minute)
	}
	lc.Set("other", []byte("y"), time.Minute)
	lc.DeletePrefix("user:")
	if len(lc.items) != 1 {
		t.Fatalf("items len = %d, want 1", len(lc.items))
	}
	var v []byte
	if !lc.Get("other", &v) {
		t.Fatal("other should survive prefix delete")
	}
	if lc.Get("user:info:a", &v) {
		t.Fatal("prefixed key should be gone")
	}
}
