package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"draarl/internal/accesspoint"
	"draarl/internal/aprs"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"
	"draarl/pkg/storage"

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

// isSensitiveConfigKey 判断站点配置键是否属于密钥类（password/secret/key/token）。
// 用于列表/分类读取与审计日志的脱敏，避免 SMTP 授权码等明文泄露。
func isSensitiveConfigKey(key string) bool {
	return gormdb.IsSensitiveSiteConfigKey(key)
}

// maskConfigValue 对密钥类配置值脱敏：保留首尾 2 位，中间以 **** 代替；
// 长度不足 4 位时整体替换为 ****。空值原样返回。
func maskConfigValue(value string) string {
	if value == "" {
		return value
	}
	if len(value) > 4 {
		return value[:2] + "****" + value[len(value)-2:]
	}
	return "****"
}

// maskSensitiveConfigs 返回脱敏后的配置副本；非密钥键保持原值。
func maskSensitiveConfigs(configs []gormdb.SiteConfig) []gormdb.SiteConfig {
	out := make([]gormdb.SiteConfig, len(configs))
	for i, cfg := range configs {
		out[i] = cfg
		if isSensitiveConfigKey(cfg.Key) {
			out[i].Value = maskConfigValue(cfg.Value)
		}
	}
	return out
}

// GetAllConfigs 获取所有配置（管理员）
func (h *SiteConfigHandler) GetAllConfigs(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
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
		configs, err = h.repo.WithContext(c.Request.Context()).GetAll()
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    maskSensitiveConfigs(configs),
	})
}

// GetConfigsByCategory 根据分类获取配置（【S3 安全修复】仅管理员，且密钥类值脱敏）
func (h *SiteConfigHandler) GetConfigsByCategory(c *gin.Context) {
	_, exists := c.Get("username")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	// 路由已通过 RequireAdmin 中间件；此处仅按分类读取并统一脱敏
	category := c.Param("category")

	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var configs []gormdb.SiteConfig
	var err error

	if configCache != nil {
		configs, err = configCache.GetConfigsByCategory(ctx, category)
	} else {
		configs, err = h.repo.WithContext(c.Request.Context()).GetByCategory(category)
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    maskSensitiveConfigs(configs),
	})
}

func resolveSystemInfoURLs(cfg *gormdb.SystemInfoConfig) *gormdb.SystemInfoConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	out.LogoURL = storage.ResolveAssetURL(cfg.LogoURL)
	out.FaviconURL = storage.ResolveAssetURL(cfg.FaviconURL)
	return &out
}

func (h *SiteConfigHandler) GetPublicConfigs(c *gin.Context) {
	ctx := c.Request.Context()
	configCache := cache.GetConfigCache()

	var icpConfig *gormdb.ICPConfig
	var systemConfig *gormdb.SystemInfoConfig
	var registrationConfig *gormdb.RegistrationConfig
	var err error

	// 获取公开配置：ICP、SystemInfo（使用缓存）
	if configCache != nil {
		icpConfig, err = configCache.GetICPConfig(ctx)
	} else {
		icpConfig, err = h.repo.WithContext(c.Request.Context()).GetICPConfig()
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
		return
	}

	if configCache != nil {
		systemConfig, err = configCache.GetSystemInfoConfig(ctx)
	} else {
		systemConfig, err = h.repo.WithContext(c.Request.Context()).GetSystemInfoConfig()
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
		return
	}

	registrationConfig, err = h.repo.WithContext(c.Request.Context()).GetRegistrationConfig()
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
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
			"icp":          icpConfig,
			"systemInfo":   resolveSystemInfoURLs(systemConfig),
			"registration": registrationConfig,
			"sso_enabled":  config.Get().Keycloak.Enabled,
			"sso_name":     ssoName,
		},
	})
}

// UpdateConfig 更新单个配置（管理员）
func (h *SiteConfigHandler) UpdateConfig(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req struct {
		Key      string `json:"key" binding:"required"`
		Value    string `json:"value" binding:"required"`
		Category string `json:"category"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	if isReservedBroadcastRuntimeKey(strings.TrimSpace(req.Key)) {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "自动播报运行状态必须通过专用管理接口修改"})
		return
	}

	if err := h.repo.WithContext(c.Request.Context()).Set(req.Key, req.Value, req.Category, ""); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新配置失败"})
		return
	}

	// 使配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateConfig(c.Request.Context(), req.Key)
		if req.Category != "" {
			_ = configCache.InvalidateCategory(c.Request.Context(), req.Category)
		}
	}

	// 记录审计日志（【H11 安全修复】密钥类值脱敏，防止明文落入 operator_logs）
	logValue := req.Value
	if isSensitiveConfigKey(req.Key) {
		logValue = maskConfigValue(req.Value)
	}
	oplog.AddLog(
		fmt.Sprintf("更新站点配置: %s = %s (分类: %s)", req.Key, logValue, req.Category),
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req struct {
		ICP string `json:"icp"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	if err := h.repo.WithContext(c.Request.Context()).SetICPConfig(req.ICP); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新ICP配置失败"})
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req gormdb.SystemInfoConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	if err := h.repo.WithContext(c.Request.Context()).SetSystemInfoConfig(req); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新系统信息配置失败"})
		return
	}

	// 使系统信息配置缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateSystemInfoConfig(c.Request.Context())
	}

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
		fmt.Sprintf("更新系统信息配置: 平台名称=%s, 语言=%s", req.Name, req.Language),
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

// UpdateAPRSConfig 更新APRS配置（管理员）
func (h *SiteConfigHandler) UpdateAPRSConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req gormdb.APRSConfig

	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	// 校验经纬度范围
	if req.Longitude < -180 || req.Longitude > 180 {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "经度必须在 -180 到 180 之间"})
		return
	}
	if req.Latitude < -90 || req.Latitude > 90 {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "纬度必须在 -90 到 90 之间"})
		return
	}

	if err := h.repo.WithContext(c.Request.Context()).SetAPRSConfig(req); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新APRS配置失败"})
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

// GetAccessDiscoveryConfig 获取设备接入点发现配置（管理员）。
func (h *SiteConfigHandler) GetAccessDiscoveryConfig(c *gin.Context) {
	if _, exists := c.Get("user"); !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	settings, err := accessDiscoveryForRequest(c)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取接入点配置失败"})
		return
	}
	c.JSON(http.StatusOK, Response{Code: 200, Message: "获取成功", Data: settings})
}

// UpdateAccessDiscoveryConfig 更新设备接入点发现配置（管理员）。
func (h *SiteConfigHandler) UpdateAccessDiscoveryConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	var req gormdb.AccessDiscoveryConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	normalized, err := normalizeAccessDiscoveryConfig(&req)
	if err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if err := h.repo.WithContext(c.Request.Context()).SetAccessDiscoveryConfig(*normalized); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新接入点配置失败"})
		return
	}
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateAll(c.Request.Context())
		_ = configCache.InvalidateCategory(c.Request.Context(), gormdb.CategoryAccessDiscovery)
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}
	oplog.AddLog(
		fmt.Sprintf("更新接入点配置: 节点=%s, 设备入口=%s:%d", normalized.NodeID, normalized.UDPHost, normalized.UDPPort),
		"config_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)
	c.JSON(http.StatusOK, Response{Code: 200, Message: "更新成功", Data: normalized})
}

// GetAPRSConfig 获取APRS配置（管理员）
func (h *SiteConfigHandler) GetAPRSConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
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
		config, err = h.repo.WithContext(c.Request.Context()).GetAPRSConfig()
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取APRS配置失败"})
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
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
		config, err = h.repo.WithContext(c.Request.Context()).GetSystemInfoConfig()
	}
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取系统信息配置失败"})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    resolveSystemInfoURLs(config),
	})
}

// GetRegistrationConfig 获取注册配置（管理员）
func (h *SiteConfigHandler) GetRegistrationConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	config, err := h.repo.WithContext(c.Request.Context()).GetRegistrationConfig()
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取注册配置失败"})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    config,
	})
}

// UpdateRegistrationConfig 更新注册配置（管理员）
func (h *SiteConfigHandler) UpdateRegistrationConfig(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req gormdb.RegistrationConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	if err := h.repo.WithContext(c.Request.Context()).SetRegistrationConfig(req); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新注册配置失败"})
		return
	}

	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateCategory(c.Request.Context(), gormdb.CategoryRegistration)
		_ = configCache.InvalidateConfig(c.Request.Context(), "registration.require_email_verification")
	}

	oplog.AddLog(
		fmt.Sprintf("更新注册配置: 邮箱验证必需=%t", req.RequireEmailVerification),
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

// GetAPRSLogs 获取APRS日志（管理员）
func (h *SiteConfigHandler) GetAPRSLogs(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	// 清空 Logo URL 配置（将值设置为空字符串）
	if err := h.repo.WithContext(c.Request.Context()).Set("system.logo_url", "", "system", "站点Logo URL"); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "删除Logo配置失败"})
		return
	}

	// 使系统信息配置缓存和分类缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateSystemInfoConfig(c.Request.Context())
		_ = configCache.InvalidateCategory(c.Request.Context(), "system")
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

// DeleteFavicon 删除站点Favicon（管理员）
func (h *SiteConfigHandler) DeleteFavicon(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	// 清空 Favicon URL 配置（将值设置为空字符串）
	if err := h.repo.WithContext(c.Request.Context()).Set("system.favicon_url", "", "system", "站点Favicon URL"); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "删除Favicon配置失败"})
		return
	}

	// 使系统信息配置缓存和分类缓存失效
	if configCache := cache.GetConfigCache(); configCache != nil {
		_ = configCache.InvalidateSystemInfoConfig(c.Request.Context())
		_ = configCache.InvalidateCategory(c.Request.Context(), "system")
	}

	// 记录审计日志
	oplog.AddLog(
		"删除站点Favicon",
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	_ = user // 路由已通过 RequireAdmin 中间件验证权限

	config, err := h.repo.WithContext(c.Request.Context()).GetSMTPConfig()
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "获取SMTP配置失败"})
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
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}

	userModel, userValid := user.(*gormdb.User)
	if !userValid || userModel == nil {
		writeErrorResponse(c, http.StatusUnauthorized, gin.H{"message": "登录状态无效，请重新登录"})
		return
	}

	var req gormdb.SMTPConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	// 验证端口范围
	if req.Port < 1 || req.Port > 65535 {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "端口必须在1-65535之间"})
		return
	}

	// 生产环境强制 TLS 证书校验
	if config.Get().IsProduction() && req.InsecureSkipVerify {
		writeErrorResponse(c, http.StatusBadRequest, gin.H{"code": 400, "message": "生产环境禁止开启 InsecureSkipVerify"})
		return
	}

	// 如果密码是脱敏格式（包含*），则不更新密码
	existingConfig, _ := h.repo.WithContext(c.Request.Context()).GetSMTPConfig()
	if existingConfig != nil && len(req.Password) > 0 && containsStars(req.Password) {
		req.Password = existingConfig.Password
	}

	if err := h.repo.WithContext(c.Request.Context()).SetSMTPConfig(req); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"code": 500, "message": "更新SMTP配置失败"})
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

func normalizeAccessDiscoveryConfig(settings *gormdb.AccessDiscoveryConfig) (*gormdb.AccessDiscoveryConfig, error) {
	if settings == nil {
		return nil, fmt.Errorf("接入点配置不存在")
	}
	result := *settings
	if strings.TrimSpace(result.NodeID) == "" {
		result.NodeID = "center"
	}
	id, err := accesspoint.NormalizePublicID(result.NodeID)
	if err != nil {
		return nil, fmt.Errorf("节点 ID 无效: %w", err)
	}
	result.NodeID = id
	host := strings.TrimSpace(result.UDPHost)
	if host != "" {
		host, err = accesspoint.NormalizeUDPHost(host)
		if err != nil {
			return nil, fmt.Errorf("节点 IP／域名无效: %w", err)
		}
	}
	result.UDPHost = host
	if result.UDPPort == 0 {
		result.UDPPort = 60050
		if cfg := config.TryGet(); cfg != nil {
			if port, parseErr := strconv.Atoi(cfg.System.Port); parseErr == nil && accesspoint.ValidateUDPPort(port) == nil {
				result.UDPPort = port
			}
		}
	}
	if err := accesspoint.ValidateUDPPort(result.UDPPort); err != nil {
		return nil, fmt.Errorf("节点 UDP 端口无效: %w", err)
	}
	return &result, nil
}
