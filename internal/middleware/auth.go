package middleware

import (
	"log"
	"net/http"
	"strings"

	gormdb "draarl/internal/gormdb"
	"draarl/pkg/jwt"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware JWT 认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Authorization header 获取 token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未提供认证令牌",
			})
			c.Abort()
			return
		}

		// 解析 Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "认证令牌格式错误",
			})
			c.Abort()
			return
		}

		token := parts[1]

		// 验证 token
		claims, err := jwt.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": err.Error(),
			})
			c.Abort()
			return
		}

		// 将用户信息存入 context
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)

		repo := gormdb.NewUserRepository()
		user, err := repo.GetUserByName(claims.Username)
		if err != nil {
			log.Printf("获取用户信息失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取用户信息失败",
			})
			c.Abort()
			return
		}
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}
		if user.Status != 1 {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "用户已被禁用",
			})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("user_callsign", user.CallSign)

		c.Next()
	}
}

// RequireAdmin 要求管理员权限的中间件
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未认证",
			})
			c.Abort()
			return
		}

		user, ok := c.Get("user")
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 检查是否有 admin 角色
		if !hasRole(user, "admin") {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "需要管理员权限",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireApproved 要求用户已审核通过的中间件
// 用于限制待审核用户操作设备和群组
func RequireApproved() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未认证",
			})
			c.Abort()
			return
		}

		userValue, ok := c.Get("user")
		user, ok := userValue.(*gormdb.User)
		if !ok || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 检查用户是否已审核通过
		if user.ApprovalStatus != 1 {
			// 管理员可以绕过审核限制
			if !hasRole(user, "admin") {
				c.JSON(http.StatusForbidden, gin.H{
					"code":    403,
					"message": "您的账号正在审核中，请等待管理员审核通过后再操作",
					"data": gin.H{
						"approval_status": user.ApprovalStatus,
						"review_note":     user.ReviewNote,
					},
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// hasRole 检查用户是否有指定角色（使用 User.HasRole() 方法）
func hasRole(user interface{}, role string) bool {
	type UserWithRoles interface {
		HasRole(string) bool
	}

	// 检查是否有 HasRole 方法
	if u, ok := user.(UserWithRoles); ok {
		return u.HasRole(role)
	}
	return false
}

// LoadUserInfo 加载用户信息到 context 的中间件
// 在 AuthMiddleware 之后使用，会查询数据库获取 user_id 并存入 context
func LoadUserInfo() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("username")
		if !exists {
			c.Next()
			return
		}

		userValue, ok := c.Get("user")
		user, ok := userValue.(*gormdb.User)
		if !ok || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 将用户ID存入 context
		c.Set("user_id", user.ID)
		c.Set("user_callsign", user.CallSign)
		c.Next()
	}
}
