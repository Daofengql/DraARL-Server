package handler

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"nrllink/internal/aprs"
	"nrllink/internal/config"
	"nrllink/internal/gormdb"
	oplog "nrllink/internal/log"
	"nrllink/pkg/cache"

	"github.com/gin-gonic/gin"
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

// GetAllConfigs 获取所有配置（管理员）
func (h *SiteConfigHandler) GetAllConfigs(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	_ = user

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var configs []gormdb.SiteConfig
	var err error

	if configCache != nil {
		configs, err = configCache.GetAllConfigs(ctx)
	} else {
		configs, err = h.repo.GetAll()
	}
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

	// 任何已登录用户都可以读取配置
	category := c.Param("category")

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var configs []gormdb.SiteConfig
	var err error

	if configCache != nil {
		configs, err = configCache.GetConfigsByCategory(ctx, category)
	} else {
		configs, err = h.repo.GetByCategory(category)
	}
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
	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var icpConfig *gormdb.ICPConfig
	var systemConfig *gormdb.SystemInfoConfig
	var err error

	// 获取公开配置：ICP、SystemInfo（使用缓存）
	if configCache != nil {
		icpConfig, err = configCache.GetICPConfig(ctx)
	} else {
		icpConfig, err = h.repo.GetICPConfig()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	if configCache != nil {
		systemConfig, err = configCache.GetSystemInfoConfig(ctx)
	} else {
		systemConfig, err = h.repo.GetSystemInfoConfig()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取配置失败",
		})
		return
	}

	// 获取 SSO 显示名称
	ssoName := config.Get().Keycloak.Name
	if ssoName == "" {
		ssoName = "SSO" // 默认值
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data: gin.H{
			"icp":         icpConfig,
			"systemInfo":  systemConfig,
			"sso_enabled": config.Get().Keycloak.Enabled,
			"sso_name":    ssoName,
		},
	})
}

// UpdateConfig 更新单个配置（管理员）
func (h *SiteConfigHandler) UpdateConfig(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)

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

	// 使配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateConfig(c.Request.Context(), req.Key)
		if req.Category != "" {
			_ = configCache.InvalidateCategory(c.Request.Context(), req.Category)
		}
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新站点配置: %s = %s (分类: %s)", req.Key, req.Value, req.Category),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// UpdateICPConfig 更新ICP配置（管理员）
func (h *SiteConfigHandler) UpdateICPConfig(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)

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

	// 使ICP配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateICPConfig(c.Request.Context())
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新ICP配置: %s", req.ICP),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

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

	// 使系统信息配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateSystemInfoConfig(c.Request.Context())
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新系统信息配置: 平台名称=%s, 语言=%s", req.Name, req.Language),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

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

	var req gormdb.APRSConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 校验经纬度范围
	if req.Longitude < -180 || req.Longitude > 180 {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "经度必须在 -180 到 180 之间",
		})
		return
	}
	if req.Latitude < -90 || req.Latitude > 90 {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "纬度必须在 -90 到 90 之间",
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

	// 使APRS配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateAPRSConfig(c.Request.Context())
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

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新APRS配置: 服务器=%s:%s", req.APRSServerHost, req.APRSServerPort),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

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

	// 使OpenAI配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateOpenAIConfig(c.Request.Context())
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新OpenAI配置: BaseURL=%s, Engine=%s", req.BaseURL, req.Engine),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

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

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var config *gormdb.APRSConfig
	var err error

	if configCache != nil {
		config, err = configCache.GetAPRSConfig(ctx)
	} else {
		config, err = h.repo.GetAPRSConfig()
	}
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

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var config *gormdb.OpenAIConfig
	var err error

	if configCache != nil {
		config, err = configCache.GetOpenAIConfig(ctx)
	} else {
		config, err = h.repo.GetOpenAIConfig()
	}
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

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var config *gormdb.SystemInfoConfig
	var err error

	if configCache != nil {
		config, err = configCache.GetSystemInfoConfig(ctx)
	} else {
		config, err = h.repo.GetSystemInfoConfig()
	}
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

	_ = user // 由已通过 RequireAdmin 中间件验证权限

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

// DeleteLogo 删除站点Logo（管理员）
func (h *SiteConfigHandler) DeleteLogo(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)

	// 清空 Logo URL 配置（将值设置为空字符串）
	if err := h.repo.Set("system.logo_url", "", "system", "站点Logo URL"); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "删除Logo配置失败",
		})
		return
	}

	// 使系统信息配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateSystemInfoConfig(c.Request.Context())
	}

	// 记录审计日志
	oplog.AddLog(
		"删除站点Logo",
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "删除成功",
	})
}

// GetSMTPConfig 获取SMTP配置（管理员）
func (h *SiteConfigHandler) GetSMTPConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	config, err := h.repo.GetSMTPConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取SMTP配置失败",
		})
		return
	}

	// 密码脱敏处理
	maskedConfig := *config
	if len(maskedConfig.Password) > 4 {
		maskedConfig.Password = maskedConfig.Password[:2] + "****" + maskedConfig.Password[len(maskedConfig.Password)-2:]
	} else if len(maskedConfig.Password) > 0 {
		maskedConfig.Password = "****"
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    maskedConfig,
	})
}

// UpdateSMTPConfig 更新SMTP配置（管理员）
func (h *SiteConfigHandler) UpdateSMTPConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*gormdb.User)

	var req gormdb.SMTPConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 验证端口范围
	if req.Port < 1 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "端口必须在1-65535之间",
		})
		return
	}

	// 如果密码是脱敏格式（包含*），则不更新密码
	existingConfig, _ := h.repo.GetSMTPConfig()
	if existingConfig != nil && len(req.Password) > 0 && containsStars(req.Password) {
		req.Password = existingConfig.Password
	}

	if err := h.repo.SetSMTPConfig(req); err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新SMTP配置失败",
		})
		return
	}

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("更新SMTP配置: 服务器=%s:%d, 发件人=%s", req.Host, req.Port, req.SenderEmail),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// containsStars 检查字符串是否包含星号（用于判断是否为脱敏密码）
func containsStars(s string) bool {
	for _, c := range s {
		if c == '*' {
			return true
		}
	}
	return false
}
