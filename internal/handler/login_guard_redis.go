package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"

	"github.com/redis/go-redis/v9"
)

type sharedLoginGuard interface {
	CheckLock(kind, key string) (bool, time.Duration, error)
	RecordFailure(kind, key string, limit int, window, lockDuration time.Duration) error
	Clear(kind, key string) error
	Allow(kind, key string, limit int, window time.Duration) (bool, time.Duration, error)
	Close() error
}

type redisLoginGuard struct {
	client           *redis.Client
	prefix           string
	operationTimeout time.Duration
}

var (
	sharedLoginGuardMu sync.RWMutex
	activeLoginGuard   sharedLoginGuard
	loginGuardFactory  = newRedisLoginGuard
)

var loginFailureScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
if count >= tonumber(ARGV[1]) then
  redis.call('SET', KEYS[2], '1', 'PX', ARGV[3])
end
return count
`)

var fixedWindowScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('PTTL', KEYS[1])
return {count, ttl}
`)

func InitLoginGuardStore(cfg *config.Configuration) error {
	sharedLoginGuardMu.Lock()
	defer sharedLoginGuardMu.Unlock()
	if activeLoginGuard != nil {
		_ = activeLoginGuard.Close()
		activeLoginGuard = nil
	}
	store, err := loginGuardFactory(cfg)
	if err != nil {
		if config.IsReleaseBuild() {
			return fmt.Errorf("release build requires Redis login guard storage: %w", err)
		}
		log.Printf("[AUTH] Redis 登录/注册保护不可用，使用单进程内存保护: %v", err)
		return nil
	}
	activeLoginGuard = store
	log.Printf("[AUTH] 登录/注册保护已启用共享 Redis: %s", cfg.RedisAddr())
	return nil
}

func CloseLoginGuardStore() {
	sharedLoginGuardMu.Lock()
	defer sharedLoginGuardMu.Unlock()
	if activeLoginGuard != nil {
		_ = activeLoginGuard.Close()
		activeLoginGuard = nil
	}
}

func currentSharedLoginGuard() sharedLoginGuard {
	sharedLoginGuardMu.RLock()
	defer sharedLoginGuardMu.RUnlock()
	return activeLoginGuard
}

func newRedisLoginGuard(cfg *config.Configuration) (*redisLoginGuard, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is nil")
	}
	client := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr(), Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSec) * time.Second,
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSec) * time.Second,
		PoolSize:     cfg.Redis.PoolSize,
		// Enforce operationContext as a total deadline in addition to socket
		// read/write timeouts so Redis stalls fail closed within a known bound.
		ContextTimeoutEnabled: true,
	})
	operationTimeout := maxDuration(
		time.Duration(cfg.Redis.DialTimeoutSec)*time.Second,
		time.Duration(cfg.Redis.ReadTimeoutSec)*time.Second,
		time.Duration(cfg.Redis.WriteTimeoutSec)*time.Second,
	)
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &redisLoginGuard{client: client, prefix: strings.TrimSuffix(strings.TrimSpace(cfg.Redis.Prefix), ":") + ":http_guard", operationTimeout: operationTimeout}, nil
}

func maxDuration(values ...time.Duration) time.Duration {
	result := 3 * time.Second
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func (r *redisLoginGuard) operationContext() (context.Context, context.CancelFunc) {
	timeout := r.operationTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (r *redisLoginGuard) Close() error { return r.client.Close() }

func (r *redisLoginGuard) CheckLock(kind, key string) (bool, time.Duration, error) {
	ctx, cancel := r.operationContext()
	defer cancel()
	ttl, err := r.client.PTTL(ctx, r.lockKey(kind, key)).Result()
	if err != nil {
		return false, 0, err
	}
	return ttl > 0, maxDurationZero(ttl), nil
}

func (r *redisLoginGuard) RecordFailure(kind, key string, limit int, window, lockDuration time.Duration) error {
	if limit <= 0 || window <= 0 || lockDuration <= 0 {
		return fmt.Errorf("invalid login guard failure policy")
	}
	ctx, cancel := r.operationContext()
	defer cancel()
	return loginFailureScript.Run(ctx, r.client, []string{r.countKey(kind, key), r.lockKey(kind, key)}, limit, window.Milliseconds(), lockDuration.Milliseconds()).Err()
}

func (r *redisLoginGuard) Clear(kind, key string) error {
	ctx, cancel := r.operationContext()
	defer cancel()
	return r.client.Del(ctx, r.countKey(kind, key), r.lockKey(kind, key)).Err()
}

func (r *redisLoginGuard) Allow(kind, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit <= 0 || window <= 0 {
		return false, 0, fmt.Errorf("invalid login guard rate-limit policy")
	}
	ctx, cancel := r.operationContext()
	defer cancel()
	values, err := fixedWindowScript.Run(ctx, r.client, []string{r.countKey(kind, key)}, limit, window.Milliseconds()).Slice()
	if err != nil {
		return false, 0, err
	}
	if len(values) != 2 {
		return false, 0, fmt.Errorf("unexpected Redis rate-limit response")
	}
	count, ok := values[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("invalid Redis rate-limit count")
	}
	ttlMS, ok := values[1].(int64)
	if !ok {
		return false, 0, fmt.Errorf("invalid Redis rate-limit ttl")
	}
	return count <= int64(limit), maxDurationZero(time.Duration(ttlMS) * time.Millisecond), nil
}

func (r *redisLoginGuard) countKey(kind, key string) string { return r.key(kind, "count", key) }
func (r *redisLoginGuard) lockKey(kind, key string) string  { return r.key(kind, "lock", key) }

func (r *redisLoginGuard) key(kind, state, key string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return r.prefix + ":" + kind + ":" + state + ":" + hex.EncodeToString(digest[:])
}

func maxDurationZero(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
