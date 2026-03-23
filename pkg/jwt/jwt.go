package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SecretMinLength 密钥最小长度
const SecretMinLength = 32

var jwtSecret = []byte("nrl1234")

// Claims JWT声明
type Claims struct {
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// SetSecret 设置JWT密钥，密钥长度必须至少32字符
func SetSecret(secret string) error {
	if len(secret) < SecretMinLength {
		return fmt.Errorf("JWT密钥长度不足，当前%d字符，最少需要%d字符", len(secret), SecretMinLength)
	}
	jwtSecret = []byte(secret)
	return nil
}

// GenerateToken 生成JWT令牌
func GenerateToken(username string, roles []string) (string, error) {
	now := time.Now()
	// 默认过期时间：30天
	expireTime := now.Add(30 * 24 * time.Hour)

	claims := Claims{
		username,
		roles,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expireTime),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "draarl",
		},
	}

	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := tokenClaims.SignedString(jwtSecret)

	return token, err
}

// ParseToken 解析JWT令牌
func ParseToken(token string) (*Claims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*Claims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}

	return nil, err
}

// ValidateToken 验证令牌
func ValidateToken(tokenString string) (*Claims, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return nil, errors.New("令牌错误，登录超时，请重新登录")
	}
	return claims, nil
}

// GetUsername 从令牌获取用户名
func GetUsername(tokenString string) (string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Username, nil
}

// RefreshToken 刷新令牌
func RefreshToken(tokenString string) (string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", err
	}
	return GenerateToken(claims.Username, claims.Roles)
}

// MustParseToken 强制解析令牌，失败则panic
func MustParseToken(tokenString string) *Claims {
	claims, err := ParseToken(tokenString)
	if err != nil {
		panic(fmt.Sprintf("failed to parse token: %v", err))
	}
	return claims
}
