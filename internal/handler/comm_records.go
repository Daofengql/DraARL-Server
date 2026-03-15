package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
	"nrllink/internal/udphub"
	minio_local "nrllink/pkg/minio"
)

// CommRecordResponse 通信记录响应结构（用于前端显示）
type CommRecordResponse struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	DeviceName  string `json:"device_name"`  // 通过联表查询获取：users.callsign + devices.ssid
	GroupID     *uint  `json:"group_id"`
	GroupName   string `json:"group_name"`   // 通过联表查询获取：public_groups.name
	UserID      *uint  `json:"user_id"`
	Username    string `json:"username"`     // 通过联表查询获取：users.nickname 或 users.name
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	DurationMs  int    `json:"duration_ms"`
	AudioPath   string `json:"audio_path"`
	AudioURL    string `json:"audio_url"`
	AudioSize   int64  `json:"audio_size"`
	Status      int    `json:"status"`
}

// CommRecordWithDetails 联表查询结果
type CommRecordWithDetails struct {
	ID          uint
	DeviceID    uint
	DeviceSSID  uint8
	OwnerCallSign string
	OwnerNickName string
	GroupID     *uint
	GroupName   string
	UserID      *uint
	UserCallSign string
	UserNickName string
	StartTime   time.Time
	EndTime     time.Time
	DurationMs  int
	AudioPath   string
	AudioSize   int64
	Status      int
}

// toCommRecordResponse 将联表查询结果转换为响应结构
func toCommRecordResponse(r CommRecordWithDetails) CommRecordResponse {
	audioURL := ""
	if r.AudioPath != "" {
		audioURL = minio_local.GetFileURL(r.AudioPath)
	}

	// 设备名称：呼号-SSID
	deviceName := ""
	if r.OwnerCallSign != "" {
		deviceName = r.OwnerCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		}
	}

	// 用户名：优先显示昵称
	username := r.UserNickName
	if username == "" {
		username = r.UserCallSign
	}

	return CommRecordResponse{
		ID:         r.ID,
		DeviceID:   r.DeviceID,
		DeviceName: deviceName,
		GroupID:    r.GroupID,
		GroupName:  r.GroupName,
		UserID:     r.UserID,
		Username:   username,
		StartTime:  r.StartTime.Format("2006-01-02 15:04:05"),
		EndTime:    r.EndTime.Format("2006-01-02 15:04:05"),
		DurationMs: r.DurationMs,
		AudioPath:  r.AudioPath,
		AudioURL:   audioURL,
		AudioSize:  r.AudioSize,
		Status:     r.Status,
	}
}

// GetCommRecords 获取通信记录列表（使用联表查询）
// 权限规则：
// - 管理员：可查看所有记录
// - 普通用户：只能查看自己设备的记录
func GetCommRecords(c *gin.Context) {
	// 获取分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	// 获取筛选参数
	deviceIDStr := c.Query("device_id")
	groupIDStr := c.Query("group_id")
	userIDStr := c.Query("user_id")

	db := gormdb.Get().Table("comm_records cr").
		Select(`
			cr.id, cr.device_id, cr.device_ssid, cr.group_id, cr.user_id,
			cr.start_time, cr.end_time, cr.duration_ms, cr.audio_path, cr.audio_size, cr.status,
			d_owner.callsign as owner_call_sign, d_owner.nickname as owner_nick_name,
			g.name as group_name,
			u.callsign as user_call_sign, u.nickname as user_nick_name
		`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Joins("LEFT JOIN users d_owner ON d.owner_id = d_owner.id").
		Joins("LEFT JOIN public_groups g ON cr.group_id = g.id").
		Joins("LEFT JOIN users u ON cr.user_id = u.id").
		Where("cr.status = ?", 2) // 只返回已完成的记录

	// 检查是否是管理员（优先从 user 对象获取，其次从 roles 获取）
	isAdmin := false
	if userInterface, exists := c.Get("user"); exists {
		if user, ok := userInterface.(*gormdb.User); ok && user.Roles == "admin" {
			isAdmin = true
		}
	} else if roles, exists := c.Get("roles"); exists {
		if rolesStr, ok := roles.(string); ok && rolesStr == "admin" {
			isAdmin = true
		}
	}

	// 非管理员只能查看自己设备的记录
	if !isAdmin {
		// 获取当前用户名
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未授权",
			})
			return
		}

		// 通过用户名获取用户ID
		userRepo := gormdb.NewUserRepository()
		currentUser, err := userRepo.GetUserByName(username.(string))
		if err != nil || currentUser == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "用户不存在",
			})
			return
		}

		// 通过 owner_id 筛选（设备属于当前用户）
		db = db.Where("d.owner_id = ?", currentUser.ID)
	}

	// 筛选条件
	if deviceIDStr != "" {
		deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
		if err == nil {
			db = db.Where("cr.device_id = ?", deviceID)
		}
	}
	if groupIDStr != "" {
		groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
		if err == nil {
			db = db.Where("cr.group_id = ?", groupID)
		}
	}
	// 管理员可以按 user_id 筛选
	if isAdmin && userIDStr != "" {
		userIDFilter, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			db = db.Where("cr.user_id = ?", userIDFilter)
		}
	}

	// 统计总数
	var total int64
	db.Count(&total)

	// 查询列表
	var results []CommRecordWithDetails
	offset := (page - 1) * pageSize
	db.Order("cr.start_time DESC").
		Offset(offset).
		Limit(pageSize).
		Scan(&results)

	// 转换为响应格式
	list := make([]CommRecordResponse, len(results))
	for i, r := range results {
		list[i] = toCommRecordResponse(r)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"list":       list,
			"total":      total,
			"page":       page,
			"page_size":  pageSize,
		},
	})
}

// GetCommRecord 获取单个通信记录
func GetCommRecord(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的记录ID",
		})
		return
	}

	var result CommRecordWithDetails
	err = gormdb.Get().Table("comm_records cr").
		Select(`
			cr.id, cr.device_id, cr.device_ssid, cr.group_id, cr.user_id,
			cr.start_time, cr.end_time, cr.duration_ms, cr.audio_path, cr.audio_size, cr.status,
			d_owner.callsign as owner_call_sign, d_owner.nickname as owner_nick_name,
			g.name as group_name,
			u.callsign as user_call_sign, u.nickname as user_nick_name
		`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Joins("LEFT JOIN users d_owner ON d.owner_id = d_owner.id").
		Joins("LEFT JOIN public_groups g ON cr.group_id = g.id").
		Joins("LEFT JOIN users u ON cr.user_id = u.id").
		Where("cr.id = ?", id).
		First(&result).Error

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "记录不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    toCommRecordResponse(result),
	})
}

// DeleteCommRecord 删除通信记录
func DeleteCommRecord(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的记录ID",
		})
		return
	}

	result := gormdb.Get().Delete(&gormdb.CommRecord{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// GetCommSettings 获取通信设置
func GetCommSettings(c *gin.Context) {
	repo := gormdb.GetSiteConfigRepo()
	settings, err := repo.GetCommSettingsConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取设置失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    settings,
	})
}

// UpdateCommSettingsRequest 更新通信设置请求
type UpdateCommSettingsRequest struct {
	Enabled        bool `json:"enabled"`
	RetentionDays  int  `json:"retention_days"`
	MinDurationMs  int  `json:"min_duration_ms"`
	MaxDurationSec int  `json:"max_duration_sec"`
	BatchUploadSec int  `json:"batch_upload_sec"`
}

// UpdateCommSettings 更新通信设置
func UpdateCommSettings(c *gin.Context) {
	var req UpdateCommSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	// 验证参数
	if req.RetentionDays < 1 || req.RetentionDays > 365 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "数据保留天数必须在 1-365 之间",
		})
		return
	}
	if req.MinDurationMs < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "最小录制阈值不能为负数",
		})
		return
	}
	if req.MaxDurationSec < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "最大录制时长不能为负数",
		})
		return
	}
	if req.BatchUploadSec < 1 || req.BatchUploadSec > 300 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "批量上传间隔必须在 1-300 秒之间",
		})
		return
	}

	// 保存到 site_configs 表
	repo := gormdb.GetSiteConfigRepo()
	settings := gormdb.CommSettingsConfig{
		Enabled:        req.Enabled,
		RetentionDays:  req.RetentionDays,
		MinDurationMs:  req.MinDurationMs,
		MaxDurationSec: req.MaxDurationSec,
		BatchUploadSec: req.BatchUploadSec,
	}

	if err := repo.SetCommSettingsConfig(settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存失败",
		})
		return
	}

	// 重新加载录制器配置
	udphub.ReloadCommSettings(&udphub.CommSettingsConfig{
		Enabled:        settings.Enabled,
		RetentionDays:  settings.RetentionDays,
		MinDurationMs:  settings.MinDurationMs,
		MaxDurationSec: settings.MaxDurationSec,
		BatchUploadSec: settings.BatchUploadSec,
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "保存成功",
		"data":    settings,
	})
}

// GetCommRecorderStats 获取录制器统计信息
func GetCommRecorderStats(c *gin.Context) {
	stats := udphub.GetCommRecorderStats()

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
	})
}
