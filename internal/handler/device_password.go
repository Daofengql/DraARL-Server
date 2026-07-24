package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/pkg/crypto"
	"github.com/gin-gonic/gin"
)

// revealDevicePassword decodes a user's device password. Empty or historical
// one-way values cannot be revealed, so they are replaced with a new AES-backed
// password, matching the existing self-service migration behavior.
func revealDevicePassword(repo *gormdb.UserRepository, user *gormdb.User) (password string, isNew bool, err error) {
	if user.DevicePassword != "" {
		password, legacy, decodeErr := crypto.DecodeDevicePassword(user.DevicePassword)
		if decodeErr != nil {
			return "", false, decodeErr
		}
		if !legacy && password != "" {
			return password, false, nil
		}
	}

	password = generateDevicePassword()
	encryptedPassword, err := crypto.Encrypt(password)
	if err != nil {
		return "", false, err
	}
	if err := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); err != nil {
		return "", false, err
	}
	return password, true, nil
}

func setCredentialResponseNoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
}

// AdminGetUserDevicePassword lets an administrator reveal one user's device
// password on demand. The user list never contains this sensitive value.
func AdminGetUserDevicePassword(c *gin.Context) {
	setCredentialResponseNoStore(c)
	adminUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	if !isAdminUser(adminUser) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "需要管理员权限",
		})
		return
	}

	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "无效的用户ID",
		})
		return
	}

	repo := gormdb.NewUserRepository()
	targetUser, err := repo.GetUserByID(userID)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "用户不存在",
		})
		return
	}

	devicePassword, isNew, err := revealDevicePassword(repo, targetUser)
	if err != nil {
		log.Printf("管理员读取用户设备密码失败: admin=%d target=%d err=%v", adminUser.ID, userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": "读取设备密码失败",
		})
		return
	}

	oplog.AddLog(
		fmt.Sprintf("管理员查看用户设备准入密码: target_user_id=%d, target_username=%s", targetUser.ID, targetUser.Name),
		"admin_device_password_view",
		adminUser.ID,
		adminUser.Name,
		adminUser.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "成功",
		"data": gin.H{
			"device_password": devicePassword,
			"is_new":          isNew,
		},
	})
}

// GetDevicePassword 获取设备密码
func GetDevicePassword(c *gin.Context) {
	setCredentialResponseNoStore(c)
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
				"device_password": devicePassword,
				"has_password":    true,
				"is_new":          true,
				"created_at":      user.UpdateTime.Format("2006-01-02 15:04:05"),
			},
		})
		return
	}

	// 解码已存在的密码（兼容历史 bcrypt，不可逆则自动重建）
	devicePassword, legacyPassword, err := crypto.DecodeDevicePassword(user.DevicePassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码解密失败",
		})
		return
	}
	if legacyPassword || devicePassword == "" {
		devicePassword = generateDevicePassword()
		encryptedPassword, encErr := crypto.Encrypt(devicePassword)
		if encErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "设备密码迁移失败",
			})
			return
		}
		if updateErr := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); updateErr != nil {
			log.Printf("迁移历史设备密码失败: %v", updateErr)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "设备密码迁移失败",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "成功",
			"data": gin.H{
				"device_password": devicePassword,
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
			"device_password": devicePassword,
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
	setCredentialResponseNoStore(c)
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
