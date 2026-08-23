package auth

import (
	"fmt"
	"log"
	"sync"
	"time"

	"draarl/internal/config"
)

var (
	// ErrRefreshTokenNotActive 表示刷新令牌已失效（不存在/已吊销）。
	ErrRefreshTokenNotActive = fmt.Errorf("refresh token is not active")
)

// RefreshTokenRecord 刷新令牌记录。
type RefreshTokenRecord struct {
	UserID         int
	TokenHash      string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	ReplacedByHash string
	RevokeReason   string
	CreatedIP      string
	UserAgent      string
	LastUsedAt     *time.Time
}

// RefreshTokenStore 刷新令牌存储接口。
type RefreshTokenStore interface {
	Create(token *RefreshTokenRecord) error
	GetByTokenHash(hash string) (*RefreshTokenRecord, error)
	Rotate(oldTokenHash string, newToken *RefreshTokenRecord, revokeReason string, now time.Time) error
	RevokeByTokenHash(hash, reason string, now time.Time) error
	RevokeAllByUser(userID int, reason string, now time.Time) error
}

type storeCloser interface {
	Close() error
}

var (
	storeMu           sync.RWMutex
	refreshTokenStore RefreshTokenStore
	refreshStoreClose storeCloser
	redisStoreFactory = newRedisRefreshTokenStore
)

// InitRefreshTokenStore 初始化刷新令牌存储。
// release 构建要求 Redis 可用；development/test 构建连接失败时降级到内存存储。
func InitRefreshTokenStore(cfg *config.Configuration) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	if refreshStoreClose != nil {
		_ = refreshStoreClose.Close()
		refreshStoreClose = nil
	}

	redisStore, err := redisStoreFactory(cfg)
	if err != nil {
		if config.IsReleaseBuild() {
			refreshTokenStore = nil
			return fmt.Errorf("release build requires Redis refresh-token storage: %w", err)
		}
		log.Printf("[AUTH] Redis 不可用，refresh token 降级到内存存储: %v", err)
		refreshTokenStore = newMemoryRefreshTokenStore()
		return nil
	}

	refreshTokenStore = redisStore
	refreshStoreClose = redisStore
	log.Printf("[AUTH] Refresh token 存储已启用: redis(%s)", cfg.RedisAddr())
	return nil
}

// GetRefreshTokenStore 获取刷新令牌存储实例。
func GetRefreshTokenStore() RefreshTokenStore {
	storeMu.RLock()
	if refreshTokenStore != nil {
		defer storeMu.RUnlock()
		return refreshTokenStore
	}
	storeMu.RUnlock()

	storeMu.Lock()
	defer storeMu.Unlock()
	if refreshTokenStore == nil {
		log.Printf("[AUTH] Refresh token 存储未初始化，回退到内存存储")
		refreshTokenStore = newMemoryRefreshTokenStore()
	}
	return refreshTokenStore
}

// CloseRefreshTokenStore 关闭刷新令牌存储资源。
func CloseRefreshTokenStore() {
	storeMu.Lock()
	defer storeMu.Unlock()

	if refreshStoreClose != nil {
		_ = refreshStoreClose.Close()
		refreshStoreClose = nil
	}
	refreshTokenStore = nil
}
