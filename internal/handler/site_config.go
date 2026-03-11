package handler

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"nrllink/internal/aprs"
	"nrllink/internal/gormdb"
)

// SiteConfigHandler 站点配置处理器
type SiteConfigHandler struct {
	repo *gormdb.SiteConfigRepository
}

// NewSiteConfigHandler 创建配置处理器
func NewSiteConfigHandler() *SiteConfigHandler {
	return &SiteConfigHandler{
		repo: gormdb.GetSiteConfigRepo(),
	}
}

// isSuperAdmin 检查是否是超级管理员 (ID=1)
func isSuperAdmin(user *gormdb.User) bool {
	return user.ID == 1
}

// GetAllConfigs 获取所有配置（仅超级管理员）
func (h *SiteConfigHandler) GetAllConfigs(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	configs, err := h.repo.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    configs,
	})
}

// GetConfigsByCategory 根据分类获取配置（已登录用户可读取）
func (h *SiteConfigHandler) GetConfigsByCategory(c *gin.Context) {
	_, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	// 任何已登录用户都可以读取��置
	category := c.Param("category")
	configs, err := h.repo.GetByCategory(category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    configs,
	})
}

// GetPublicConfigs 获取公开配置（不需要登录）
func (h *SiteConfigHandler) GetPublicConfigs(c *gin.Context) {
	// 获取公开配置：ICP、SystemInfo
	icpConfig, err := h.repo.GetICPConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	systemConfig, err := h.repo.GetSystemInfoConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data: gin.H{
			"icp":        icpConfig,
			"systemInfo": systemConfig,
		},
	})
}

// UpdateConfig 更新单个配置（管理员）
func (h *SiteConfigHandler) UpdateConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	var req struct {
		Key      string `json:"key" binding:"required"`
		Value    string `json:"value" binding:"required"`
		Category string `json:"category"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	if err := h.repo.Set(req.Key, req.Value, req.Category, ""); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// UpdateICPConfig 更新ICP配置（管理员）
func (h *SiteConfigHandler) UpdateICPConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	var req struct {
		ICP string `json:"icp"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	if err := h.repo.SetICPConfig(req.ICP); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新ICP配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// UpdateSystemInfoConfig 更新系统信息配置（管理员）
func (h *SiteConfigHandler) UpdateSystemInfoConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	var req gormdb.SystemInfoConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	if err := h.repo.SetSystemInfoConfig(req); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新系统信息配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// UpdateAPRSConfig 更新APRS配置（管理员）
func (h *SiteConfigHandler) UpdateAPRSConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	var req gormdb.APRSConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	if err := h.repo.SetAPRSConfig(req); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新APRS配置失败",
		})
		return
	}

	// 重启 APRS 服务
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("APRS重启时发生panic: %v", r)
			}
		}()
		aprs.RestartAPRSService()
	}()

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功，APRS服务正在重启",
	})
}

// UpdateOpenAIConfig 更新OpenAI配置（管理员）
func (h *SiteConfigHandler) UpdateOpenAIConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	var req gormdb.OpenAIConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	if err := h.repo.SetOpenAIConfig(req); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新OpenAI配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// GetAPRSConfig 获取APRS配置（管理员）
func (h *SiteConfigHandler) GetAPRSConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	config, err := h.repo.GetAPRSConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取APRS配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    config,
	})
}

// GetOpenAIConfig 获取OpenAI配置（管理员）
func (h *SiteConfigHandler) GetOpenAIConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	config, err := h.repo.GetOpenAIConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取OpenAI配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    config,
	})
}

// GetSystemInfoConfig 获取系统信息配置（管理员）
func (h *SiteConfigHandler) GetSystemInfoConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	config, err := h.repo.GetSystemInfoConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取系统信息配置失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    config,
	})
}

// GetAPRSLogs 获取APRS日志（管理员）
func (h *SiteConfigHandler) GetAPRSLogs(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)
	if !isSuperAdmin(userModel) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "仅超级管理员可访问",
		})
		return
	}

	logs := aprs.GetAPRSLogs()

	// 只返回最近10分钟内的日志
	cutoffTime := time.Now().Add(-10 * time.Minute)
	filteredLogs := make([]aprs.APRSLogEntry, 0)

	for _, log := range logs {
		// 解析时间戳
		logTime, err := time.Parse("2006-01-02 15:04:05", log.Timestamp)
		if err != nil {
			continue
		}

		// 只保留最近10分钟内的日志
		if logTime.After(cutoffTime) {
			filteredLogs = append(filteredLogs, log)
		}
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    filteredLogs,
	})
}
