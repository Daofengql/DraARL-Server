package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/protocol"
	"draarl/pkg/cache"
	"draarl/pkg/minio"

	"github.com/gin-gonic/gin"
)

func GetUserPublicInfo(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// 检查用户是否已登录
	_, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	if userCache != nil {
		user, err = userCache.GetUserByID(ctx, id)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByID(id)
	}
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 只返回公开信息
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"created_at":   user.CreateTime,
			"status":       user.Status,
		},
	})
}

// GetUserPublicInfoByName 通过用户名获取用户公开信息（任何登录用户名访问）

func GetCurrentUser(c *gin.Context) {
	username, _ := c.Get("username")

	var user *gormdb.User
	var err error

	// 尝试从缓存获取用户信息
	userCache := cache.GetUserCache()
	if userCache != nil {
		user, err = userCache.GetUserByName(c.Request.Context(), username.(string))
	} else {
		// 缓存不可用，直接从数据库查询
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByName(username.(string))
	}

	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取用户 Web 端的群组偏好
	userRepo := gormdb.NewUserRepository()
	lastGroupID, _ := userRepo.GetUserLastGroupID(user.ID, protocol.DraARLDevModelBrowser)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":              user.ID,
			"username":        user.Name,
			"nickname":        user.NickName,
			"callsign":        user.CallSign,
			"phone":           user.Phone,
			"email":           user.Email,
			"email_verified":  user.EmailVerified,
			"address":         user.Address,
			"introduction":    user.Introduction,
			"avatar":          minio.GetAvatarURL(user.Avatar),
			"avatar_thumb":    minio.GetAvatarThumbURL(user.Avatar),
			"sex":             user.Sex,
			"birthday":        user.Birthday,
			"role":            getRoleNameFromUser(user),
			"roles":           user.Roles,
			"isAdmin":         hasRoleGORM(user, "admin"),
			"status":          user.Status,
			"approval_status": user.ApprovalStatus,
			"review_note":     user.ReviewNote,
			"dmrid":           user.DMRID,
			"mdcid":           user.MDCID,
			"alarm_msg":       user.AlarmMsg,
			"last_group_id":   lastGroupID, // 从设备偏好表获取
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":          user.LastLoginIP,
			"last_login_ip_location": getIPLocation(user.LastLoginIP),
			"login_err_times":        user.LoginErrTimes,
			"created_at":             user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":             user.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// getRoleNameFromUser 从用户获取角色名称

func GetUserPublicInfoByName(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户名",
		})
		return
	}

	// 检查用户是否已登录
	_, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	var err error
	if userCache != nil {
		user, err = userCache.GetUserByName(ctx, username)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByName(username)
	}
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 只返回公开信息
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"nickname":     user.NickName,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"created_at":   user.CreateTime,
			"status":       user.Status,
		},
	})
}

// UpdateProfileRequest 更新个人资料请求

func UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
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

	// 更新字段
	if req.NickName != "" {
		user.NickName = req.NickName
	}
	if req.Phone != "" {
		user.Phone = req.Phone
	}
	if req.Address != "" {
		user.Address = req.Address
	}
	if req.Introduction != "" {
		user.Introduction = req.Introduction
	}
	// 性别字段处理：
	// - 0 = 保密
	// - 1 = 男
	// - 2 = 女
	if req.Sex != nil {
		user.Sex = *req.Sex
	}
	if req.Birthday != "" {
		user.Birthday = req.Birthday
	}
	if req.DMRID != nil {
		user.DMRID = *req.DMRID
	}
	if req.MDCID != "" {
		user.MDCID = req.MDCID
	}
	if req.AlarmMsg != nil {
		user.AlarmMsg = *req.AlarmMsg
	}

	if err := repo.UpdateUser(user); err != nil {
		log.Printf("更新个人资料失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新个人资料失败",
		})
		return
	}

	// 使用户缓存失效
	if userCache := cache.GetUserCache(); userCache != nil {
		_ = userCache.InvalidateUser(c.Request.Context(), user.ID, user.Name)
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("用户更新个人资料: %s (%s)", user.Name, user.CallSign),
		"profile_update",
		user.ID,
		user.Name,
		user.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data": gin.H{
			"id":           user.ID,
			"username":     user.Name,
			"nickname":     user.NickName,
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"address":      user.Address,
			"introduction": user.Introduction,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"sex":          user.Sex,
			"birthday":     user.Birthday,
			"dmrid":        user.DMRID,
			"mdcid":        user.MDCID,
			"alarm_msg":    user.AlarmMsg,
			"role":         getRoleNameFromUser(user),
			"roles":        user.Roles,
			"status":       user.Status,
			"isAdmin":      hasRoleGORM(user, "admin"),
			"last_login_time": func() string {
				if user.LastLoginTime != nil {
					return user.LastLoginTime.Format("2006-01-02 15:04:05")
				}
				return ""
			}(),
			"last_login_ip":          user.LastLoginIP,
			"last_login_ip_location": getIPLocation(user.LastLoginIP),
			"login_err_times":        user.LoginErrTimes,
			"created_at":             user.CreateTime.Format("2006-01-02 15:04:05"),
			"updated_at":             user.UpdateTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// ChangeOwnPasswordRequest 修改自己的密码请求

func GetUserDetail(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	ctx := c.Request.Context()
	userCache := cache.GetUserCache()

	var user *gormdb.User
	if userCache != nil {
		user, err = userCache.GetUserByID(ctx, id)
	} else {
		repo := gormdb.NewUserRepository()
		user, err = repo.GetUserByID(id)
	}
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           user.ID,
			"name":         user.Name,
			"nickname":     user.NickName,
			"callsign":     user.CallSign,
			"phone":        user.Phone,
			"status":       user.Status,
			"isAdmin":      hasRoleGORM(user, "admin"),
			"roles":        user.Roles,
			"avatar":       minio.GetAvatarURL(user.Avatar),
			"avatar_thumb": minio.GetAvatarThumbURL(user.Avatar),
			"introduction": user.Introduction,
			"address":      user.Address,
			"sex":          user.Sex,
			"birthday":     user.Birthday,
		},
	})
}

// GetUserPublicInfo 获取用户公开信息（任何登录用户可访问）
