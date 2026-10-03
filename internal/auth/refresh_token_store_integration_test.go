package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/config"
	"github.com/redis/go-redis/v9"
)

func redisRefreshTestStores(t *testing.T) (*redisRefreshTokenStore, *redisRefreshTokenStore) {
	t.Helper()
	addr := os.Getenv("DRAARL_REDIS_ADDR")
	if addr == "" {
		t.Skip("set DRAARL_REDIS_ADDR for real Redis integration tests")
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Configuration{}
	cfg.Redis.Host, cfg.Redis.Port = host, port
	cfg.Redis.Password = os.Getenv("DRAARL_REDIS_PASSWORD")
	cfg.Redis.DB, _ = strconv.Atoi(os.Getenv("DRAARL_REDIS_DB"))
	cfg.Redis.Prefix = fmt.Sprintf("draarl-refresh-review-%d", time.Now().UnixNano())
	cfg.Redis.DialTimeoutSec, cfg.Redis.ReadTimeoutSec, cfg.Redis.WriteTimeoutSec, cfg.Redis.PoolSize = 2, 2, 2, 8
	first, err := newRedisRefreshTokenStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newRedisRefreshTokenStore(cfg)
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var cursor uint64
		for {
			keys, next, err := first.client.Scan(ctx, cursor, first.prefix+":*", 100).Result()
			if err != nil {
				t.Errorf("cleanup: %v", err)
				break
			}
			if len(keys) > 0 {
				if err := first.client.Del(ctx, keys...).Err(); err != nil {
					t.Errorf("cleanup: %v", err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
		_ = first.Close()
		_ = second.Close()
	})
	return first, second
}

// Inject a second instance's mutation after the revoker has read its snapshot.
type refreshSnapshotHook struct {
	once   atomic.Bool
	mutate func()
}

func (h *refreshSnapshotHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (h *refreshSnapshotHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *refreshSnapshotHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		err := next(ctx, commands)
		if err == nil && len(commands) > 0 && commands[0].Name() == "hgetall" && h.once.CompareAndSwap(false, true) {
			h.mutate()
		}
		return err
	}
}

func TestRedisRefreshRevocationIncludesConcurrentRotation(t *testing.T) {
	for _, mutation := range []string{"rotation", "creation"} {
		t.Run(mutation, func(t *testing.T) {
			first, second := redisRefreshTestStores(t)
			now := time.Now().Truncate(time.Second)
			old := &RefreshTokenRecord{UserID: 101, SessionVersion: 2, TokenHash: "old", ExpiresAt: now.Add(time.Hour)}
			if err := first.Create(old); err != nil {
				t.Fatal(err)
			}
			next := &RefreshTokenRecord{UserID: 101, SessionVersion: 2, TokenHash: "new", ExpiresAt: now.Add(2 * time.Hour)}
			hook := &refreshSnapshotHook{mutate: func() {
				var err error
				if mutation == "rotation" {
					err = second.Rotate("old", next, "rotated", now)
				} else {
					err = second.Create(next)
				}
				if err != nil {
					t.Errorf("concurrent %s: %v", mutation, err)
				}
			}}
			first.client.AddHook(hook)
			if err := first.RevokeAllByUser(101, "reuse_detected", now); err != nil {
				t.Fatal(err)
			}
			if !hook.once.Load() {
				t.Fatal("concurrent mutation did not run")
			}
			for _, hash := range []string{"old", "new"} {
				record, err := second.GetByTokenHash(hash)
				if err != nil || record == nil || record.RevokedAt == nil {
					t.Fatalf("session %s survived revocation: record=%+v err=%v", hash, record, err)
				}
			}
		})
	}
}

func TestRedisRefreshUserIndexKeepsLongestRetention(t *testing.T) {
	store, _ := redisRefreshTestStores(t)
	now := time.Now().Truncate(time.Second)
	long := &RefreshTokenRecord{UserID: 102, TokenHash: "long", ExpiresAt: now.Add(14 * 24 * time.Hour)}
	short := &RefreshTokenRecord{UserID: 102, TokenHash: "short", ExpiresAt: now.Add(time.Hour)}
	if err := store.Create(long); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(short); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	check := func() {
		t.Helper()
		ttl, err := store.client.PTTL(ctx, store.userSetKey(102)).Result()
		if err != nil || ttl < 14*24*time.Hour {
			t.Fatalf("index TTL shortened: %v, %v", ttl, err)
		}
	}
	check()
	if err := store.RevokeByTokenHash("short", "logout", now); err != nil {
		t.Fatal(err)
	}
	check()
	if err := store.RevokeAllByUser(102, "reuse_detected", now); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestRedisRefreshRotationHasSingleWinnerAndValidatesSession(t *testing.T) {
	first, second := redisRefreshTestStores(t)
	now := time.Now().Truncate(time.Second)
	old := &RefreshTokenRecord{UserID: 103, SessionVersion: 2, TokenHash: "old", ExpiresAt: now.Add(time.Hour)}
	if err := first.Create(old); err != nil {
		t.Fatal(err)
	}
	invalid := *old
	invalid.TokenHash = "invalid"
	invalid.SessionVersion = 3
	if err := first.Rotate("old", &invalid, "rotated", now); !errors.Is(err, ErrRefreshTokenNotActive) {
		t.Fatalf("changed version accepted: %v", err)
	}
	if err := first.Rotate("old", &invalid, "rotated", old.ExpiresAt); !errors.Is(err, ErrRefreshTokenNotActive) {
		t.Fatalf("expired token accepted: %v", err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := first
			if i%2 != 0 {
				store = second
			}
			next := *old
			next.TokenHash = fmt.Sprintf("new-%d", i)
			err := store.Rotate("old", &next, "rotated", now)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrRefreshTokenNotActive) {
				t.Errorf("rotate: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("rotation winners = %d", successes.Load())
	}
}
