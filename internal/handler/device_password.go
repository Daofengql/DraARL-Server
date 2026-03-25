package handler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/pkg/crypto"
)

// GetDevicePassword 获取设备密码（脱敏显示）
func GetDevicePassword(c *gin.Context) {
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

	// 如果设备密码为空，生成一个新的
	if user.DevicePassword == "" {
		devicePassword := generateDevicePassword()
		encryptedPassword, err := crypto.Encrypt(devicePassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "设备密码加密失败",
			})
			return
		}

		if err := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); err != nil {
			log.Printf("更新设备密码失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新设备密码失败",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "成功",
			"data": gin.H{
				"masked_password": protocol.MaskDevicePassword(devicePassword),
				"has_password":    true,
				"is_new":          true,
				"created_at":      user.UpdateTime.Format("2006-01-02 15:04:05"),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"masked_password": "********", // 已设置的密码不显示任何信息
			"has_password":    true,
			"is_new":          false,
			"created_at":      user.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// UpdateDevicePasswordRequest 修改设备密码请求
type UpdateDevicePasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

// UpdateDevicePassword 修改设备密码
func UpdateDevicePassword(c *gin.Context) {
	var req UpdateDevicePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 验证密码格式
	if !protocol.IsValidDevicePassword(req.NewPassword) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "设备密码格式错误，需要6-10位字母或数字",
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

	// AES 加密新密码
	encryptedPassword, err := crypto.Encrypt(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// 更新密码
	if err := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); err != nil {
		log.Printf("更新设备密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新设备密码失败",
		})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户修改设备准入密码: %s (%s)", user.Name, user.CallSign),
		"device_password_change",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "设备密码修改成功",
		"data": gin.H{
			"masked_password": protocol.MaskDevicePassword(req.NewPassword),
		},
	})
}

// RegenerateDevicePassword 重新生成设备密码
func RegenerateDevicePassword(c *gin.Context) {
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

	// 生成新的设备密码
	devicePassword := generateDevicePassword()
	encryptedPassword, err := crypto.Encrypt(devicePassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	// 更新密码
	if err := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); err != nil {
		log.Printf("更新设备密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新设备密码失败",
		})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户重新生成设备准入密码: %s (%s)", user.Name, user.CallSign),
		"device_password_regenerate",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "设备密码已重新生成",
		"data": gin.H{
			"device_password": devicePassword, // 仅显示一次
		},
	})
}
