package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	authstore "draarl/internal/auth"
	gormdb "draarl/internal/gormdb"
	"draarl/pkg/jwt"

	"github.com/gin-gonic/gin"
)

const (
	refreshTokenCookieName = "refresh_token"
	refreshTokenCookiePath = "/api/auth"
	refreshTokenTTL        = 14 * 24 * time.Hour
)

type issuedAuthTokens struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresIn  int64
	RefreshExpiresIn int64
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshToken 刷新短时效 access token，并执行 refresh token 轮换。
func RefreshToken(c *gin.Context) {
	rawToken, fromCookie, bindErr := readRefreshTokenFromRequest(c)
	if bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}
	if rawToken == "" {
		clearRefreshTokenCookie(c)
		clearWSTokenCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "refresh_token_required",
		})
		return
	}

	tokenHash := hashRefreshToken(rawToken)
	store := authstore.GetRefreshTokenStore()
	stored, err := store.GetByTokenHash(tokenHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "刷新令牌校验失败",
		})
		return
	}
	if stored == nil {
		clearRefreshTokenCookie(c)
		clearWSTokenCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "invalid_refresh_token",
		})
		return
	}

	now := time.Now()
	if stored.RevokedAt != nil {
		// 令牌重放：发现替换链，吊销该用户所有有效 refresh token。
		if stored.ReplacedByHash != "" {
			_ = store.RevokeAllByUser(stored.UserID, "reuse_detected", now)
		}
		clearRefreshTokenCookie(c)
		clearWSTokenCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "refresh_token_revoked",
		})
		return
	}

	if now.After(stored.ExpiresAt) {
		_ = store.RevokeByTokenHash(tokenHash, "expired", now)
		clearRefreshTokenCookie(c)
		clearWSTokenCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "refresh_token_expired",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByID(stored.UserID)
	if err != nil || user == nil || user.Status != 1 || user.ApprovalStatus != 1 {
		_ = store.RevokeAllByUser(stored.UserID, "user_invalid", now)
		clearRefreshTokenCookie(c)
		clearWSTokenCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "user_invalid",
		})
		return
	}

	issued, err := rotateAuthTokens(c, user, stored)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "刷新登录态失败",
		})
		return
	}

	data := gin.H{
		"token":      issued.AccessToken,
		"expires_in": issued.AccessExpiresIn,
	}
	// 非 Cookie 客户端（如桌面/移动端）需要显式拿到新 refresh_token。
	if !fromCookie {
		data["refresh_token"] = issued.RefreshToken
		data["refresh_expires_in"] = issued.RefreshExpiresIn
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    data,
	})
}

func issueAuthTokens(c *gin.Context, user *gormdb.User) (*issuedAuthTokens, error) {
	accessToken, err := jwt.GenerateToken(user.Name, user.GetRoles())
	if err != nil {
		return nil, err
	}

	refreshToken, refreshHash, err := generateRefreshTokenPair()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	store := authstore.GetRefreshTokenStore()
	if err := store.Create(&authstore.RefreshTokenRecord{
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: now.Add(refreshTokenTTL),
		CreatedIP: c.ClientIP(),
		UserAgent: trimUserAgent(c.Request.UserAgent()),
	}); err != nil {
		return nil, err
	}

	setWSTokenCookie(c, accessToken)
	setRefreshTokenCookie(c, refreshToken)

	return &issuedAuthTokens{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresIn:  int64(jwt.AccessTokenTTL / time.Second),
		RefreshExpiresIn: int64(refreshTokenTTL / time.Second),
	}, nil
}

func rotateAuthTokens(c *gin.Context, user *gormdb.User, oldToken *authstore.RefreshTokenRecord) (*issuedAuthTokens, error) {
	accessToken, err := jwt.GenerateToken(user.Name, user.GetRoles())
	if err != nil {
		return nil, err
	}

	newRefreshToken, newHash, err := generateRefreshTokenPair()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	newRecord := &authstore.RefreshTokenRecord{
		UserID:    user.ID,
		TokenHash: newHash,
		ExpiresAt: now.Add(refreshTokenTTL),
		CreatedIP: c.ClientIP(),
		UserAgent: trimUserAgent(c.Request.UserAgent()),
	}

	store := authstore.GetRefreshTokenStore()
	if err := store.Rotate(oldToken.TokenHash, newRecord, "rotated", now); err != nil {
		return nil, err
	}

	setWSTokenCookie(c, accessToken)
	setRefreshTokenCookie(c, newRefreshToken)

	return &issuedAuthTokens{
		AccessToken:      accessToken,
		RefreshToken:     newRefreshToken,
		AccessExpiresIn:  int64(jwt.AccessTokenTTL / time.Second),
		RefreshExpiresIn: int64(refreshTokenTTL / time.Second),
	}, nil
}

func revokeCurrentRefreshToken(c *gin.Context, reason string) {
	rawToken, _, _ := readRefreshTokenFromRequest(c)
	if rawToken == "" {
		return
	}

	_ = authstore.GetRefreshTokenStore().
		RevokeByTokenHash(hashRefreshToken(rawToken), reason, time.Now())
}

func setRefreshTokenCookie(c *gin.Context, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		Path:     refreshTokenCookiePath,
		MaxAge:   int(refreshTokenTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   shouldUseSecureCookie(c),
	})
}

func clearRefreshTokenCookie(c *gin.Context) {
	secure := shouldUseSecureCookie(c)

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		Path:     refreshTokenCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})

	// 兼容历史路径，确保残留 cookie 被彻底清掉。
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func readRefreshTokenFromRequest(c *gin.Context) (token string, fromCookie bool, bindErr error) {
	if cookie, err := c.Request.Cookie(refreshTokenCookieName); err == nil {
		if val := strings.TrimSpace(cookie.Value); val != "" {
			return val, true, nil
		}
	}

	// 空请求体视为“未提供 body token”。
	if c.Request.ContentLength == 0 {
		return "", false, nil
	}

	var req refreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return "", false, nil
		}
		return "", false, err
	}

	return strings.TrimSpace(req.RefreshToken), false, nil
}

func generateRefreshTokenPair() (plainToken string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}

	plainToken = base64.RawURLEncoding.EncodeToString(buf)
	tokenHash = hashRefreshToken(plainToken)
	return plainToken, tokenHash, nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func trimUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if len(userAgent) <= 512 {
		return userAgent
	}
	return userAgent[:512]
}
