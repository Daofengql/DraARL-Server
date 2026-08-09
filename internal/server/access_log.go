package server

import (
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// accessLogMiddleware 自定义访问日志，避免敏感 query 参数写入日志。
func accessLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		method := c.Request.Method
		path := sanitizeLogPath(c.Request.URL.Path, c.Request.URL.RawQuery)
		clientIP := c.ClientIP()

		c.Next()

		latency := time.Since(start).Truncate(time.Millisecond)
		status := c.Writer.Status()
		log.Printf("[HTTP] %3d | %13s | %15s | %-7s %s", status, latency.String(), clientIP, method, path)
	}
}

func sanitizeLogPath(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	return path + "?" + redactSensitiveQuery(rawQuery)
}

func hasTokenLikeQuery(rawQuery string) bool {
	if rawQuery == "" {
		return false
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}

	for key := range values {
		if isTokenLikeQueryKey(key) {
			return true
		}
	}
	return false
}

func redactSensitiveQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[invalid_query]"
	}

	changed := false
	for key := range values {
		if isSensitiveQueryKey(key) {
			values.Set(key, "[REDACTED]")
			changed = true
		}
	}

	if !changed {
		return rawQuery
	}
	return values.Encode()
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "access_token", "refresh_token", "id_token", "jwt", "authorization",
		"api_key", "apikey", "secret", "signature", "sig",
		"code", "password", "session_id", "email_code", "captcha_code":
		return true
	default:
		return false
	}
}

func isTokenLikeQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "access_token", "refresh_token", "id_token", "jwt", "authorization":
		return true
	default:
		return false
	}
}
