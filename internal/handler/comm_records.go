package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	minio_local "draarl/pkg/minio"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CommRecordResponse 通信记录响应结构（用于前端显示）
type CommRecordResponse struct {
	ID          uint   `json:"id"`
	DeviceID    uint   `json:"device_id"`
	DeviceName  string `json:"device_name"` // 通过联表查询获取：users.callsign + devices.ssid
	DevModel    int    `json:"dev_model"`   // 设备型号：105=浏览器
	GroupID     *uint  `json:"group_id"`
	GroupName   string `json:"group_name"` // 通过联表查询获取：public_groups.name
	UserID      *uint  `json:"user_id"`
	Username    string `json:"username"` // 登录用户名（用于前端查询头像）
	Nickname    string `json:"nickname"` // 用户昵称（用于显示）
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
	ID              uint      `gorm:"column:id"`
	DeviceID        uint      `gorm:"column:device_id"`
	DeviceOwnerID   int       `gorm:"column:device_owner_id"`
	DeviceSSID      uint8     `gorm:"column:device_ssid"`
	OwnerCallSign   string    `gorm:"column:owner_call_sign"`
	OwnerNickName   string    `gorm:"column:owner_nick_name"`
	GroupID         *uint     `gorm:"column:group_id"`
	GroupName       string    `gorm:"column:group_name"`
	UserID          *uint     `gorm:"column:user_id"`
	UserName        string    `gorm:"column:user_name"`
	UserCallSign    string    `gorm:"column:user_call_sign"`
	UserNickName    string    `gorm:"column:user_nick_name"`
	StartTime       time.Time `gorm:"column:start_time"`
	EndTime         time.Time `gorm:"column:end_time"`
	DurationMs      int       `gorm:"column:duration_ms"`
	AudioPath       string    `gorm:"column:audio_path"`
	AudioSize       int64     `gorm:"column:audio_size"`
	Status          int       `gorm:"column:status"`
	MessageType     uint8     `gorm:"column:message_type"`
	TextContent     string    `gorm:"column:text_content"`
	SenderUsername  string    `gorm:"column:sender_username"`
	SenderCallSign  string    `gorm:"column:sender_callsign"`
	SenderNickname  string    `gorm:"column:sender_nickname"`
	SenderDevModel  int       `gorm:"column:sender_dev_model"`
	CurrentDevModel int       `gorm:"column:current_dev_model"`
}

// canViewOwnCommRecord applies the communication-record ownership boundary.
// UserID is the sender snapshot. Device ownership is only a fallback for
// legacy physical records that predate that snapshot.
func canViewOwnCommRecord(user *gormdb.User, record *CommRecordWithDetails) bool {
	if user == nil || record == nil {
		return false
	}
	if isAdminUser(user) {
		return true
	}
	if record.UserID != nil {
		return int(*record.UserID) == user.ID
	}
	return record.DeviceID > 0 && record.DeviceOwnerID == user.ID
}

// getDevModelName 获取设备型号名称（100-105）
func getDevModelName(devModel int) string {
	switch devModel {
	case 100:
		return "微信小程序"
	case 101:
		return "安卓客户端"
	case 102:
		return "iOS客户端"
	case 103:
		return "Windows客户端"
	case 104:
		return "macOS客户端"
	case 105:
		return "浏览器"
	default:
		return ""
	}
}

// toCommRecordResponse 将联表查询结果转换为响应结构
func toCommRecordResponse(r CommRecordWithDetails) CommRecordResponse {
	audioURL := ""
	msgType := 0 // 默认音频
	textContent := ""

	// 新记录使用显式字段；滚动升级期间兼容旧 text: 前缀。
	if r.MessageType == gormdb.CommMessageTypeText || strings.HasPrefix(r.AudioPath, "text:") {
		msgType = 1
		textContent = r.TextContent
		if textContent == "" && strings.HasPrefix(r.AudioPath, "text:") {
			textContent = strings.TrimPrefix(r.AudioPath, "text:")
		}
	} else if r.AudioPath != "" {
		audioURL = minio_local.GetFileURL(r.AudioPath)
	}
	hasSenderSnapshot := r.SenderUsername != "" || r.SenderCallSign != "" ||
		r.SenderNickname != "" || r.SenderDevModel != 0
	devModel := r.SenderDevModel
	if !hasSenderSnapshot {
		devModel = r.CurrentDevModel
	}

	// 设备名称：呼号 + 设备标识
	deviceName := ""
	senderCallSign := r.SenderCallSign
	if senderCallSign == "" {
		senderCallSign = r.UserCallSign
	}
	if r.DeviceID == 0 {
		// 幽灵设备：呼号-DevModel（100-105），前端根据 dev_model 判断设备类型
		if senderCallSign != "" {
			deviceName = senderCallSign + "-" + strconv.Itoa(devModel)
		}
	} else if r.SenderCallSign != "" {
		deviceName = r.SenderCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		}
	} else if r.OwnerCallSign != "" {
		// 物理设备：呼号-SSID
		deviceName = r.OwnerCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		}
	} else if r.UserCallSign != "" {
		// 兜底显示
		deviceName = r.UserCallSign
		if r.DeviceSSID > 0 {
			deviceName += "-" + strconv.Itoa(int(r.DeviceSSID))
		}
	}

	// 用户名：登录用户名（用于前端查询头像）
	username := r.SenderUsername
	if username == "" {
		username = r.UserName
	}
	// 昵称：用于显示
	nickname := r.SenderNickname
	if nickname == "" {
		nickname = r.UserNickName
	}
	if nickname == "" {
		nickname = r.UserCallSign
	}

	return CommRecordResponse{
		ID:          r.ID,
		DeviceID:    r.DeviceID,
		DeviceName:  deviceName,
		DevModel:    devModel,
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

type commRecordScopeFilter struct {
	CanViewGlobal bool
	ActorUserID   uint
	DeviceID      *uint
	GroupID       *uint
	UserID        *uint
}

func newCommRecordListScope(db *gorm.DB, filter commRecordScopeFilter) *gorm.DB {
	scope := db.Table("comm_records cr").Where("cr.status = ?", 2)
	if !filter.CanViewGlobal {
		scope = scope.
			Joins("LEFT JOIN devices scope_device ON cr.device_id = scope_device.id").
			Where(
				"cr.user_id = ? OR (cr.user_id IS NULL AND cr.device_id > 0 AND scope_device.owner_id = ?)",
				filter.ActorUserID,
				filter.ActorUserID,
			)
	}
	if filter.DeviceID != nil {
		scope = scope.Where("cr.device_id = ?", *filter.DeviceID)
	}
	if filter.GroupID != nil {
		scope = scope.Where("cr.group_id = ?", *filter.GroupID)
	}
	if filter.CanViewGlobal && filter.UserID != nil {
		scope = scope.Where("cr.user_id = ?", *filter.UserID)
	}
	return scope
}

func newCommRecordDetailsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("comm_records cr").
		Select(`
			cr.id, cr.device_id, cr.device_ssid as "DeviceSSID", cr.group_id, cr.user_id,
			cr.start_time, cr.end_time, cr.duration_ms, cr.audio_path, cr.audio_size, cr.status,
			cr.message_type, cr.text_content, cr.sender_username, cr.sender_callsign, cr.sender_nickname, cr.sender_dev_model,
			CASE WHEN cr.device_id = 0 THEN cr.device_ssid ELSE COALESCE(d.dev_model, 0) END as current_dev_model,
			d.owner_id as device_owner_id,
			d_owner.callsign as owner_call_sign, d_owner.nickname as owner_nick_name,
			g.name as group_name,
			u.name as user_name, u.callsign as user_call_sign, u.nickname as user_nick_name
		`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Joins("LEFT JOIN users d_owner ON d.owner_id = d_owner.id").
		Joins("LEFT JOIN public_groups g ON cr.group_id = g.id").
		Joins("LEFT JOIN users u ON cr.user_id = u.id")
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
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	// 获取筛选参数
	deviceIDStr := c.Query("device_id")
	groupIDStr := c.Query("group_id")
	userIDStr := c.Query("user_id")
	// 获取管理员模式参数：只有管理员在后台页面时才为 true
	adminMode := c.Query("admin_mode") == "true"
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}
	canViewGlobal := isAdminUser(currentUser) && adminMode

	var requestedGroupID *uint
	if groupIDStr != "" {
		parsedGroupID, err := strconv.ParseUint(groupIDStr, 10, 32)
		if err != nil || parsedGroupID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    http.StatusBadRequest,
				"message": "无效的群组ID",
			})
			return
		}

		group, err := gormdb.NewGroupRepository().GetGroupByID(int(parsedGroupID))
		if err != nil {
			log.Printf("[COMM_RECORDS] 查询群组失败 group=%d err=%v", parsedGroupID, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    http.StatusInternalServerError,
				"message": "查询群组失败",
			})
			return
		}
		if group == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "群组不存在",
			})
			return
		}
		value := uint(parsedGroupID)
		requestedGroupID = &value
	}

	filter := commRecordScopeFilter{CanViewGlobal: canViewGlobal, ActorUserID: uint(currentUser.ID), GroupID: requestedGroupID}
	if deviceIDStr != "" {
		deviceID, err := strconv.ParseUint(deviceIDStr, 10, 32)
		if err == nil {
			value := uint(deviceID)
			filter.DeviceID = &value
		}
	}
	if canViewGlobal && userIDStr != "" {
		userIDFilter, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			value := uint(userIDFilter)
			filter.UserID = &value
		}
	}

	var total int64
	if err := newCommRecordListScope(gormdb.Get(), filter).Count(&total).Error; err != nil {
		log.Printf("[COMM_RECORDS] 统计通信记录总数失败 err=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询通信记录失败",
		})
		return
	}

	offset := (page - 1) * pageSize
	var recordIDs []uint
	if err := newCommRecordListScope(gormdb.Get(), filter).
		Order("cr.start_time DESC").Order("cr.id DESC").
		Offset(offset).
		Limit(pageSize).
		Pluck("cr.id", &recordIDs).Error; err != nil {
		log.Printf("[COMM_RECORDS] 查询通信记录列表失败 err=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询通信记录失败",
		})
		return
	}
	var results []CommRecordWithDetails
	if len(recordIDs) > 0 {
		if err := newCommRecordDetailsQuery(gormdb.Get()).
			Where("cr.id IN ?", recordIDs).
			Order("cr.start_time DESC").Order("cr.id DESC").
			Scan(&results).Error; err != nil {
			log.Printf("[COMM_RECORDS] 查询通信记录详情失败 err=%v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "查询通信记录失败",
			})
			return
		}
	}

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
	currentUser, ok := requireCurrentUser(c)
	if !ok {
		return
	}

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
			cr.message_type, cr.text_content, cr.sender_username, cr.sender_callsign, cr.sender_nickname, cr.sender_dev_model,
			CASE WHEN cr.device_id = 0 THEN cr.device_ssid ELSE COALESCE(d.dev_model, 0) END as current_dev_model,
			d.owner_id as device_owner_id,
			d_owner.callsign as owner_call_sign, d_owner.nickname as owner_nick_name,
			g.name as group_name,
			u.name as user_name, u.callsign as user_call_sign, u.nickname as user_nick_name
		`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Joins("LEFT JOIN users d_owner ON d.owner_id = d_owner.id").
		Joins("LEFT JOIN public_groups g ON cr.group_id = g.id").
		Joins("LEFT JOIN users u ON cr.user_id = u.id").
		Where("cr.id = ?", id).
		First(&result).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "记录不存在",
		})
		return
	}
	if err != nil {
		log.Printf("[COMM_RECORDS] 查询通信记录详情失败 id=%d err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": "查询通信记录失败",
		})
		return
	}
	if !canViewOwnCommRecord(currentUser, &result) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    http.StatusForbidden,
			"message": "无权访问该通信记录",
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
	thirtyDaysAgoTime := time.Now().AddDate(0, 0, -30)
	thirtyDaysAgo := thirtyDaysAgoTime.Format("2006-01-02")

	var trends []DailyCommStats

	// 查询用户设备近30天的通信趋势
	// 使用 DATE_FORMAT (MySQL) 确保日期格式为字符串 'YYYY-MM-DD'
	// 同时包含幽灵设备（device_id=0）的记录，通过 user_id 直接关联
	err = gormdb.Get().Table("comm_records cr").
		Select(`DATE_FORMAT(cr.start_time, '%Y-%m-%d') as date, COUNT(cr.id) as count, COALESCE(SUM(cr.duration_ms), 0) as duration`).
		Joins("LEFT JOIN devices d ON cr.device_id = d.id").
		Where("d.owner_id = ? OR cr.user_id = ?", user.ID, user.ID).
		Where("cr.start_time >= ?", thirtyDaysAgoTime).
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
	thirtyDaysAgoTime := time.Now().AddDate(0, 0, -30)
	thirtyDaysAgo := thirtyDaysAgoTime.Format("2006-01-02")

	var trends []DailyCommStats

	// 使用 DATE_FORMAT (MySQL) 确保日期格式为字符串 'YYYY-MM-DD'
	err := gormdb.Get().Table("comm_records").
		Select(`DATE_FORMAT(start_time, '%Y-%m-%d') as date, COUNT(id) as count, COALESCE(SUM(duration_ms), 0) as duration`).
		Where("start_time >= ?", thirtyDaysAgoTime).
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
