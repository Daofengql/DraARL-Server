package handler

import (
	"net/http"
	"strings"
	"time"

	"draarl/internal/config"
	"draarl/pkg/jwt"

	"github.com/gin-gonic/gin"
)

const (
	wsTokenCookieName = "ws_token"
	wsTokenCookieTTL  = jwt.AccessTokenTTL
)

func setWSTokenCookie(c *gin.Context, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     wsTokenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(wsTokenCookieTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(c),
	})
}

func clearWSTokenCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     wsTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(c),
	})
}

func shouldUseSecureCookie(c *gin.Context) bool {
	if c.Request != nil && c.Request.TLS != nil {
		return true
	}

	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); strings.EqualFold(proto, "https") {
		return true
	}

	cfg := config.Config
	return cfg != nil && cfg.IsProduction()
}

// SyncWSTokenCookie 从 Authorization header 同步 ws_token（HttpOnly）到 Cookie。
func SyncWSTokenCookie(c *gin.Context) {
	token, ok := parseBearerToken(c.GetHeader("Authorization"))
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未提供认证令牌",
		})
		return
	}

	setWSTokenCookie(c, token)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
	})
}

// ClearWSTokenCookie 清理 ws_token Cookie。
func ClearWSTokenCookie(c *gin.Context) {
	// 401 场景下同时清理 refresh_token，避免残留会话导致状态混乱
	clearRefreshTokenCookie(c)
	clearWSTokenCookie(c)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
	})
}

func parseBearerToken(authHeader string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}

	return token, true
}
