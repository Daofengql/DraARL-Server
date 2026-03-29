package middleware

import (
	"net/http"
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
	mu     sync.RWMutex
	limits map[string]*RateLimitEntry // key: limitType:value -> entry

	// 预定义的限速规则
	rules map[string]RateLimitRule
}

// 全局限速器
var deviceRateLimiter *DeviceRateLimiter

// InitDeviceRateLimiter 初始化设备接口限速器
func InitDeviceRateLimiter() {
	deviceRateLimiter = &DeviceRateLimiter{
		limits: make(map[string]*RateLimitEntry),
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
				Limit:       1,
				Window:      10 * time.Second,
				Description: "同一 IP 每 10 秒 1 次",
			},
			"request-code-mac": {
				Key:         "mac",
				Limit:       1,
				Window:      time.Minute,
				Description: "同一 MAC 每分钟 1 次",
			},
			"confirm-bind-mac": {
				Key:         "mac",
				Limit:       1,
				Window:      5 * time.Second,
				Description: "同一 MAC 每 5 秒 1 次",
			},
			"bind-user": {
				Key:         "user",
				Limit:       5,
				Window:      time.Minute,
				Description: "同一用户每分钟 5 次",
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
		},
	}

	// 启动清理协程
	go deviceRateLimiter.cleanup()
}

// GetDeviceRateLimiter 获取全局限速器
func GetDeviceRateLimiter() *DeviceRateLimiter {
	return deviceRateLimiter
}

// checkLimit 检查是否超过限速
func (r *DeviceRateLimiter) checkLimit(ruleName, value string) (allowed bool, retryAfter time.Duration) {
	rule, exists := r.rules[ruleName]
	if !exists {
		return true, 0
	}

	key := ruleName + ":" + value

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	entry, exists := r.limits[key]

	if !exists || now.After(entry.ExpiresAt) {
		// 不存在或已过期，创建新条目
		r.limits[key] = &RateLimitEntry{
			Count:     1,
			ExpiresAt: now.Add(rule.Window),
		}
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

// cleanup 定期清理过期的限速条目
func (r *DeviceRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for key, entry := range r.limits {
			if now.After(entry.ExpiresAt) {
				delete(r.limits, key)
			}
		}
		r.mu.Unlock()
	}
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

			allowed, retryAfter := deviceRateLimiter.checkLimit(ruleName, value)
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
				allowed, retryAfter := deviceRateLimiter.checkLimit("pre-check-mac", req.MAC)
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
		allowed, retryAfter := deviceRateLimiter.checkLimit("pre-check-ip", c.ClientIP())
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
				allowed, retryAfter := deviceRateLimiter.checkLimit("request-code-mac", req.MAC)
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
		allowed, retryAfter := deviceRateLimiter.checkLimit("request-code-ip", c.ClientIP())
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
				allowed, retryAfter := deviceRateLimiter.checkLimit("confirm-bind-mac", req.MAC)
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
			allowed, retryAfter := deviceRateLimiter.checkLimit("bind-user", userKey)
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
			allowed, retryAfter := deviceRateLimiter.checkLimit("submit-config-user", userKey)
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
		allowed, retryAfter := deviceRateLimiter.checkLimit("public-relay-search-ip", clientIP)
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
