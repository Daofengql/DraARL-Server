package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	gormdb "draarl/internal/gormdb"
)

// UserCache 用户信息缓存管理器
type UserCache struct {
	cache      *TwoLevelCache
	loadByID   func(context.Context, int) (*gormdb.User, error)
	loadByName func(context.Context, string) (*gormdb.User, error)

	// 【缓存击穿/穿透修复】singleflight 合并并发未命中的重复 DB 查询；
	// 负缓存记录"不存在"的用户，短 TTL 内不再反复穿透数据库。
	inflightMu sync.Mutex
	inflight   map[string]*userInflight
	negMu      sync.Mutex
	neg        map[string]time.Time
}

// userInflight 表示一次进行中的数据库加载。
type userInflight struct {
	done chan struct{}
	user *gormdb.User
	err  error
}

// userNegativeTTL 负缓存有效期：防止攻击者用不存在用户名反复穿透 DB。
const userNegativeTTL = 30 * time.Second

const userLoadTimeout = 5 * time.Second

func newUserCache(cache *TwoLevelCache) *UserCache {
	return &UserCache{
		cache: cache,
		loadByID: func(ctx context.Context, id int) (*gormdb.User, error) {
			return gormdb.NewUserRepository().GetUserByIDContext(ctx, id)
		},
		loadByName: func(ctx context.Context, name string) (*gormdb.User, error) {
			return gormdb.NewUserRepository().GetUserByNameContext(ctx, name)
		},
		inflight: make(map[string]*userInflight),
		neg:      make(map[string]time.Time),
	}
}

// UserCacheConfig 用户缓存配置
type UserCacheConfig struct {
	LocalTTL time.Duration // 默认 2 分钟
	MaxSize  int           // 默认 10000
}

// NewUserCache 创建用户缓存管理器
func NewUserCache(config UserCacheConfig) (*UserCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = 2 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 10000
	}

	cache, err := NewTwoLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
	})
	if err != nil {
		return nil, err
	}

	return newUserCache(cache), nil
}

// 缓存键生成函数

// userKey 用户基本信息缓存键
func userKey(userID int) string {
	return fmt.Sprintf("user:info:v2:%d", userID)
}

// userByNameKey 通过用户名查询的缓存键
func userByNameKey(username string) string {
	return fmt.Sprintf("user:name:v2:%s", username)
}

// userRoleKey 用户角色缓存键
func userRoleKey(userID int) string {
	return fmt.Sprintf("user:role:%d", userID)
}

// isNegative 检查是否存在未过期的负缓存记录。
func (c *UserCache) isNegative(key string) bool {
	c.negMu.Lock()
	defer c.negMu.Unlock()
	exp, ok := c.neg[key]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(c.neg, key)
		return false
	}
	return true
}

// markNegative 记录一次负缓存（短 TTL）。
func (c *UserCache) markNegative(key string) {
	c.negMu.Lock()
	defer c.negMu.Unlock()
	now := time.Now()
	if len(c.neg) >= 16384 {
		for candidate, expiresAt := range c.neg {
			if !now.Before(expiresAt) {
				delete(c.neg, candidate)
			}
		}
	}
	if len(c.neg) >= 16384 {
		// 仍满时仅淘汰一个条目，避免攻击者用一个新 key 清空全部负缓存。
		for candidate := range c.neg {
			delete(c.neg, candidate)
			break
		}
	}
	c.neg[key] = now.Add(userNegativeTTL)
}

// clearNegative 删除负缓存记录。
func (c *UserCache) clearNegative(key string) {
	c.negMu.Lock()
	delete(c.neg, key)
	c.negMu.Unlock()
}

// coalesce 合并同一 key 的并发加载。等待者使用自己的 context，避免一个
// 慢查询把已经取消的 HTTP 请求继续挂在内存中。
func (c *UserCache) coalesce(ctx context.Context, key string, load func(context.Context) (*gormdb.User, error)) (*gormdb.User, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.inflightMu.Lock()
	pending, exists := c.inflight[key]
	if !exists {
		pending = &userInflight{done: make(chan struct{})}
		c.inflight[key] = pending
	}
	c.inflightMu.Unlock()

	if !exists {
		// No request owns the shared query after admission. Each caller may stop
		// waiting independently, while the bounded load can still populate the
		// cache for other callers and subsequent requests.
		go func() {
			loadCtx, cancel := context.WithTimeout(context.Background(), userLoadTimeout)
			defer cancel()
			defer func() {
				if recovered := recover(); recovered != nil {
					pending.err = fmt.Errorf("user cache loader panic: %v", recovered)
				}
				c.inflightMu.Lock()
				delete(c.inflight, key)
				c.inflightMu.Unlock()
				close(pending.done)
			}()
			pending.user, pending.err = load(loadCtx)
		}()
	}

	select {
	case <-pending.done:
		return cloneCachedUser(pending.user), pending.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func cloneCachedUser(user *gormdb.User) *gormdb.User {
	if user == nil {
		return nil
	}
	clone := *user
	if user.ReviewerID != nil {
		value := *user.ReviewerID
		clone.ReviewerID = &value
	}
	if user.ReviewTime != nil {
		value := *user.ReviewTime
		clone.ReviewTime = &value
	}
	if user.LastLoginTime != nil {
		value := *user.LastLoginTime
		clone.LastLoginTime = &value
	}
	return &clone
}

// GetUserByID 通过ID获取用户（带缓存 + singleflight + 负缓存）
func (c *UserCache) GetUserByID(ctx context.Context, id int) (*gormdb.User, error) {
	key := userKey(id)

	var user gormdb.User
	if err := c.cache.Get(ctx, key, &user); err == nil {
		return &user, nil
	}
	if c.isNegative(key) {
		return nil, nil
	}

	dbUser, err := c.coalesce(ctx, key, func(loadCtx context.Context) (*gormdb.User, error) {
		var cached gormdb.User
		if err := c.cache.Get(loadCtx, key, &cached); err == nil {
			return &cached, nil
		}
		if c.isNegative(key) {
			return nil, nil
		}
		loaded, err := c.loadByID(loadCtx, id)
		if err != nil {
			return nil, err
		}
		if loaded == nil {
			c.markNegative(key)
			return nil, nil
		}
		c.clearNegative(key)
		c.clearNegative(userByNameKey(loaded.Name))
		if err := c.cache.Set(loadCtx, key, loaded, 0); err != nil {
			return nil, err
		}
		if loaded.Name != "" {
			if err := c.cache.Set(loadCtx, userByNameKey(loaded.Name), loaded, 0); err != nil {
				return nil, err
			}
		}
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return dbUser, nil
}

// GetUserByName 通过用户名获取用户（带缓存 + singleflight + 负缓存）
func (c *UserCache) GetUserByName(ctx context.Context, name string) (*gormdb.User, error) {
	key := userByNameKey(name)

	var user gormdb.User
	if err := c.cache.Get(ctx, key, &user); err == nil {
		return &user, nil
	}
	if c.isNegative(key) {
		return nil, nil
	}

	dbUser, err := c.coalesce(ctx, key, func(loadCtx context.Context) (*gormdb.User, error) {
		var cached gormdb.User
		if err := c.cache.Get(loadCtx, key, &cached); err == nil {
			return &cached, nil
		}
		if c.isNegative(key) {
			return nil, nil
		}
		loaded, err := c.loadByName(loadCtx, name)
		if err != nil {
			return nil, err
		}
		if loaded == nil {
			c.markNegative(key)
			return nil, nil
		}
		c.clearNegative(key)
		c.clearNegative(userKey(loaded.ID))
		if err := c.cache.Set(loadCtx, key, loaded, 0); err != nil {
			return nil, err
		}
		if err := c.cache.Set(loadCtx, userKey(loaded.ID), loaded, 0); err != nil {
			return nil, err
		}
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return dbUser, nil
}

// InvalidateUser 使用户缓存失效（更新/删除用户时调用）
func (c *UserCache) InvalidateUser(ctx context.Context, userID int, username string) error {
	keys := []string{
		userKey(userID),
		userRoleKey(userID),
	}
	if username != "" {
		keys = append(keys, userByNameKey(username))
	}
	for _, key := range keys {
		c.clearNegative(key)
	}
	return c.cache.Delete(ctx, keys...)
}

// InvalidateUserRole 使用户角色缓存失效（角色变更时调用）
func (c *UserCache) InvalidateUserRole(ctx context.Context, userID int) error {
	return c.cache.Delete(ctx, userRoleKey(userID))
}

// GetCache 获取底层缓存接口（用于特殊操作）
func (c *UserCache) GetCache() *TwoLevelCache {
	return c.cache
}

// Warmup 预热缓存（可选，启动时加载热点用户）
func (c *UserCache) Warmup(ctx context.Context, userIDs []int) error {
	repo := gormdb.NewUserRepository()
	for _, id := range userIDs {
		user, err := repo.GetUserByID(id)
		if err != nil || user == nil {
			continue
		}
		_ = c.cache.Set(ctx, userKey(id), user, 0)
	}
	return nil
}
