package jwt

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SecretMinLength 密钥最小长度
const SecretMinLength = 32

// AccessTokenTTL Access Token 默认有效期（短时效）。
const AccessTokenTTL = 3 * time.Hour

const (
	TokenUseAccess        = "access"
	TokenUseEdgeDiscovery = "edge-discovery"
	EdgeDiscoveryAudience = "draarl-edge-discovery"
	EdgeDiscoveryTokenTTL = 5 * time.Minute
)

// jwtSecret 由 SetSecret 初始化（main.go 启动时调用）。
// 【安全修复】移除 nrl fork 遗留的公开弱默认密钥 "nrl1234"：未初始化即
// fail-fast，任何绕过初始化流程的入口都无法用公开密钥伪造 token。
var jwtSecret struct {
	sync.RWMutex
	value []byte
}

// ErrInvalidToken 无效令牌错误。
var ErrInvalidToken = errors.New("invalid token")

// secret 返回当前 JWT 密钥；未初始化返回 nil。
func secret() []byte {
	jwtSecret.RLock()
	defer jwtSecret.RUnlock()
	return jwtSecret.value
}

// isSecretInitialized 判断 JWT 密钥是否已初始化。
func isSecretInitialized() bool {
	return len(secret()) > 0
}

// Claims JWT声明
type Claims struct {
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	TokenUse string   `json:"token_use,omitempty"`
	jwt.RegisteredClaims
}

// SetSecret 设置JWT密钥，密钥长度必须至少32字符
func SetSecret(secret string) error {
	if len(secret) < SecretMinLength {
		return fmt.Errorf("JWT密钥长度不足，当前%d字符，最少需要%d字符", len(secret), SecretMinLength)
	}
	jwtSecret.Lock()
	jwtSecret.value = []byte(secret)
	jwtSecret.Unlock()
	return nil
}

// GenerateToken 生成JWT令牌
func GenerateToken(username string, roles []string) (string, error) {
	now := time.Now()
	expireTime := now.Add(AccessTokenTTL)

	claims := Claims{
		Username: username,
		Roles:    roles,
		TokenUse: TokenUseAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "draarl",
		},
	}

	if !isSecretInitialized() {
		return "", errors.New("JWT密钥未初始化，禁止签发令牌")
	}
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := tokenClaims.SignedString(secret())

	return token, err
}

func GenerateEdgeDiscoveryToken(username string, ttl time.Duration) (string, time.Time, error) {
	if username == "" {
		return "", time.Time{}, errors.New("discovery token username is required")
	}
	if ttl <= 0 || ttl > EdgeDiscoveryTokenTTL {
		ttl = EdgeDiscoveryTokenTTL
	}
	now := time.Now()
	expiresAt := now.Add(ttl)
	claims := Claims{
		Username: username,
		TokenUse: TokenUseEdgeDiscovery,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{EdgeDiscoveryAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "draarl",
			Subject:   username,
		},
	}
	if !isSecretInitialized() {
		return "", time.Time{}, errors.New("JWT密钥未初始化，禁止签发令牌")
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret())
	return token, expiresAt, err
}

// ParseToken 解析JWT令牌
func ParseToken(token string) (*Claims, error) {
	if !isSecretInitialized() {
		return nil, errors.New("JWT密钥未初始化，禁止解析令牌")
	}
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected JWT signing method")
		}
		return secret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("draarl"), jwt.WithIssuedAt(), jwt.WithExpirationRequired())

	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*Claims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}

	return nil, err
}

// ValidateToken 验证令牌
func ValidateToken(tokenString string) (*Claims, error) {
	return ValidateAccessToken(tokenString)
}

func ValidateAccessToken(tokenString string) (*Claims, error) {
	claims, err := ParseToken(tokenString)
	if err != nil || (claims.TokenUse != "" && claims.TokenUse != TokenUseAccess) {
		return nil, errors.New("令牌错误，登录超时，请重新登录")
	}
	return claims, nil
}

func ValidateEdgeDiscoveryToken(tokenString string) (*Claims, error) {
	claims, err := ParseToken(tokenString)
	if err != nil || claims.TokenUse != TokenUseEdgeDiscovery || claims.Subject != claims.Username {
		return nil, errors.New("invalid edge discovery token")
	}
	foundAudience := false
	for _, audience := range claims.Audience {
		if audience == EdgeDiscoveryAudience {
			foundAudience = true
			break
		}
	}
	if !foundAudience {
		return nil, errors.New("invalid edge discovery audience")
	}
	return claims, nil
}

// GetUsername 从令牌获取用户名
func GetUsername(tokenString string) (string, error) {
	claims, err := ValidateAccessToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Username, nil
}

// RefreshToken 保留旧导出符号，但禁止无状态续期。
//
// Refresh 必须通过 internal/handler 的 refresh-token endpoint 完成，该
// endpoint 会校验服务端 session、轮换令牌并执行重放检测。保留此函数
// 仅为避免旧的编译依赖在升级时突然失效；它永远不会签发新令牌。
func RefreshToken(tokenString string) (string, error) {
	if _, err := ValidateAccessToken(tokenString); err != nil {
		return "", err
	}
	return "", errors.New("stateless token refresh is disabled; use the refresh-token endpoint")
}

// MustParseToken 强制解析令牌（兼容函数）。
// 为避免在 goroutine 中触发不可恢复 panic，解析失败时返回 nil。
func MustParseToken(tokenString string) *Claims {
	claims, err := ValidateAccessToken(tokenString)
	if err != nil {
		return nil
	}
	return claims
}
