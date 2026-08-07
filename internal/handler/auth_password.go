package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func ChangeOwnPassword(c *gin.Context) {
	var req ChangeOwnPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	username, _ := c.Get("username")
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "原密码错误",
		})
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// 更新密码
	if err := repo.UpdateUserPassword(user.ID, string(hashedPassword)); err != nil {
		log.Printf("更新密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码修改失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户修改自己的密码: %s (%s)", user.Name, user.CallSign),
		"password_change",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码修改成功",
	})
}

func UpdateUserPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	var req UpdateUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewUserRepository()

	// 获取当前用户信息
	username, _ := c.Get("username")
	currentUser, err := repo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取目标用户（检查权限）
	targetUser, err := repo.GetUserByID(id)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "目标用户不存在",
		})
		return
	}

	// 检查权限：只有管理员或用户本人可以修改密码
	isAdmin := hasRoleGORM(currentUser, "admin")
	isSelf := currentUser.ID == id

	if !isAdmin && !isSelf {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "权限不足",
		})
		return
	}

	// 如果是修改自己的密码，需要验证旧密码
	if isSelf && !isAdmin {
		if err := bcrypt.CompareHashAndPassword([]byte(currentUser.Password), []byte(req.OldPassword)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "原密码错误",
			})
			return
		}
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// 更新密码
	if err := repo.UpdateUserPassword(id, string(hashedPassword)); err != nil {
		log.Printf("更新密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码修改失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), targetUser.ID, targetUser.Name)
	}

	// 记录审计日志
	if isAdmin {
		oplog.AddLog(
			fmt.Sprintf("管理员重置用户密码: %s (%s)", targetUser.Name, targetUser.CallSign),
			"password_reset",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	} else {
		oplog.AddLog(
			fmt.Sprintf("用户修改自己的密码: %s (%s)", currentUser.Name, currentUser.CallSign),
			"password_change",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码修改成功",
		"data": gin.H{
			"id": id,
		},
	})
}

// GetUserDetail 获取用户详情
