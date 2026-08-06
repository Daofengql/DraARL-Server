package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/config"

	"github.com/gin-gonic/gin"
)

const (
	messageAPIDefaultPageSizeKey = "message_api_default_page_size"
	messageAPIMaxPageSizeKey     = "message_api_max_page_size"
	maxMessageAPIRateLimitKeys   = 100_000
)

type messageAPIWindow struct {
	count     int
	expiresAt time.Time
}

type messageAPIWindowLimiter struct {
	mu      sync.Mutex
	entries map[string]messageAPIWindow
	checks  uint64
}

func newMessageAPIWindowLimiter() *messageAPIWindowLimiter {
	return &messageAPIWindowLimiter{entries: make(map[string]messageAPIWindow)}
}

func (l *messageAPIWindowLimiter) allow(key string, limit int, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks++
	if l.checks%256 == 0 || len(l.entries) >= maxMessageAPIRateLimitKeys {
		for entryKey, entry := range l.entries {
			if !entry.expiresAt.After(now) {
				delete(l.entries, entryKey)
			}
		}
	}
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= maxMessageAPIRateLimitKeys {
		return false, time.Minute
	}
	if entry.expiresAt.IsZero() || !entry.expiresAt.After(now) {
		entry = messageAPIWindow{expiresAt: now.Add(time.Minute)}
	}
	if entry.count >= limit {
		return false, entry.expiresAt.Sub(now)
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}

type MessageAPIGuard struct {
	config    config.MessageAPIConfig
	limiter   *messageAPIWindowLimiter
	semaphore chan struct{}
	now       func() time.Time
}

var (
	messageAPIRateLimitUserRejects atomic.Uint64
	messageAPIRateLimitIPRejects   atomic.Uint64
	messageAPIConcurrencyRejects   atomic.Uint64
	messageAPIActiveRequests       atomic.Int64
	messageAPIMaxActiveRequests    atomic.Int64
)

func NewMessageAPIGuard(cfg config.MessageAPIConfig) *MessageAPIGuard {
	if err := cfg.SetDefaults(); err != nil {
		cfg = config.MessageAPIConfig{}
		_ = cfg.SetDefaults()
	}
	return &MessageAPIGuard{
		config: cfg, limiter: newMessageAPIWindowLimiter(),
		semaphore: make(chan struct{}, cfg.MaxConcurrentQueries), now: time.Now,
	}
}

func (g *MessageAPIGuard) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(messageAPIDefaultPageSizeKey, g.config.DefaultPageSize)
		c.Set(messageAPIMaxPageSizeKey, g.config.MaxPageSize)
		now := g.now()
		userID, ok := messageAPIUserID(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": http.StatusUnauthorized, "error": "authentication_required", "message": "需要登录",
			})
			return
		}
		if allowed, retryAfter := g.limiter.allow(fmt.Sprintf("user:%d", userID), g.config.RequestsPerMinutePerUser, now); !allowed {
			messageAPIRateLimitUserRejects.Add(1)
			writeMessageAPILimit(c, "message_api_user_rate_limited", retryAfter)
			return
		}
		if allowed, retryAfter := g.limiter.allow("ip:"+c.ClientIP(), g.config.RequestsPerMinutePerIP, now); !allowed {
			messageAPIRateLimitIPRejects.Add(1)
			writeMessageAPILimit(c, "message_api_ip_rate_limited", retryAfter)
			return
		}
		select {
		case g.semaphore <- struct{}{}:
			active := messageAPIActiveRequests.Add(1)
			updateMessageAPIMaxActive(active)
			defer func() {
				messageAPIActiveRequests.Add(-1)
				<-g.semaphore
			}()
			c.Next()
		default:
			messageAPIConcurrencyRejects.Add(1)
			c.Header("Retry-After", "1")
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"code": http.StatusServiceUnavailable, "error": "message_api_busy", "message": "消息查询繁忙，请稍后重试",
			})
		}
	}
}

func messageAPIUserID(c *gin.Context) (int, bool) {
	value, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	switch id := value.(type) {
	case int:
		return id, id > 0
	case uint:
		return int(id), id > 0
	default:
		return 0, false
	}
}

func writeMessageAPILimit(c *gin.Context, code string, retryAfter time.Duration) {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"code": http.StatusTooManyRequests, "error": code, "message": "消息查询过于频繁，请稍后重试",
	})
}

func MessageAPIPageLimits(c *gin.Context) (int, int) {
	defaultPageSize, defaultExists := c.Get(messageAPIDefaultPageSizeKey)
	maxPageSize, maxExists := c.Get(messageAPIMaxPageSizeKey)
	defaultValue, defaultOK := defaultPageSize.(int)
	maxValue, maxOK := maxPageSize.(int)
	if !defaultExists || !maxExists || !defaultOK || !maxOK || defaultValue < 1 || maxValue < defaultValue {
		return config.DefaultMessageAPIPageSize, config.DefaultMessageAPIMaxPageSize
	}
	return defaultValue, maxValue
}

func updateMessageAPIMaxActive(candidate int64) {
	for current := messageAPIMaxActiveRequests.Load(); candidate > current; current = messageAPIMaxActiveRequests.Load() {
		if messageAPIMaxActiveRequests.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func GetMessageAPIGuardMetrics() map[string]uint64 {
	active := messageAPIActiveRequests.Load()
	if active < 0 {
		active = 0
	}
	peak := messageAPIMaxActiveRequests.Load()
	if peak < 0 {
		peak = 0
	}
	return map[string]uint64{
		"rate_limit_user_rejects": messageAPIRateLimitUserRejects.Load(),
		"rate_limit_ip_rejects":   messageAPIRateLimitIPRejects.Load(),
		"concurrency_rejects":     messageAPIConcurrencyRejects.Load(),
		"active_requests":         uint64(active),
		"max_active_requests":     uint64(peak),
	}
}
