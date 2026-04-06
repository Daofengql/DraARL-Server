package cache

import (
	"context"
	"fmt"
	"time"

	gormdb "draarl/internal/gormdb"
)

// CertCache 操作证缓存管理器
type CertCache struct {
	cache *TwoLevelCache
}

// CertCacheConfig 操作证缓存配置
type CertCacheConfig struct {
	LocalTTL time.Duration // 默认 5 分钟
	MaxSize  int           // 默认 1000
}

// NewCertCache 创建操作证缓存管理器
func NewCertCache(config CertCacheConfig) (*CertCache, error) {
	// 设置默认值
	if config.LocalTTL == 0 {
		config.LocalTTL = 5 * time.Minute
	}
	if config.MaxSize == 0 {
		config.MaxSize = 1000
	}

	cache, err := NewTwoLevelCache(CacheConfig{
		LocalTTL: config.LocalTTL,
		MaxSize:  config.MaxSize,
	})
	if err != nil {
		return nil, err
	}

	return &CertCache{cache: cache}, nil
}

// 缓存键生成函数

// certByUserIDKey 通过用户ID的操作证缓存键
func certByUserIDKey(userID int) string {
	return fmt.Sprintf("cert:info:%d", userID)
}

// certByIDKey 通过操作证ID的缓存键
func certByIDKey(certID int) string {
	return fmt.Sprintf("cert:id:%d", certID)
}

// certPendingListKey 待审核操作证列表缓存键（分页）
func certPendingListKey(page, pageSize int) string {
	return fmt.Sprintf("cert:pending:page:%d:size:%d", page, pageSize)
}

// certPendingTotalKey 待审核操作证总数缓存键
func certPendingTotalKey() string {
	return "cert:pending:total"
}

// GetActiveCertByUserID 获取用户有效的操作证（带缓存）
func (c *CertCache) GetActiveCertByUserID(ctx context.Context, userID int) (*gormdb.OperatorCert, error) {
	key := certByUserIDKey(userID)

	var cert gormdb.OperatorCert
	if err := c.cache.Get(ctx, key, &cert); err == nil {
		if cert.ID == 0 {
			return nil, nil
		}
		return &cert, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewOperatorCertRepository()
	dbCert, err := repo.GetActiveByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 写入缓存（即使为空也缓存，防止缓存穿透）
	if dbCert == nil {
		// 缓存空对象，使用较短的 TTL
		_ = c.cache.Set(ctx, key, &gormdb.OperatorCert{}, 30*time.Second)
		return nil, nil
	}

	_ = c.cache.Set(ctx, key, dbCert, 0)
	return dbCert, nil
}

// GetLatestCertByUserID 获取用户最新的操作证（带缓存）
func (c *CertCache) GetLatestCertByUserID(ctx context.Context, userID int) (*gormdb.OperatorCert, error) {
	key := fmt.Sprintf("cert:latest:%d", userID)

	var cert gormdb.OperatorCert
	if err := c.cache.Get(ctx, key, &cert); err == nil {
		if cert.ID == 0 {
			return nil, nil
		}
		return &cert, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewOperatorCertRepository()
	dbCert, err := repo.GetLatestByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 写入缓存
	if dbCert == nil {
		_ = c.cache.Set(ctx, key, &gormdb.OperatorCert{}, 30*time.Second)
		return nil, nil
	}

	_ = c.cache.Set(ctx, key, dbCert, 0)
	return dbCert, nil
}

// GetPendingCertByUserID 获取用户待审核的操作证（带缓存）
func (c *CertCache) GetPendingCertByUserID(ctx context.Context, userID int) (*gormdb.OperatorCert, error) {
	key := fmt.Sprintf("cert:pending:user:%d", userID)

	var cert gormdb.OperatorCert
	if err := c.cache.Get(ctx, key, &cert); err == nil {
		if cert.ID == 0 {
			return nil, nil
		}
		return &cert, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewOperatorCertRepository()
	dbCert, err := repo.GetPendingByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 写入缓存（待审核状态变化较快，使用较短 TTL）
	if dbCert == nil {
		_ = c.cache.Set(ctx, key, &gormdb.OperatorCert{}, 30*time.Second)
		return nil, nil
	}

	_ = c.cache.Set(ctx, key, dbCert, time.Minute)
	return dbCert, nil
}

// GetLatestPendingOrRejectedCertByUserID 获取用户最新的待审核/已拒绝操作证（带缓存）
func (c *CertCache) GetLatestPendingOrRejectedCertByUserID(ctx context.Context, userID int) (*gormdb.OperatorCert, error) {
	key := fmt.Sprintf("cert:review:user:%d", userID)

	var cert gormdb.OperatorCert
	if err := c.cache.Get(ctx, key, &cert); err == nil {
		if cert.ID == 0 {
			return nil, nil
		}
		return &cert, nil
	}

	repo := gormdb.NewOperatorCertRepository()
	dbCert, err := repo.GetLatestPendingOrRejectedByUserID(userID)
	if err != nil {
		return nil, err
	}

	if dbCert == nil {
		_ = c.cache.Set(ctx, key, &gormdb.OperatorCert{}, 30*time.Second)
		return nil, nil
	}

	_ = c.cache.Set(ctx, key, dbCert, time.Minute)
	return dbCert, nil
}

// GetCertByID 通过ID获取操作证（带缓存）
func (c *CertCache) GetCertByID(ctx context.Context, certID int) (*gormdb.OperatorCert, error) {
	key := certByIDKey(certID)

	var cert gormdb.OperatorCert
	if err := c.cache.Get(ctx, key, &cert); err == nil {
		if cert.ID == 0 {
			return nil, nil
		}
		return &cert, nil
	}

	// 缓存未命中，从数据库查询
	repo := gormdb.NewOperatorCertRepository()
	dbCert, err := repo.GetByID(certID)
	if err != nil {
		return nil, err
	}

	// 写入缓存
	if dbCert == nil {
		_ = c.cache.Set(ctx, key, &gormdb.OperatorCert{}, 30*time.Second)
		return nil, nil
	}

	_ = c.cache.Set(ctx, key, dbCert, 0)
	return dbCert, nil
}

// InvalidateUserCert 使用户操作证缓存失效（上传/审批时调用）
func (c *CertCache) InvalidateUserCert(ctx context.Context, userID int) error {
	keys := []string{
		certByUserIDKey(userID),
		fmt.Sprintf("cert:latest:%d", userID),
		fmt.Sprintf("cert:pending:user:%d", userID),
		fmt.Sprintf("cert:review:user:%d", userID),
	}
	return c.cache.Delete(ctx, keys...)
}

// InvalidateCert 使操作证详情缓存失效
func (c *CertCache) InvalidateCert(ctx context.Context, certID int, userID int) error {
	keys := []string{
		certByIDKey(certID),
	}
	if userID > 0 {
		keys = append(keys,
			certByUserIDKey(userID),
			fmt.Sprintf("cert:latest:%d", userID),
			fmt.Sprintf("cert:pending:user:%d", userID),
			fmt.Sprintf("cert:review:user:%d", userID),
		)
	}
	return c.cache.Delete(ctx, keys...)
}

// InvalidatePendingList 使待审核列表缓存失效
// 待审核列表同样存在分页，必须使用前缀删除
func (c *CertCache) InvalidatePendingList(ctx context.Context) error {
	// 1. 删除总数
	if err := c.cache.Delete(ctx, certPendingTotalKey()); err != nil {
		return err
	}
	// 2. 主动删除待审核的分页列表 (形如 cert:pending:page:*)
	return c.cache.DeletePrefix(ctx, "cert:pending:page:")
}

// GetCache 获取底层缓存接口（用于特殊操作）
func (c *CertCache) GetCache() *TwoLevelCache {
	return c.cache
}
