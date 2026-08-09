package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "draarl/internal/gormdb"
)

// UserCache 用户信息缓存管理器
type UserCache struct {
	cache *TwoLevelCache
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

	return &UserCache{cache: cache}, nil
}

// 缓存键生成函数

// userKey 用户基本信息缓存键
func userKey(userID int) string {
	return fmt.Sprintf("user:info:%d", userID)
}

// userByNameKey 通过用户名查询的缓存键
func userByNameKey(username string) string {
	return fmt.Sprintf("user:name:%s", username)
}

// userRoleKey 用户角色缓存键
func userRoleKey(userID int) string {
	return fmt.Sprintf("user:role:%d", userID)
}

// GetUserByID 通过ID获取用户（带缓存）
func (c *UserCache) GetUserByID(ctx context.Context, id int) (*gormdb.User, error) {
	key := userKey(id)

	var user gormdb.User
	if err := c.cache.Get(ctx, key, &user); err == nil {
		return &user, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewUserRepository()
	dbUser, err := repo.GetUserByID(id)
	if err != nil {
		return nil, err
	}
	if dbUser == nil {
		return nil, nil
	}

	// 写入缓存
	_ = c.cache.Set(ctx, key, dbUser, 0)

	return dbUser, nil
}

// GetUserByName 通过用户名获取用户（带缓存）
func (c *UserCache) GetUserByName(ctx context.Context, name string) (*gormdb.User, error) {
	key := userByNameKey(name)

	var user gormdb.User
	if err := c.cache.Get(ctx, key, &user); err == nil {
		return &user, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewUserRepository()
	dbUser, err := repo.GetUserByName(name)
	if err != nil {
		return nil, err
	}
	if dbUser == nil {
		return nil, nil
	}

	// 写入缓存（包括按名称和按ID两个键）
	_ = c.cache.Set(ctx, key, dbUser, 0)
	_ = c.cache.Set(ctx, userKey(dbUser.ID), dbUser, 0)

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
