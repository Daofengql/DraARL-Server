package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"draarl/internal/config"

	"github.com/redis/go-redis/v9"
)

type redisRefreshTokenStore struct {
	client *redis.Client
	prefix string
}

func newRedisRefreshTokenStore(cfg *config.Configuration) (*redisRefreshTokenStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSec) * time.Second,
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSec) * time.Second,
		PoolSize:     cfg.Redis.PoolSize,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Redis.DialTimeoutSec)*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &redisRefreshTokenStore{
		client: client,
		prefix: normalizeStorePrefix(cfg.Redis.Prefix),
	}, nil
}

func (r *redisRefreshTokenStore) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *redisRefreshTokenStore) Create(token *RefreshTokenRecord) error {
	if token == nil || strings.TrimSpace(token.TokenHash) == "" {
		return nil
	}

	ctx := context.Background()
	tokenKey := r.tokenKey(token.TokenHash)
	userSetKey := r.userSetKey(token.UserID)

	pipe := r.client.Pipeline()
	pipe.HSet(ctx, tokenKey, buildRecordFields(token))
	pipe.ExpireAt(ctx, tokenKey, token.ExpiresAt.Add(expiredTokenRetention))
	pipe.SAdd(ctx, userSetKey, token.TokenHash)
	pipe.Expire(ctx, userSetKey, userSetTTL(token.ExpiresAt))
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisRefreshTokenStore) GetByTokenHash(hash string) (*RefreshTokenRecord, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	ctx := context.Background()
	tokenKey := r.tokenKey(hash)
	values, err := r.client.HGetAll(ctx, tokenKey).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}

	record, err := parseRecordFields(hash, values)
	if err != nil {
		return nil, err
	}

	if time.Now().After(record.ExpiresAt.Add(expiredTokenRetention)) {
		pipe := r.client.Pipeline()
		pipe.Del(ctx, tokenKey)
		pipe.SRem(ctx, r.userSetKey(record.UserID), hash)
		_, _ = pipe.Exec(ctx)
		return nil, nil
	}

	return record, nil
}

func (r *redisRefreshTokenStore) Rotate(oldTokenHash string, newToken *RefreshTokenRecord, revokeReason string, now time.Time) error {
	oldTokenHash = strings.TrimSpace(oldTokenHash)
	if oldTokenHash == "" || newToken == nil || strings.TrimSpace(newToken.TokenHash) == "" {
		return ErrRefreshTokenNotActive
	}

	ctx := context.Background()
	oldTokenKey := r.tokenKey(oldTokenHash)
	newTokenKey := r.tokenKey(newToken.TokenHash)
	userSetKey := r.userSetKey(newToken.UserID)

	for i := 0; i < 3; i++ {
		err := r.client.Watch(ctx, func(tx *redis.Tx) error {
			values, err := tx.HGetAll(ctx, oldTokenKey).Result()
			if err != nil {
				return err
			}
			if len(values) == 0 {
				return ErrRefreshTokenNotActive
			}
			if strings.TrimSpace(values["revoked_at"]) != "" {
				return ErrRefreshTokenNotActive
			}

			oldUserID, err := strconv.Atoi(strings.TrimSpace(values["user_id"]))
			if err != nil || oldUserID != newToken.UserID {
				return ErrRefreshTokenNotActive
			}

			oldExpiresUnix, err := strconv.ParseInt(strings.TrimSpace(values["expires_at"]), 10, 64)
			if err != nil {
				return err
			}
			oldExpiry := time.Unix(oldExpiresUnix, 0)
			nowUnix := strconv.FormatInt(now.Unix(), 10)

			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.HSet(ctx, oldTokenKey, map[string]any{
					"revoked_at":       nowUnix,
					"replaced_by_hash": newToken.TokenHash,
					"revoke_reason":    strings.TrimSpace(revokeReason),
					"last_used_at":     nowUnix,
				})
				pipe.ExpireAt(ctx, oldTokenKey, oldExpiry.Add(expiredTokenRetention))
				pipe.HSet(ctx, newTokenKey, buildRecordFields(newToken))
				pipe.ExpireAt(ctx, newTokenKey, newToken.ExpiresAt.Add(expiredTokenRetention))
				pipe.SAdd(ctx, userSetKey, oldTokenHash, newToken.TokenHash)
				pipe.Expire(ctx, userSetKey, userSetTTL(newToken.ExpiresAt))
				return nil
			})
			return err
		}, oldTokenKey)

		if err == redis.TxFailedErr {
			continue
		}
		return err
	}

	return fmt.Errorf("rotate refresh token failed after retries")
}

func (r *redisRefreshTokenStore) RevokeByTokenHash(hash, reason string, now time.Time) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil
	}

	ctx := context.Background()
	tokenKey := r.tokenKey(hash)
	values, err := r.client.HGetAll(ctx, tokenKey).Result()
	if err != nil {
		return err
	}
	if len(values) == 0 || strings.TrimSpace(values["revoked_at"]) != "" {
		return nil
	}

	record, err := parseRecordFields(hash, values)
	if err != nil {
		return err
	}

	nowUnix := strconv.FormatInt(now.Unix(), 10)
	pipe := r.client.Pipeline()
	pipe.HSet(ctx, tokenKey, map[string]any{
		"revoked_at":    nowUnix,
		"revoke_reason": strings.TrimSpace(reason),
		"last_used_at":  nowUnix,
	})
	pipe.ExpireAt(ctx, tokenKey, record.ExpiresAt.Add(expiredTokenRetention))
	pipe.SAdd(ctx, r.userSetKey(record.UserID), hash)
	pipe.Expire(ctx, r.userSetKey(record.UserID), userSetTTL(record.ExpiresAt))
	_, err = pipe.Exec(ctx)
	return err
}

func (r *redisRefreshTokenStore) RevokeAllByUser(userID int, reason string, now time.Time) error {
	if userID <= 0 {
		return nil
	}

	ctx := context.Background()
	userSetKey := r.userSetKey(userID)
	members, err := r.client.SMembers(ctx, userSetKey).Result()
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}

	nowUnix := strconv.FormatInt(now.Unix(), 10)
	stale := make([]any, 0)
	for _, hash := range members {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			continue
		}

		tokenKey := r.tokenKey(hash)
		values, getErr := r.client.HGetAll(ctx, tokenKey).Result()
		if getErr != nil {
			return getErr
		}
		if len(values) == 0 {
			stale = append(stale, hash)
			continue
		}

		record, parseErr := parseRecordFields(hash, values)
		if parseErr != nil {
			stale = append(stale, hash)
			continue
		}

		if time.Now().After(record.ExpiresAt.Add(expiredTokenRetention)) {
			stale = append(stale, hash)
			_ = r.client.Del(ctx, tokenKey).Err()
			continue
		}

		if strings.TrimSpace(values["revoked_at"]) != "" {
			continue
		}

		pipe := r.client.Pipeline()
		pipe.HSet(ctx, tokenKey, map[string]any{
			"revoked_at":    nowUnix,
			"revoke_reason": strings.TrimSpace(reason),
			"last_used_at":  nowUnix,
		})
		pipe.ExpireAt(ctx, tokenKey, record.ExpiresAt.Add(expiredTokenRetention))
		if _, execErr := pipe.Exec(ctx); execErr != nil {
			return execErr
		}
	}

	if len(stale) > 0 {
		if err := r.client.SRem(ctx, userSetKey, stale...).Err(); err != nil {
			return err
		}
	}

	card, err := r.client.SCard(ctx, userSetKey).Result()
	if err != nil {
		return err
	}
	if card == 0 {
		return r.client.Del(ctx, userSetKey).Err()
	}

	return r.client.Expire(ctx, userSetKey, userSetTTL(now.Add(expiredTokenRetention))).Err()
}

func (r *redisRefreshTokenStore) tokenKey(hash string) string {
	return fmt.Sprintf("%s:auth:rt:%s", r.prefix, strings.TrimSpace(hash))
}

func (r *redisRefreshTokenStore) userSetKey(userID int) string {
	return fmt.Sprintf("%s:auth:rt:user:%d", r.prefix, userID)
}

func normalizeStorePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "draarl"
	}
	return prefix
}

func buildRecordFields(record *RefreshTokenRecord) map[string]any {
	fields := map[string]any{
		"user_id":          strconv.Itoa(record.UserID),
		"token_hash":       record.TokenHash,
		"expires_at":       strconv.FormatInt(record.ExpiresAt.Unix(), 10),
		"replaced_by_hash": strings.TrimSpace(record.ReplacedByHash),
		"revoke_reason":    strings.TrimSpace(record.RevokeReason),
		"created_ip":       strings.TrimSpace(record.CreatedIP),
		"user_agent":       strings.TrimSpace(record.UserAgent),
	}

	if record.RevokedAt != nil {
		fields["revoked_at"] = strconv.FormatInt(record.RevokedAt.Unix(), 10)
	} else {
		fields["revoked_at"] = ""
	}

	if record.LastUsedAt != nil {
		fields["last_used_at"] = strconv.FormatInt(record.LastUsedAt.Unix(), 10)
	} else {
		fields["last_used_at"] = ""
	}

	return fields
}

func parseRecordFields(hash string, values map[string]string) (*RefreshTokenRecord, error) {
	userID, err := strconv.Atoi(strings.TrimSpace(values["user_id"]))
	if err != nil {
		return nil, err
	}

	expiresUnix, err := strconv.ParseInt(strings.TrimSpace(values["expires_at"]), 10, 64)
	if err != nil {
		return nil, err
	}

	record := &RefreshTokenRecord{
		UserID:         userID,
		TokenHash:      hash,
		ExpiresAt:      time.Unix(expiresUnix, 0),
		ReplacedByHash: strings.TrimSpace(values["replaced_by_hash"]),
		RevokeReason:   strings.TrimSpace(values["revoke_reason"]),
		CreatedIP:      strings.TrimSpace(values["created_ip"]),
		UserAgent:      strings.TrimSpace(values["user_agent"]),
	}

	if revokedAt := strings.TrimSpace(values["revoked_at"]); revokedAt != "" {
		revokedUnix, parseErr := strconv.ParseInt(revokedAt, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		revokedAtTime := time.Unix(revokedUnix, 0)
		record.RevokedAt = &revokedAtTime
	}

	if lastUsedAt := strings.TrimSpace(values["last_used_at"]); lastUsedAt != "" {
		lastUsedUnix, parseErr := strconv.ParseInt(lastUsedAt, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		lastUsedAtTime := time.Unix(lastUsedUnix, 0)
		record.LastUsedAt = &lastUsedAtTime
	}

	return record, nil
}

func userSetTTL(expiresAt time.Time) time.Duration {
	ttl := time.Until(expiresAt.Add(expiredTokenRetention))
	if ttl < time.Hour {
		return time.Hour
	}
	return ttl
}
