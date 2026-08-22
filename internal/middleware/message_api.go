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
	mu            sync.Mutex
	entries       map[string]messageAPIWindow
	order         []string
	orderIndex    map[string]int
	cleanupCursor int
	checks        uint64
}

func newMessageAPIWindowLimiter() *messageAPIWindowLimiter {
	return &messageAPIWindowLimiter{entries: make(map[string]messageAPIWindow), orderIndex: make(map[string]int)}
}

func (l *messageAPIWindowLimiter) allow(key string, limit int, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks++
	// 【限速器 DoS 修复】清理改为有界：每次至多扫描 64 个条目，避免满表时
	// 每个请求都全表 O(n) 扫描（100k 条目）阻塞热路径。
	if l.checks%256 == 0 {
		l.pruneExpired(now, 64)
	}
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= maxMessageAPIRateLimitKeys {
		// 表满且是新 key：尝试清理过期条目腾位，腾不出才拒绝，
		// 避免大量唯一 IP 把合法新用户全部 429。
		if !l.pruneExpired(now, 64) {
			return false, time.Minute
		}
	}
	if entry.expiresAt.IsZero() || !entry.expiresAt.After(now) {
		entry = messageAPIWindow{expiresAt: now.Add(time.Minute)}
	}
	if entry.count >= limit {
		return false, entry.expiresAt.Sub(now)
	}
	entry.count++
	if !exists {
		l.orderIndex[key] = len(l.order)
		l.order = append(l.order, key)
	}
	l.entries[key] = entry
	return true, 0
}

// deleteEntryLocked removes a limiter key from the ordered cleanup index in
// O(1). The caller must hold l.mu.
func (l *messageAPIWindowLimiter) deleteEntryLocked(key string) {
	delete(l.entries, key)
	idx, ok := l.orderIndex[key]
	if !ok {
		return
	}
	last := len(l.order) - 1
	if idx != last {
		lastKey := l.order[last]
		l.order[idx] = lastKey
		l.orderIndex[lastKey] = idx
	}
	l.order = l.order[:last]
	delete(l.orderIndex, key)
	if l.cleanupCursor > idx {
		l.cleanupCursor--
	}
	if l.cleanupCursor >= len(l.order) {
		l.cleanupCursor = 0
	}
}

// pruneExpired 删除至多 max 个已过期条目，返回是否删除到至少一个。
// 持续游标保证有界清理最终覆盖所有 key，不依赖 map 随机迭代顺序。
// 仅在持锁时调用。
func (l *messageAPIWindowLimiter) pruneExpired(now time.Time, max int) bool {
	removed := false
	for scanned := 0; scanned < max && len(l.order) > 0; {
		if l.cleanupCursor >= len(l.order) {
			l.cleanupCursor = 0
		}
		entryKey := l.order[l.cleanupCursor]
		entry, exists := l.entries[entryKey]
		if !exists {
			l.deleteEntryLocked(entryKey)
			continue
		}
		scanned++
		if !entry.expiresAt.After(now) {
			l.deleteEntryLocked(entryKey)
			removed = true
			continue
		}
		l.cleanupCursor++
	}
	return removed
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
