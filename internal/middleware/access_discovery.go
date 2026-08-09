package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"draarl/internal/gormdb"
	appjwt "draarl/pkg/jwt"
)

const DiscoveryTokenUseContextKey = "discovery_token_use"

func AccessDiscoveryAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, tokenUse, err := validateDiscoveryAuthorization(c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "发现凭证无效或已过期"})
			c.Abort()
			return
		}

		user, err := gormdb.NewUserRepository().GetUserByName(claims.Username)
		if err != nil {
			log.Printf("查询接入点发现用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "认证服务暂时不可用"})
			c.Abort()
			return
		}
		if user == nil || user.Status != 1 || (user.ApprovalStatus != 1 && !user.HasRole("admin")) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "当前账号不可使用设备接入服务"})
			c.Abort()
			return
		}

		c.Set("username", user.Name)
		c.Set("user_id", user.ID)
		c.Set("user", user)
		c.Set(DiscoveryTokenUseContextKey, tokenUse)
		c.Next()
	}
}

func validateDiscoveryAuthorization(header string) (*appjwt.Claims, string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, "", appjwt.ErrInvalidToken
	}
	if claims, err := appjwt.ValidateAccessToken(parts[1]); err == nil {
		return claims, appjwt.TokenUseAccess, nil
	}
	claims, err := appjwt.ValidateEdgeDiscoveryToken(parts[1])
	if err != nil {
		return nil, "", err
	}
	return claims, appjwt.TokenUseEdgeDiscovery, nil
}
