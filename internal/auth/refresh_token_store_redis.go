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
	client           *redis.Client
	prefix           string
	operationTimeout time.Duration
}

const defaultRefreshStoreOperationTimeout = 10 * time.Second

// An older token must never shorten the index lifetime of newer sessions.
// EVAL is also queued inside MULTI so the comparison and update are atomic.
const extendUserSetTTLScript = `
local ttl = redis.call('PTTL', KEYS[1])
if ttl < tonumber(ARGV[1]) then
  return redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return 0
`

func extendUserSetTTL(ctx context.Context, pipe redis.Pipeliner, key string, expiresAt time.Time) {
	pipe.Eval(ctx, extendUserSetTTLScript, []string{key}, userSetTTL(expiresAt).Milliseconds())
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
		// Store methods use a total operation deadline in addition to per-command
		// read/write timeouts. Respecting the context prevents multi-command flows
		// such as RevokeAllByUser from accumulating one socket timeout per token.
		ContextTimeoutEnabled: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Redis.DialTimeoutSec)*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &redisRefreshTokenStore{
		client:           client,
		prefix:           normalizeStorePrefix(cfg.Redis.Prefix),
		operationTimeout: refreshStoreOperationTimeout(cfg),
	}, nil
}

func refreshStoreOperationTimeout(cfg *config.Configuration) time.Duration {
	if cfg == nil {
		return defaultRefreshStoreOperationTimeout
	}
	dialTimeout := time.Duration(cfg.Redis.DialTimeoutSec) * time.Second
	readTimeout := time.Duration(cfg.Redis.ReadTimeoutSec) * time.Second
	writeTimeout := time.Duration(cfg.Redis.WriteTimeoutSec) * time.Second
	commandTimeout := max(readTimeout, writeTimeout)
	total := dialTimeout + 4*commandTimeout
	if total < defaultRefreshStoreOperationTimeout {
		return defaultRefreshStoreOperationTimeout
	}
	return total
}

func (r *redisRefreshTokenStore) operationContext() (context.Context, context.CancelFunc) {
	timeout := r.operationTimeout
	if timeout <= 0 {
		timeout = defaultRefreshStoreOperationTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
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

	ctx, cancel := r.operationContext()
	defer cancel()
	tokenKey := r.tokenKey(token.TokenHash)
	userSetKey := r.userSetKey(token.UserID)

	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, tokenKey, buildRecordFields(token))
	pipe.ExpireAt(ctx, tokenKey, token.ExpiresAt.Add(expiredTokenRetention))
	pipe.SAdd(ctx, userSetKey, token.TokenHash)
	extendUserSetTTL(ctx, pipe, userSetKey, token.ExpiresAt)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisRefreshTokenStore) GetByTokenHash(hash string) (*RefreshTokenRecord, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	ctx, cancel := r.operationContext()
	defer cancel()
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

	ctx, cancel := r.operationContext()
	defer cancel()
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
			oldVersion, err := strconv.ParseUint(values["session_version"], 10, 64)
			if err != nil || oldVersion != newToken.SessionVersion || !now.Before(oldExpiry) {
				return ErrRefreshTokenNotActive
			}
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
				extendUserSetTTL(ctx, pipe, userSetKey, newToken.ExpiresAt)
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

	ctx, cancel := r.operationContext()
	defer cancel()
	tokenKey := r.tokenKey(hash)
	return r.watchWithRetry(ctx, []string{tokenKey}, func(tx *redis.Tx) error {
		values, err := tx.HGetAll(ctx, tokenKey).Result()
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
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, tokenKey, map[string]any{
				"revoked_at": now.Unix(), "revoke_reason": strings.TrimSpace(reason), "last_used_at": now.Unix(),
			})
			pipe.ExpireAt(ctx, tokenKey, record.ExpiresAt.Add(expiredTokenRetention))
			pipe.SAdd(ctx, r.userSetKey(record.UserID), hash)
			extendUserSetTTL(ctx, pipe, r.userSetKey(record.UserID), record.ExpiresAt)
			return nil
		})
		return err
	})
}

func (r *redisRefreshTokenStore) RevokeAllByUser(userID int, reason string, now time.Time) error {
	if userID <= 0 {
		return nil
	}

	ctx, cancel := r.operationContext()
	defer cancel()
	userSetKey := r.userSetKey(userID)
	// Watch the index before taking the snapshot. A concurrent Create/Rotate
	// adds a member atomically and forces a retry, including its new token.
	// Also watch records so expiry cannot resurrect a partial hash on commit.
	return r.watchWithRetry(ctx, []string{userSetKey}, func(tx *redis.Tx) error {
		members, err := tx.SMembers(ctx, userSetKey).Result()
		if err != nil || len(members) == 0 {
			return err
		}
		keys := make([]string, 0, len(members))
		for _, hash := range members {
			keys = append(keys, r.tokenKey(hash))
		}
		if err := tx.Watch(ctx, keys...).Err(); err != nil {
			return err
		}
		fetchPipe := tx.Pipeline()
		recordCommands := make(map[string]*redis.MapStringStringCmd, len(members))
		for _, hash := range members {
			recordCommands[hash] = fetchPipe.HGetAll(ctx, r.tokenKey(hash))
		}
		if _, err := fetchPipe.Exec(ctx); err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			remainingMembers := 0
			for hash, command := range recordCommands {
				tokenKey := r.tokenKey(hash)
				values := command.Val()
				if len(values) == 0 {
					pipe.SRem(ctx, userSetKey, hash)
					continue
				}
				record, parseErr := parseRecordFields(hash, values)
				if parseErr != nil || !now.Before(record.ExpiresAt.Add(expiredTokenRetention)) {
					pipe.Del(ctx, tokenKey)
					pipe.SRem(ctx, userSetKey, hash)
					continue
				}
				remainingMembers++
				if record.RevokedAt == nil {
					pipe.HSet(ctx, tokenKey, map[string]any{
						"revoked_at": now.Unix(), "revoke_reason": strings.TrimSpace(reason), "last_used_at": now.Unix(),
					})
					pipe.ExpireAt(ctx, tokenKey, record.ExpiresAt.Add(expiredTokenRetention))
				}
			}
			if remainingMembers == 0 {
				pipe.Del(ctx, userSetKey)
			}
			// Preserve the existing index TTL: it already covers the longest token.
			return nil
		})
		return err
	})
}

func (r *redisRefreshTokenStore) watchWithRetry(ctx context.Context, keys []string, fn func(*redis.Tx) error) error {
	for attempt := 0; attempt < 5; attempt++ {
		err := r.client.Watch(ctx, fn, keys...)
		if err != redis.TxFailedErr {
			return err
		}
	}
	return fmt.Errorf("refresh token transaction conflict after retries: %w", redis.TxFailedErr)
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
		"session_version":  strconv.FormatUint(record.SessionVersion, 10),
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
	if value := values["session_version"]; value != "" {
		version, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		record.SessionVersion = version
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
