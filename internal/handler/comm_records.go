package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"nrllink/internal/gormdb"
	"nrllink/internal/udphub"
	minio_local "nrllink/pkg/minio"
)

// CommRecordResponse 通信记录响应结构
type CommRecordResponse struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	DeviceName  string `json:"device_name"`
	GroupID     *uint  `json:"group_id"`
	GroupName   string `json:"group_name"`
	UserID      *uint  `json:"user_id"`
	Username    string `json:"username"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	DurationMs  int    `json:"duration_ms"`
	AudioPath   string `json:"audio_path"`
	AudioURL    string `json:"audio_url"`
	AudioSize   int64  `json:"audio_size"`
	Status      int    `json:"status"`
}

// toResponse 将 CommRecord 转换为响应结构
func toCommRecordResponse(r gormdb.CommRecord) CommRecordResponse {
	audioURL := ""
	if r.AudioPath != "" {
		audioURL = minio_local.GetFileURL(r.AudioPath)
	}
	return CommRecordResponse{
		ID:         r.ID,
		DeviceID:   r.DeviceID,
		DeviceName: r.DeviceName,
		GroupID:    r.GroupID,
		GroupName:  r.GroupName,
		UserID:     r.UserID,
		Username:   r.Username,
		StartTime:  r.StartTime.Format("2006-01-02 15:04:05"),
		EndTime:    r.EndTime.Format("2006-01-02 15:04:05"),
		DurationMs: r.DurationMs,
		AudioPath:  r.AudioPath,
		AudioURL:   audioURL,
		AudioSize:  r.AudioSize,
		Status:     r.Status,
	}
}

// GetCommRecords 获取通信记录列表
// 权限规则：
// - 后台路由（/admin/...）：管理员可查看所有记录
// - 前台路由：所有用户只能查看自己设备的记录
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

	db := gormdb.Get().Model(&gormdb.CommRecord{})

	// 只返回已完成的记录
	db = db.Where("status = ?", 2)

	// 判断是否是后台请求（根据路由前缀）
	isAdminRoute := false
	if path := c.Request.URL.Path; len(path) >= 7 && path[:7] == "/admin/" {
		isAdminRoute = true
	}

	// 前台只能查看自己设备的记录
	if !isAdminRoute {
		// 获取当前用户ID
		userID, _ := c.Get("userID")

		// 查询该用户拥有的设备ID列表
		var userDeviceIDs []uint
		gormdb.Get().Model(&gormdb.Device{}).Where("user_id = ?", userID).Pluck("id", &userDeviceIDs)
		if len(userDeviceIDs) == 0 {
			// 用户没有设备，返回空列表
			c.JSON(http.StatusOK, gin.H{
				"code":    200,
				"message": "成功",
				"data": gin.H{
					"list":       []CommRecordResponse{},
					"total":      0,
					"page":       page,
					"page_size":  pageSize,
				},
			})
			return
		}
		db = db.Where("device_id IN ?", userDeviceIDs)
	}

	// 筛选条件
	if deviceIDStr != "" {
		deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
		if err == nil {
			db = db.Where("device_id = ?", deviceID)
		}
	}
	if groupIDStr != "" {
		groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
		if err == nil {
			db = db.Where("group_id = ?", groupID)
		}
	}
	// 后台可以按 user_id 筛选
	if isAdminRoute && userIDStr != "" {
		userIDFilter, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			db = db.Where("user_id = ?", userIDFilter)
		}
	}

	// 统计总数
	var total int64
	db.Count(&total)

	// 查询列表
	var records []gormdb.CommRecord
	offset := (page - 1) * pageSize
	db.Order("start_time DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&records)

	// 转换为响应格式
	list := make([]CommRecordResponse, len(records))
	for i, r := range records {
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

	var record gormdb.CommRecord
	result := gormdb.Get().First(&record, id)
	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "记录不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    toCommRecordResponse(record),
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
	var settings gormdb.CommSettings
	result := gormdb.Get().FirstOrCreate(&settings, 1)
	if result.Error != nil {
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

// UpdateCommSettings 更新通信设置
type UpdateCommSettingsRequest struct {
	Enabled        bool `json:"enabled"`
	RetentionDays  int  `json:"retention_days"`
	MinDurationMs  int  `json:"min_duration_ms"`
	MaxDurationSec int  `json:"max_duration_sec"`
	BatchUploadSec int  `json:"batch_upload_sec"`
}

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

	// 更新数据库
	settings := gormdb.CommSettings{
		ID:              1,
		Enabled:         req.Enabled,
		RetentionDays:   req.RetentionDays,
		MinDurationMs:   req.MinDurationMs,
		MaxDurationSec:  req.MaxDurationSec,
		BatchUploadSec:  req.BatchUploadSec,
	}

	result := gormdb.Get().Save(&settings)
	if result.Error != nil {
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
