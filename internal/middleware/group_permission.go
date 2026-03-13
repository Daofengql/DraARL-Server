package middleware

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
)

// RequireGroupOwner 要求群组创建者权限的中间件
func RequireGroupOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未认证",
			})
			c.Abort()
			return
		}

		// 获取群组ID
		groupIDStr := c.Param("id")
		groupID, err := strconv.Atoi(groupIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的群组ID",
			})
			c.Abort()
			return
		}

		// 获取用户信息
		userRepo := gormdb.NewUserRepository()
		currentUser, err := userRepo.GetUserByName(username.(string))
		if err != nil || currentUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 管理员可以绕过群组创建者检查
		if hasRole(currentUser, "admin") {
			c.Set("user", currentUser)
			c.Next()
			return
		}

		// 获取群组信息
		groupRepo := gormdb.NewGroupRepository()
		group, err := groupRepo.GetGroupByID(groupID)
		if err != nil || group == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "群组不存在",
			})
			c.Abort()
			return
		}

		// 检查是否是群组创建者
		if group.OwerID != currentUser.ID {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "需要群组创建者权限",
			})
			c.Abort()
			return
		}

		// 将用户信息存入 context
		c.Set("user", currentUser)
		c.Set("group", group)
		c.Next()
	}
}

// RequireGroupMember 要求已验证群组成员权限的中间件
func RequireGroupMember() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未认证",
			})
			c.Abort()
			return
		}

		// 获取群组ID
		groupIDStr := c.Param("id")
		groupID, err := strconv.Atoi(groupIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的群组ID",
			})
			c.Abort()
			return
		}

		// 获取用户信息
		userRepo := gormdb.NewUserRepository()
		currentUser, err := userRepo.GetUserByName(username.(string))
		if err != nil || currentUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 管理员可以绕过群组成员检查
		if hasRole(currentUser, "admin") {
			c.Set("user", currentUser)
			c.Next()
			return
		}

		// 获取群组信息
		groupRepo := gormdb.NewGroupRepository()
		group, err := groupRepo.GetGroupByID(groupID)
		if err != nil || group == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "群组不存在",
			})
			c.Abort()
			return
		}

		// 公开群组所有人可查看
		if group.Type == 1 {
			c.Set("user", currentUser)
			c.Set("group", group)
			c.Next()
			return
		}

		// 私有群组需要已验证
		memberRepo := gormdb.NewGroupMemberRepository()
		isVerified := memberRepo.IsVerifiedMember(groupID, currentUser.ID)
		if !isVerified {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "需要先验证加入该群组",
			})
			c.Abort()
			return
		}

		// 将用户信息存入 context
		c.Set("user", currentUser)
		c.Set("group", group)
		c.Next()
	}
}

// RequireAdminOrOwner 要求管理员或群组创建者权限的中间件
func RequireAdminOrOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未认证",
			})
			c.Abort()
			return
		}

		// 获取群组ID
		groupIDStr := c.Param("id")
		groupID, err := strconv.Atoi(groupIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "无效的群组ID",
			})
			c.Abort()
			return
		}

		// 获取用户信息
		userRepo := gormdb.NewUserRepository()
		currentUser, err := userRepo.GetUserByName(username.(string))
		if err != nil || currentUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			c.Abort()
			return
		}

		// 管理员可以绕过检查
		if hasRole(currentUser, "admin") {
			c.Set("user", currentUser)
			c.Next()
			return
		}

		// 获取群组信息
		groupRepo := gormdb.NewGroupRepository()
		group, err := groupRepo.GetGroupByID(groupID)
		if err != nil || group == nil {
			log.Printf("获取群组失败: %v", err)
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "群组不存在",
			})
			c.Abort()
			return
		}

		// 检查是否是群组创建者
		if group.OwerID != currentUser.ID {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "需要管理员或群组创建者权限",
			})
			c.Abort()
			return
		}

		// 将用户信息存入 context
		c.Set("user", currentUser)
		c.Set("group", group)
		c.Next()
	}
}
