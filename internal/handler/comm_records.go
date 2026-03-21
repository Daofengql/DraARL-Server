package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gormdb "nrllink/internal/gormdb"
	oplog "nrllink/internal/log"
	"nrllink/internal/udphub"
	"nrllink/pkg/cache"
	minio_local "nrllink/pkg/minio"

	"github.com/gin-gonic/gin"
)

// CommRecordResponse 通信记录响应结构（用于前端显示）
type CommRecordResponse struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	DeviceName  string `json:"device_name"` // 通过联表查询获取：users.callsign + devices.ssid
	DevModel    int    `json:"dev_model"`   // 设备型号：105=浏览器
	GroupID     *uint  `json:"group_id"`
	GroupName   string `json:"group_name"`  // 通过联表查询获取：public_groups.name
	UserID      *uint  `json:"user_id"`
	Username    string `json:"username"`    // 登录用户名（用于前端查询头像）
	Nickname    string `json:"nickname"`    // 用户昵称（用于显示）
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	DurationMs  int    `json:"duration_ms"`
	AudioPath   string `json:"audio_path,omitempty"`
	AudioURL    string `json:"audio_url,omitempty"`
	AudioSize   int64  `json:"audio_size"`
	Status      int    `json:"status"`
	MsgType     int    `json:"msg_type"`     // 消息类型：0=音频, 1=文本
	TextContent string `json:"text_content"` // 文本消息内容（仅文本消息有值)
}

// CommRecordWithDetails 联表查询结果
type CommRecordWithDetails struct {
	ID            uint      `gorm:"column:id"`
	DeviceID      uint      `gorm:"column:device_id"`
	DeviceSSID    uint8     `gorm:"column:device_ssid"`
	DevModel      int       `gorm:"column:dev_model"`
	OwnerCallSign string    `gorm:"column:owner_call_sign"`
	OwnerNickName string    `gorm:"column:owner_nick_name"`
	GroupID       *uint     `gorm:"column:group_id"`
	GroupName     string    `gorm:"column:group_name"`
	UserID        *uint     `gorm:"column:user_id"`
	UserName      string    `gorm:"column:user_name"`
	UserCallSign  string    `gorm:"column:user_call_sign"`
	UserNickName  string    `gorm:"column:user_nick_name"`
	StartTime     time.Time `gorm:"column:start_time"`
	EndTime       time.Time `gorm:"column:end_time"`
	DurationMs    int       `gorm:"column:duration_ms"`
	AudioPath     string    `gorm:"column:audio_path"`
	AudioSize     int64     `gorm:"column:audio_size"`
	Status        int       `gorm:"column:status"`
}

// toCommRecordResponse 将联表查询结果转换为响应结构
func toCommRecordResponse(r CommRecordWithDetails) CommRecordResponse {
	audioURL := ""
	msgType := 0 // 默认音频
	textContent := ""

	// 判断消息类型：text: 前缀表示文本消息
	if strings.HasPrefix(r.AudioPath, "text:") {
		msgType = 1
		textContent = strings.TrimPrefix(r.AudioPath, "text:")
	} else if r.AudioPath != "" {
		audioURL = minio_local.GetFileURL(r.AudioPath)
	}

	// 设备名称：呼号-SSID
	deviceName := ""
	if r.OwnerCallSign != "" {
		// 物理设备
		deviceName = r.OwnerCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		}
	} else if r.UserCallSign != "" {
		// 幽灵设备兜底显示：使用直接关联的用户呼号
		deviceName = r.UserCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		} else {
			deviceName += "-Web"
		}
	}

	// 用户名：登录用户名（用于前端查询头像）
	username := r.UserName
	// 昵称：用于显示
	nickname := r.UserNickName
	if nickname == "" {
		nickname = r.UserCallSign
	}

	return CommRecordResponse{
		ID:          r.ID,
		DeviceID:    r.DeviceID,
		DeviceName:  deviceName,
		DevModel:    r.DevModel,
		GroupID:     r.GroupID,
		GroupName:   r.GroupName,
		UserID:      r.UserID,
		Username:    username,
		Nickname:    nickname,
		StartTime:   r.StartTime.Format("2006-01-02 15:04:05"),
		EndTime:     r.EndTime.Format("2006-01-02 15:04:05"),
		DurationMs:  r.DurationMs,
		AudioURL:    audioURL,
		AudioSize:   r.AudioSize,
		Status:      r.Status,
		MsgType:     msgType,
		TextContent: textContent,
	}
}

// GetCommRecords 获取通信记录列表（使用联表查询）
// 权限规则：
// - 管理员 + admin_mode=true：可查看所有记录（管理员后台）
// - 管理员 + admin_mode=false：只能查看自己的记录（管理员前台）
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
	// 获取管理员模式参数：只有管理员在后台页面时才为 true
	adminMode := c.Query("admin_mode") == "true"

	db := gormdb.Get().Table("comm_records cr").
		Select(`
			cr.id, cr.device_id, cr.device_ssid as "DeviceSSID", cr.group_id, cr.user_id,
			cr.start_time, cr.end_time, cr.duration_ms, cr.audio_path, cr.audio_size, cr.status,
			CASE WHEN cr.device_id = 0 THEN 105 ELSE d.dev_model END as dev_model,
			d_owner.callsign as owner_call_sign, d_owner.nickname as owner_nick_name,
			g.name as group_name,
			u.name as user_name, u.callsign as user_call_sign, u.nickname as user_nick_name
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
		// roles 是 []string 类型，需要正确处理
		if rolesSlice, ok := roles.([]string); ok {
			for _, role := range rolesSlice {
				if role == "admin" {
					isAdmin = true
					break
				}
			}
		}
	}

	// 判断是否可以查看全局记录：必须是管理员且在后台模式
	canViewGlobal := isAdmin && adminMode

	// 非全局模式只能查看自己设备的记录
	if !canViewGlobal {
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

		// 通过 owner_id 筛选（物理设备属于当前用户），或者通过 user_id 筛选（Web端幽灵设备直接属于该用户）
		db = db.Where("d.owner_id = ? OR cr.user_id = ?", currentUser.ID, currentUser.ID)
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
			// 【互联组支持】使用子查询获取所有相关群组的消息
			// 逻辑：
			// 1. 当前群组本身
			// 2. 通过 group_links 找到当前群组所属的互联组 (link_group_id)
			// 3. 找到这些互联组关联的所有目标群组 (target_group_id)
			// 使用 GORM 的子查询构建方式，避免参数绑定问题
			subQuery := gormdb.Get().Table("group_links gl1").
				Select("gl2.target_group_id").
				Joins("INNER JOIN group_links gl2 ON gl1.link_group_id = gl2.link_group_id").
				Where("gl1.target_group_id = ?", groupID)
			db = db.Where("cr.group_id = ? OR cr.group_id IN (?)", groupID, subQuery)
		}
	}
	// 全局模式下可以按 user_id 筛选
	if canViewGlobal && userIDStr != "" {
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
			"list":      list,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
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
			CASE WHEN cr.device_id = 0 THEN 105 ELSE d.dev_model END as dev_model,
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

	// 获取当前用户
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))

	result := gormdb.Get().Delete(&gormdb.CommRecord{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除失败",
		})
		return
	}

	// 记录审计日志
	if currentUser != nil {
		oplog.AddLog(
			fmt.Sprintf("删除通信记录: ID %d", id),
			"comm_record_delete",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
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

	// 使通信设置配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateCategory(c.Request.Context(), "comm")
	}

	// 重新加载录制器配置
	udphub.ReloadCommSettings(&udphub.CommSettingsConfig{
		Enabled:        settings.Enabled,
		RetentionDays:  settings.RetentionDays,
		MinDurationMs:  settings.MinDurationMs,
		MaxDurationSec: settings.MaxDurationSec,
		BatchUploadSec: settings.BatchUploadSec,
	})

	// 记录审计日志
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser != nil {
		oplog.AddLog(
			fmt.Sprintf("更新通信录制配置: 启用=%v, 保留天数=%d, 最小时长=%dms, 最大时长=%ds, 批量间隔=%ds",
				settings.Enabled, settings.RetentionDays, settings.MinDurationMs,
				settings.MaxDurationSec, settings.BatchUploadSec),
			"comm_settings_update",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	}

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

// DailyCommStats 每日通信统计
type DailyCommStats struct {
	Date     string `json:"date" gorm:"column:date"`
	Count    int64  `json:"count" gorm:"column:count"`
	Duration int64  `json:"duration" gorm:"column:duration"` // 总时长（毫秒）
}

// UserCommStats 用户通信统计
type UserCommStats struct {
	TotalCount    int64 `json:"total_count"`
	TotalSize     int64 `json:"total_size"`     // 文件总大小（字节）
	TotalDuration int64 `json:"total_duration"` // 总时长（毫秒）
}

// SystemCommStats 系统通信统计
type SystemCommStats struct {
	TotalCount    int64 `json:"total_count"`
	TotalSize     int64 `json:"total_size"`     // 文件总大小（字节）
	TotalDuration int64 `json:"total_duration"` // 总时长（毫秒）
}

// GetUserCommStats 获取当前用户的通信统计
func GetUserCommStats(c *gin.Context) {
	// 获取当前用户名
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	var stats UserCommStats

	// 查询用户设备的通信统计
	// 通过 devices 表关联查询，获取用户所有设备的通信记录
	// 同时包含幽灵设备（device_id=0）的记录，通过 user_id 直接关联
	err = gormdb.Get().Table("comm_records cr").
		Select(`
			COALESCE(COUNT(cr.id), 0) as total_count,
			COALESCE(SUM(cr.audio_size), 0) as total_size,
			COALESCE(SUM(cr.duration_ms), 0) as total_duration
		`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Where("d.owner_id = ? OR cr.user_id = ?", user.ID, user.ID).
		Scan(&stats).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取统计失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
	})
}

// GetUserCommTrend 获取当前用户近30天通信趋势
func GetUserCommTrend(c *gin.Context) {
	// 获取当前用户名
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	// 计算30天前的日期
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	var trends []DailyCommStats

	// 查询用户设备近30天的通信趋势
	// 使用 DATE_FORMAT (MySQL) 确保日期格式为字符串 'YYYY-MM-DD'
	// 同时包含幽灵设备（device_id=0）的记录，通过 user_id 直接关联
	err = gormdb.Get().Table("comm_records cr").
		Select(`DATE_FORMAT(cr.start_time, '%Y-%m-%d') as date, COUNT(cr.id) as count, COALESCE(SUM(cr.duration_ms), 0) as duration`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Where("d.owner_id = ? OR cr.user_id = ?", user.ID, user.ID).
		Where("DATE_FORMAT(cr.start_time, '%Y-%m-%d') >= ?", thirtyDaysAgo).
		Group("DATE_FORMAT(cr.start_time, '%Y-%m-%d')").
		Order("date ASC").
		Scan(&trends).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取趋势失败",
		})
		return
	}

	// 填充缺失的日期
	trends = fillMissingDates(trends, thirtyDaysAgo)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    trends,
	})
}

// GetSystemCommStats 获取系统通信统计（管理员）
func GetSystemCommStats(c *gin.Context) {
	var stats SystemCommStats

	err := gormdb.Get().Table("comm_records").
		Select(`
			COALESCE(COUNT(id), 0) as total_count,
			COALESCE(SUM(audio_size), 0) as total_size,
			COALESCE(SUM(duration_ms), 0) as total_duration
		`).
		Scan(&stats).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取统计失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
	})
}

// GetSystemCommTrend 获取系统近30天通信趋势（管理员）
func GetSystemCommTrend(c *gin.Context) {
	// 计算30天前的日期
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	var trends []DailyCommStats

	// 使用 DATE_FORMAT (MySQL) 确保日期格式为字符串 'YYYY-MM-DD'
	err := gormdb.Get().Table("comm_records").
		Select(`DATE_FORMAT(start_time, '%Y-%m-%d') as date, COUNT(id) as count, COALESCE(SUM(duration_ms), 0) as duration`).
		Where("DATE_FORMAT(start_time, '%Y-%m-%d') >= ?", thirtyDaysAgo).
		Group("DATE_FORMAT(start_time, '%Y-%m-%d')").
		Order("date ASC").
		Scan(&trends).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取趋势失败",
		})
		return
	}

	// 填充缺失的日期
	trends = fillMissingDates(trends, thirtyDaysAgo)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    trends,
	})
}

// fillMissingDates 填充缺失的日期
func fillMissingDates(trends []DailyCommStats, startDate string) []DailyCommStats {
	// 创建日期映射
	trendMap := make(map[string]int64)
	durationMap := make(map[string]int64)
	for _, t := range trends {
		trendMap[t.Date] = t.Count
		durationMap[t.Date] = t.Duration
	}

	// 解析开始日期
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return trends
	}

	// 生成完整的日期列表
	var result []DailyCommStats
	now := time.Now()
	for d := start; d.Before(now) || d.Format("2006-01-02") == now.Format("2006-01-02"); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		result = append(result, DailyCommStats{
			Date:     dateStr,
			Count:    trendMap[dateStr],
			Duration: durationMap[dateStr],
		})
	}

	return result
}
