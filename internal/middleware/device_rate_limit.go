package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// RateLimitRule 限速规则
type RateLimitRule struct {
	Key         string        // 限速键（ip 或 mac）
	Limit       int           // 限制次数
	Window      time.Duration // 时间窗口
	Description string        // 描述
}

// RateLimitEntry 限速条目
type RateLimitEntry struct {
	Count     int
	ExpiresAt time.Time
}

// DeviceRateLimiter 设备接口限速器
type DeviceRateLimiter struct {
	mu            sync.RWMutex
	limits        map[string]*RateLimitEntry // key: limitType:value -> entry
	order         []string
	orderIndex    map[string]int
	cleanupCursor int
	stopCh        chan struct{}
	stopOnce      sync.Once

	// 预定义的限速规则
	rules map[string]RateLimitRule
}

// 全局限速器
var deviceRateLimiter *DeviceRateLimiter

func newDeviceRateLimiter() *DeviceRateLimiter {
	return &DeviceRateLimiter{
		limits:     make(map[string]*RateLimitEntry),
		orderIndex: make(map[string]int),
		stopCh:     make(chan struct{}),
		rules: map[string]RateLimitRule{
			"pre-check-ip": {
				Key:         "ip",
				Limit:       1,
				Window:      time.Second,
				Description: "同一 IP 每秒 1 次",
			},
			"pre-check-mac": {
				Key:         "mac",
				Limit:       5,
				Window:      time.Minute,
				Description: "同一 MAC 每分钟 5 次",
			},
			"request-code-ip": {
				Key:         "ip",
				Limit:       30,
				Window:      time.Minute,
				Description: "同一 IP 每分钟 30 次",
			},
			"request-code-mac": {
				Key:         "mac",
				Limit:       10,
				Window:      time.Minute,
				Description: "同一 MAC 每分钟 10 次",
			},
			"confirm-bind-mac": {
				Key:         "mac",
				Limit:       1,
				Window:      5 * time.Second,
				Description: "同一 MAC 每 5 秒 1 次",
			},
			"bind-user": {
				Key:         "user",
				Limit:       20,
				Window:      time.Minute,
				Description: "同一用户每分钟 20 次",
			},
			"submit-config-user": {
				Key:         "user",
				Limit:       10,
				Window:      time.Minute,
				Description: "同一用户每分钟 10 次",
			},
			"public-relay-search-ip": {
				Key:         "ip",
				Limit:       10,
				Window:      time.Minute,
				Description: "同一 IP 每分钟 10 次",
			},
			"public-client-resource-ip": {
				Key:         "ip",
				Limit:       30,
				Window:      time.Minute,
				Description: "同一 IP 每分钟 30 次",
			},
			"captcha-ip-burst": {
				Key:         "ip",
				Limit:       5,
				Window:      10 * time.Second,
				Description: "同一 IP 每 10 秒 5 次",
			},
			"captcha-ip-minute": {
				Key:         "ip",
				Limit:       30,
				Window:      time.Minute,
				Description: "同一 IP 每分钟 30 次",
			},
			"access-discovery-token-ip-burst": {
				Key: "ip", Limit: 60, Window: 10 * time.Second, Description: "同一 IP 每 10 秒 60 次",
			},
			"access-discovery-token-ip-minute": {
				Key: "ip", Limit: 300, Window: time.Minute, Description: "同一 IP 每分钟 300 次",
			},
			"access-discovery-token-user": {
				Key: "user", Limit: 10, Window: time.Minute, Description: "同一 IP 和用户名每分钟 10 次",
			},
			"access-discovery-list-ip": {
				Key: "ip", Limit: 600, Window: time.Minute, Description: "同一 IP 每分钟 600 次",
			},
			"access-discovery-list-user": {
				Key: "user", Limit: 30, Window: time.Minute, Description: "同一用户每分钟 30 次",
			},
			"group-join-password-ip": {
				Key: "ip", Limit: 30, Window: time.Minute, Description: "同一 IP 每分钟 30 次私有群组密码验证",
			},
			"group-join-password-user": {
				Key: "user", Limit: 10, Window: time.Minute, Description: "同一用户每分钟 10 次私有群组密码验证",
			},
		},
	}
}

// InitDeviceRateLimiter 初始化设备接口限速器
func InitDeviceRateLimiter() {
	previous := deviceRateLimiter
	current := newDeviceRateLimiter()
	deviceRateLimiter = current
	if previous != nil {
		previous.stop()
	}
	// 启动清理协程
	go current.cleanup()
}

// GetDeviceRateLimiter 获取全局限速器
func GetDeviceRateLimiter() *DeviceRateLimiter {
	return deviceRateLimiter
}

// maxDeviceRateLimitKeys 限速条目上限，防止唯一键无限增长造成内存泄漏。
const maxDeviceRateLimitKeys = 100_000

// checkLimit 检查是否超过限速。
// 【MAC 伪造修复】rule.Key=="mac" 时把来源 IP 并入限速键，客户端伪造任意
// MAC 也无法绕过"同一 IP"的约束。map 有上限：满表时先清理过期条目腾位，
// 腾不出才拒绝，避免无界增长。
func (r *DeviceRateLimiter) checkLimit(ruleName, value, sourceIP string) (allowed bool, retryAfter time.Duration) {
	rule, exists := r.rules[ruleName]
	if !exists {
		return true, 0
	}
	if rule.Key == "mac" && sourceIP != "" {
		value = value + "@" + sourceIP
	}
	if value == "" {
		return true, 0
	}

	key := ruleName + ":" + value

	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureOrderIndexLocked()

	now := time.Now()
	entry, exists := r.limits[key]

	if !exists {
		if len(r.limits) >= maxDeviceRateLimitKeys {
			r.pruneExpiredLocked(now, 64)
		}
		if len(r.limits) >= maxDeviceRateLimitKeys {
			return false, rule.Window
		}
		r.limits[key] = &RateLimitEntry{
			Count:     1,
			ExpiresAt: now.Add(rule.Window),
		}
		r.orderIndex[key] = len(r.order)
		r.order = append(r.order, key)
		return true, 0
	}

	if now.After(entry.ExpiresAt) {
		entry.Count = 1
		entry.ExpiresAt = now.Add(rule.Window)
		return true, 0
	}

	// 检查是否超限
	if entry.Count >= rule.Limit {
		retryAfter = entry.ExpiresAt.Sub(now)
		return false, retryAfter
	}

	// 增加计数
	entry.Count++
	return true, 0
}

// ensureOrderIndexLocked lazily builds the cleanup index for callers/tests
// that construct a DeviceRateLimiter with only the legacy fields.
func (r *DeviceRateLimiter) ensureOrderIndexLocked() {
	if r.orderIndex != nil {
		return
	}
	r.orderIndex = make(map[string]int, len(r.limits))
	r.order = make([]string, 0, len(r.limits))
	for key := range r.limits {
		r.orderIndex[key] = len(r.order)
		r.order = append(r.order, key)
	}
}

// deleteLimitEntryLocked removes a key from the ordered cleanup index in
// O(1). The caller must hold r.mu.
func (r *DeviceRateLimiter) deleteLimitEntryLocked(key string) {
	delete(r.limits, key)
	idx, ok := r.orderIndex[key]
	if !ok {
		return
	}
	last := len(r.order) - 1
	if idx != last {
		lastKey := r.order[last]
		r.order[idx] = lastKey
		r.orderIndex[lastKey] = idx
	}
	r.order = r.order[:last]
	delete(r.orderIndex, key)
	if r.cleanupCursor > idx {
		r.cleanupCursor--
	}
	if r.cleanupCursor >= len(r.order) {
		r.cleanupCursor = 0
	}
}

// pruneExpiredLocked 删除至多 max 个已过期条目（持锁时调用）。持续游标
// 确保有界清理最终覆盖所有 key，不依赖 map 随机迭代顺序。
func (r *DeviceRateLimiter) pruneExpiredLocked(now time.Time, max int) {
	r.ensureOrderIndexLocked()
	for scanned := 0; scanned < max && len(r.order) > 0; {
		if r.cleanupCursor >= len(r.order) {
			r.cleanupCursor = 0
		}
		key := r.order[r.cleanupCursor]
		entry, exists := r.limits[key]
		if !exists {
			r.deleteLimitEntryLocked(key)
			continue
		}
		scanned++
		if now.After(entry.ExpiresAt) {
			r.deleteLimitEntryLocked(key)
			continue
		}
		r.cleanupCursor++
	}
}

// cleanup 定期清理过期的限速条目。
// 【修复】每次 tick 有界清理（至多 4096 条），避免大表时整表扫描长时间持锁
// 阻塞热路径；多次 tick 自然收敛。
func (r *DeviceRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			r.pruneExpiredLocked(time.Now(), 4096)
			r.mu.Unlock()
		case <-r.stopCh:
			return
		}
	}
}

func (r *DeviceRateLimiter) stop() {
	if r == nil || r.stopCh == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// DeviceRateLimit 设备接口限速中间件
// ruleNames: 限速规则名称列表（同时检查多个规则）
// keyExtractor: 从请求中提取限速键值的函数
func DeviceRateLimit(ruleNames []string, keyExtractor func(*gin.Context) map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		// 提取键值
		keys := keyExtractor(c)

		// 检查所有规则
		for _, ruleName := range ruleNames {
			rule, exists := deviceRateLimiter.rules[ruleName]
			if !exists {
				continue
			}

			value, hasKey := keys[rule.Key]
			if !hasKey || value == "" {
				continue
			}

			allowed, retryAfter := deviceRateLimiter.checkLimit(ruleName, value, c.ClientIP())
			if !allowed {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"code":    429,
					"message": "请求过于频繁，请稍后重试",
					"data": gin.H{
						"retry_after": int(retryAfter.Seconds()),
					},
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// 请求结构体定义（用于限速中间件）
type preCheckRequest struct {
	MAC string `json:"mac"`
}

type requestCodeRequest struct {
	MAC string `json:"mac"`
}

type confirmBindRequest struct {
	MAC string `json:"mac"`
}

// PreCheckRateLimit pre-check 接口限速中间件
func PreCheckRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		// 使用 ShouldBindBodyWith 缓存请求体，后续 handler 可以再次绑定
		var req preCheckRequest
		if err := c.ShouldBindBodyWith(&req, binding.JSON); err == nil {
			// 检查 MAC 限速
			if req.MAC != "" {
				allowed, retryAfter := deviceRateLimiter.checkLimit("pre-check-mac", req.MAC, c.ClientIP())
				if !allowed {
					c.JSON(http.StatusTooManyRequests, gin.H{
						"code":    429,
						"message": "请求过于频繁，请稍后重试",
						"data": gin.H{
							"retry_after": int(retryAfter.Seconds()),
						},
					})
					c.Abort()
					return
				}
			}
		}

		// 检查 IP 限速
		allowed, retryAfter := deviceRateLimiter.checkLimit("pre-check-ip", c.ClientIP(), c.ClientIP())
		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "请求过于频繁，请稍后重试",
				"data": gin.H{
					"retry_after": int(retryAfter.Seconds()),
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequestCodeRateLimit request-code 接口限速中间件
func RequestCodeRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		// 使用 ShouldBindBodyWith 缓存请求体
		var req requestCodeRequest
		if err := c.ShouldBindBodyWith(&req, binding.JSON); err == nil {
			// 检查 MAC 限速
			if req.MAC != "" {
				allowed, retryAfter := deviceRateLimiter.checkLimit("request-code-mac", req.MAC, c.ClientIP())
				if !allowed {
					c.JSON(http.StatusTooManyRequests, gin.H{
						"code":    429,
						"message": "请求过于频繁，请稍后重试",
						"data": gin.H{
							"retry_after": int(retryAfter.Seconds()),
						},
					})
					c.Abort()
					return
				}
			}
		}

		// 检查 IP 限速
		allowed, retryAfter := deviceRateLimiter.checkLimit("request-code-ip", c.ClientIP(), c.ClientIP())
		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "请求过于频繁，请稍后重试",
				"data": gin.H{
					"retry_after": int(retryAfter.Seconds()),
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ConfirmBindRateLimit confirm-bind 接口限速中间件
func ConfirmBindRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		// 使用 ShouldBindBodyWith 缓存请求体
		var req confirmBindRequest
		if err := c.ShouldBindBodyWith(&req, binding.JSON); err == nil {
			// 检查 MAC 限速
			if req.MAC != "" {
				allowed, retryAfter := deviceRateLimiter.checkLimit("confirm-bind-mac", req.MAC, c.ClientIP())
				if !allowed {
					c.JSON(http.StatusTooManyRequests, gin.H{
						"code":    429,
						"message": "请求过于频繁，请稍后重试",
						"data": gin.H{
							"retry_after": int(retryAfter.Seconds()),
						},
					})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// BindRateLimit bind 接口限速中间件（用户级）
func BindRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		userKey := toString(username)

		if userKey != "" {
			allowed, retryAfter := deviceRateLimiter.checkLimit("bind-user", userKey, c.ClientIP())
			if !allowed {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"code":    429,
					"message": "请求过于频繁，请稍后重试",
					"data": gin.H{
						"retry_after": int(retryAfter.Seconds()),
					},
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// SubmitConfigRateLimit submit-config 接口限速中间件（用户级）
func SubmitConfigRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		username, _ := c.Get("username")
		userKey := toString(username)

		if userKey != "" {
			allowed, retryAfter := deviceRateLimiter.checkLimit("submit-config-user", userKey, c.ClientIP())
			if !allowed {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"code":    429,
					"message": "请求过于频繁，请稍后重试",
					"data": gin.H{
						"retry_after": int(retryAfter.Seconds()),
					},
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// PublicRelaySearchRateLimit 公共中继台查询接口限速中间件（IP 级）
func PublicRelaySearchRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		allowed, retryAfter := deviceRateLimiter.checkLimit("public-relay-search-ip", clientIP, clientIP)
		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "请求过于频繁，请稍后重试",
				"data": gin.H{
					"retry_after": int(retryAfter.Seconds()),
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PublicClientResourceRateLimit limits anonymous manifest and download-link
// requests independently from relay discovery.
func PublicClientResourceRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}

		allowed, retryAfter := deviceRateLimiter.checkLimit("public-client-resource-ip", c.ClientIP(), c.ClientIP())
		if !allowed {
			c.Header("Retry-After", intToStr(maxInt(1, int(retryAfter.Seconds()))))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "请求过于频繁，请稍后重试",
				"data": gin.H{
					"retry_after": maxInt(1, int(retryAfter.Seconds())),
				},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// CaptchaRateLimit 图片验证码接口限速中间件（IP 级）
func CaptchaRateLimit() gin.HandlerFunc {
	return DeviceRateLimit([]string{"captcha-ip-burst", "captcha-ip-minute"}, func(c *gin.Context) map[string]string {
		return map[string]string{
			"ip": c.ClientIP(),
		}
	})
}

type accessDiscoveryTokenRequest struct {
	Username string `json:"username"`
}

func AccessDiscoveryTokenRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if deviceRateLimiter == nil {
			c.Next()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var req accessDiscoveryTokenRequest
		_ = c.ShouldBindBodyWith(&req, binding.JSON)
		ip := c.ClientIP()
		keys := map[string]string{"ip": ip}
		if principal := accessDiscoveryTokenPrincipalKey(ip, req.Username); principal != "" {
			// The username is still unauthenticated here. Scope this bucket to
			// the source IP so one remote client cannot exhaust another user's
			// discovery-token allowance by submitting their username.
			keys["user"] = principal
		}
		applyRateLimitRules(c, []string{"access-discovery-token-ip-burst", "access-discovery-token-ip-minute", "access-discovery-token-user"}, keys)
	}
}

func accessDiscoveryTokenPrincipalKey(ip, username string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	if ip == "" || username == "" {
		return ""
	}
	return ip + "\x00" + username
}

func AccessDiscoveryListIPRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		applyRateLimitRules(c, []string{"access-discovery-list-ip"}, map[string]string{"ip": c.ClientIP()})
	}
}

func AccessDiscoveryListUserRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, _ := c.Get("username")
		applyRateLimitRules(c, []string{"access-discovery-list-user"}, map[string]string{"user": strings.ToLower(toString(username))})
	}
}

// GroupJoinPasswordRateLimit limits private-group password attempts by both
// source IP and authenticated user. The endpoint is authenticated, but a
// leaked account must not be able to make unbounded online password guesses.
func GroupJoinPasswordRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if CheckGroupJoinPasswordRateLimit(c) {
			c.Next()
		}
	}
}

// CheckGroupJoinPasswordRateLimit applies the shared private-group password
// budget without advancing the Gin handler chain. Mixed-path handlers can
// charge only requests that actually attempt password verification.
func CheckGroupJoinPasswordRateLimit(c *gin.Context) bool {
	if c == nil {
		return false
	}
	username, _ := c.Get("username")
	return checkRateLimitRules(c, []string{"group-join-password-ip", "group-join-password-user"}, map[string]string{
		"ip":   c.ClientIP(),
		"user": strings.ToLower(toString(username)),
	})
}

func applyRateLimitRules(c *gin.Context, ruleNames []string, keys map[string]string) {
	if checkRateLimitRules(c, ruleNames, keys) {
		c.Next()
	}
}

func checkRateLimitRules(c *gin.Context, ruleNames []string, keys map[string]string) bool {
	if deviceRateLimiter == nil {
		return true
	}
	for _, ruleName := range ruleNames {
		rule, exists := deviceRateLimiter.rules[ruleName]
		if !exists {
			continue
		}
		value := keys[rule.Key]
		if value == "" {
			continue
		}
		allowed, retryAfter := deviceRateLimiter.checkLimit(ruleName, value, c.ClientIP())
		if !allowed {
			c.Header("Retry-After", intToStr(maxInt(1, int(retryAfter.Seconds()))))
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "message": "请求过于频繁，请稍后重试", "data": gin.H{"retry_after": maxInt(1, int(retryAfter.Seconds()))}})
			c.Abort()
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// toString 将 interface{} 转换为字符串
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int:
		return intToStr(val)
	case uint:
		return uintToStr(val)
	case int64:
		return int64ToStr(val)
	case uint64:
		return uint64ToStr(val)
	default:
		return ""
	}
}

// 简单的整数转字符串函数，避免导入 strconv
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var neg bool
	if n < 0 {
		neg = true
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func uintToStr(n uint) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func int64ToStr(n int64) string {
	return intToStr(int(n))
}

func uint64ToStr(n uint64) string {
	return uintToStr(uint(n))
}
