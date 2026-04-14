package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"draarl/internal/common"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/handler"
	"draarl/internal/middleware"
	"draarl/pkg/minio"
	ws "draarl/pkg/websocket"

	"github.com/gin-gonic/gin"
)

type Server struct {
	config *config.Configuration
	engine *gin.Engine
	server *http.Server
}

func New(cfg *config.Configuration) *Server {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(accessLogMiddleware())

	allowedOrigins := cfg.GetAllowedOrigins()
	allowedOriginSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowedOriginSet[strings.ToLower(origin)] = struct{}{}
	}

	// 初始化站点配置（如果数据库为空则从YAML迁移）
	initSiteConfigs(cfg)

	// WebSocket Origin 白名单
	ws.SetAllowedOrigins(allowedOrigins)

	// CORS 中间件
	engine.Use(originGuardMiddleware(allowedOriginSet))

	s := &Server{
		config: cfg,
		engine: engine,
	}

	s.setupRoutes()

	// 前端 CDN 模式会在 setupFrontend 中同步初始化 MinIO。
	// 其他情况仍按原有方式后台初始化，避免阻塞主服务启动。
	if !minio.IsEnabled() {
		minio.StartInitMinIOInBackground()
	}

	return s
}

func (s *Server) setupRoutes() {
	// 前端静态文件服务（根据编译标签选择嵌入模式或磁盘模式）
	setupFrontend(s.engine, s.config)

	// API 路由
	api := s.engine.Group("/api")
	{
		// 认证路由（无需 JWT）
		auth := api.Group("/auth")
		{
			auth.POST("/login", handler.Login)
			auth.POST("/logout", handler.Logout)
			auth.POST("/refresh", handler.RefreshToken)
			auth.POST("/register", handler.Register)
			auth.POST("/ws-token/clear", handler.ClearWSTokenCookie)
			auth.POST("/email-login", handler.EmailLogin)         // 邮箱验证码登录
			auth.POST("/send-code", handler.SendVerificationCode) // 发送邮箱验证码
			auth.POST("/verify-email", handler.VerifyEmail)       // 验证邮箱（注册用）
			auth.POST("/reset-password", handler.ResetPassword)   // 重置密码
		}

		// 验证码路由（无需认证）
		api.GET("/captcha", handler.GetCaptcha)

		// 平台信息（无需认证）
		api.GET("/platform/info", handler.GetPlatformInfo)
		api.GET("/platform/totalstats", handler.GetTotalStats)

		// 站点配置（公开配置，无需认证）
		api.GET("/config/public", handler.NewSiteConfigHandler().GetPublicConfigs)

		// 公开接口（无需认证）
		api.GET("/public/relays", middleware.PublicRelaySearchRateLimit(), handler.PublicSearchRelays)

		// Keycloak SSO 路由（无需认证）
		sso := api.Group("/sso")
		{
			sso.GET("/login", handler.GetSSOLoginURL)
			sso.GET("/callback", handler.SSOCallback)
			sso.POST("/exchange", handler.ExchangeSSOCode)
		}

		// 设备动态码绑定接口（设备端，无需 JWT）
		device := api.Group("/device")
		{
			device.POST("/pre-check", middleware.PreCheckRateLimit(), handler.PreCheck)
			device.POST("/request-code", middleware.RequestCodeRateLimit(), handler.RequestCode)
			device.POST("/confirm-bind", middleware.ConfirmBindRateLimit(), handler.ConfirmBind)
		}

		// 需要认证的路由
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware())
		protected.Use(middleware.LoadUserInfo()) // 加载用户ID到context
		{
			// SSO 绑定相关（需认证）
			protected.GET("/sso/status", handler.GetSSOStatus)
			protected.POST("/sso/bind", handler.SSOBind)
			protected.DELETE("/sso/unbind", handler.SSOUnbind)
			protected.POST("/auth/ws-token/sync", handler.SyncWSTokenCookie)

			// 当前用户信息（所有认证用户可访问）
			protected.GET("/me", handler.GetCurrentUser)
			protected.PUT("/me", handler.UpdateProfile)
			protected.PUT("/me/password", handler.ChangeOwnPassword)
			protected.PUT("/me/email", handler.ChangeEmail) // 修改邮箱

			// 设备密码管理（所有认证用户可访问）
			protected.GET("/user/device-password", handler.GetDevicePassword)
			protected.PUT("/user/device-password", handler.UpdateDevicePassword)
			protected.POST("/user/device-password/regenerate", handler.RegenerateDevicePassword)

			// 设备动态码绑定（Web 端，需要审核通过）
			protected.POST("/device/bind", middleware.BindRateLimit(), handler.BindDevice)
			protected.POST("/device/submit-config", middleware.SubmitConfigRateLimit(), handler.SubmitDeviceConfig)

			// 文件上传（所有认证用户可访问，用于头像上传）
			protected.POST("/upload/file", handler.UploadFile)

			// 操作证相关（所有认证用户可访问）
			protected.POST("/upload/operator-certificate", handler.UploadOperatorCertificate)
			protected.GET("/operator-certificate", handler.GetOperatorCertificate)

			// 通联日志（用户只能操作自己的记录）
			protected.GET("/logbooks", handler.GetLogbooks)
			protected.GET("/logbooks/:id", handler.GetLogbook)
			protected.POST("/logbooks", handler.CreateLogbook)
			protected.PUT("/logbooks/:id", handler.UpdateLogbook)
			protected.DELETE("/logbooks/:id", handler.DeleteLogbook)
			protected.DELETE("/logbooks/batch", handler.BatchDeleteLogbooks)

			// 电台预设（用户只能操作自己的预设）
			protected.GET("/user/radio-presets", handler.GetRadioPresets)
			protected.POST("/user/radio-presets", handler.CreateRadioPreset)
			protected.PUT("/user/radio-presets/:id", handler.UpdateRadioPreset)
			protected.DELETE("/user/radio-presets/:id", handler.DeleteRadioPreset)
			protected.PUT("/user/radio-presets/reorder", handler.ReorderRadioPresets)

			// 用户管理（部分需要管理员权限）
			admin := protected.Group("")
			admin.Use(middleware.RequireAdmin())
			{
				admin.GET("/users", handler.GetUsers)
				admin.POST("/users", handler.CreateUser)
				admin.PUT("/users/:id", handler.UpdateUser)
				admin.DELETE("/users/:id", handler.DeleteUser)
				admin.PUT("/users/:id/status", handler.UpdateUserStatus)
				admin.GET("/users/:id", handler.GetUserDetail)

				// 用户审批相关
				admin.GET("/approvals/pending", handler.GetPendingApprovals)
				admin.PUT("/approvals/:id/approve", handler.ApproveUser)

				// 操作证审批相关
				admin.GET("/certificate-approvals", handler.GetCertificateApprovals)
				admin.PUT("/operator-certificates/:id/approve", handler.ApproveOperatorCertificate)

				// Logo和Favicon管理（管理员专用）
				admin.POST("/upload/logo", handler.UploadLogo)
				admin.DELETE("/config/logo", handler.NewSiteConfigHandler().DeleteLogo)
				admin.POST("/upload/favicon", handler.UploadFavicon)
				admin.DELETE("/config/favicon", handler.NewSiteConfigHandler().DeleteFavicon)

				// 通联日志管理（管理员可操作所有记录）
				admin.GET("/admin/logbooks", handler.AdminGetLogbooks)
				admin.GET("/admin/logbooks/:id", handler.AdminGetLogbook)
				admin.PUT("/admin/logbooks/:id", handler.AdminUpdateLogbook)
				admin.DELETE("/admin/logbooks/:id", handler.AdminDeleteLogbook)
				admin.DELETE("/admin/logbooks/batch", handler.AdminBatchDeleteLogbooks)
			}

			// 修改用户密码（用户本人或管理员可访问）
			protected.PUT("/users/:id/password", handler.UpdateUserPassword)
			// 获取用户公开信息（任何登录用户可访问）
			protected.GET("/users/:id/public", handler.GetUserPublicInfo)
			// 通过用户名获取用户公开信息（任何登录用户可访问）
			protected.GET("/users/name/:username/public", handler.GetUserPublicInfoByName)

			// 设备相关（需要审核通过）
			approved := protected.Group("")
			approved.Use(middleware.RequireApproved())
			{
				approved.GET("/devices", handler.GetDevices)
				approved.GET("/devices/list", handler.GetDevices) // 兼容旧接口
				approved.GET("/device/get", handler.GetDevice)
				approved.GET("/device/qths", handler.GetDeviceQTHs)
				approved.PUT("/devices/:id", handler.UpdateDevice)
				approved.DELETE("/devices/:id", handler.DeleteDevice)
				approved.POST("/device/changegroup", handler.ChangeDeviceGroup)
				approved.PUT("/devices/:id/group", handler.ChangeDeviceGroup) // RESTful 风格

				// 设备 AT 命令和参数
				approved.POST("/device/at", handler.DeviceAT)
				approved.POST("/device/query", handler.QueryDeviceParm)
				approved.POST("/device/change", handler.ChangeDeviceParm)
				approved.POST("/device/change1w", handler.Change1W)
				approved.POST("/device/change2w", handler.Change2W)
				approved.GET("/device/qth", handler.GetDevice) // 兼容旧接口

				// 设备配置同步 API（UDP 普通设备）
				approved.GET("/devices/:id/config", handler.GetDeviceConfig)
				approved.PUT("/devices/:id/config", handler.UpdateDeviceConfig)
				approved.POST("/devices/:id/config/sync", handler.SyncDeviceConfig)

				// 群组相关
				approved.GET("/groups", handler.GetGroups)
				approved.GET("/group/list", handler.GetGroups) // 兼容旧接口
				approved.GET("/groups/:id", handler.GetGroup)
				approved.GET("/groups/:id/devices", handler.GetGroupDevices)
				approved.POST("/groups", handler.CreateGroup)
				approved.POST("/group/create", handler.CreateGroup) // 兼容旧接口
				approved.POST("/groups/search", handler.SearchGroups)
				// 加入群组
				approved.POST("/groups/:id/join", handler.JoinGroup)
				// 获取群组成员列表
				approved.GET("/groups/:id/members", handler.GetGroupMembers)
				// 离开群组
				approved.POST("/groups/:id/leave", handler.LeaveGroup)

				// 群组管理操作（需要群组所有者或管理员权限）
				groupOwner := approved.Group("")
				groupOwner.Use(middleware.RequireAdminOrOwner())
				{
					groupOwner.PUT("/groups/:id", handler.UpdateGroup)
					groupOwner.POST("/group/update", handler.UpdateGroup) // 兼容旧接口
					groupOwner.DELETE("/groups/:id", handler.DeleteGroup)
					groupOwner.POST("/group/delete", handler.DeleteGroup) // 兼容旧接口
					// 踢出设备
					groupOwner.DELETE("/groups/:id/devices/:deviceId", handler.KickDevice)
				}
			}

			// 虚拟互联组管理（需要管理员权限）
			admin.POST("/group-links", handler.CreateVirtualGroup)
			admin.GET("/group-links", handler.GetVirtualGroups)
			admin.GET("/group-links/available-targets", handler.GetAvailableTargetGroups)
			admin.GET("/group-links/:id", handler.GetVirtualGroup)
			admin.PUT("/group-links/:id", handler.UpdateVirtualGroup)
			admin.DELETE("/group-links/:id", handler.DeleteVirtualGroup)
			admin.GET("/group-links/:id/targets", handler.GetGroupLinkTargets)
			admin.POST("/group-links/:id/targets", handler.AddGroupLinkTarget)
			admin.DELETE("/group-links/:id/targets/:targetId", handler.RemoveGroupLinkTarget)

			// 中继台和服务器（需要管理员权限）
			admin.GET("/relays", handler.GetRelays)
			admin.GET("/relay/list", handler.GetRelays) // 兼容旧接口
			admin.POST("/relay/create", handler.CreateRelay)
			admin.POST("/relay/update", handler.UpdateRelay)
			admin.POST("/relay/delete", handler.DeleteRelay)
			admin.GET("/servers", handler.GetServers)
			admin.GET("/server/list", handler.GetServers) // 兼容旧接口
			admin.POST("/server/create", handler.CreateServer)
			admin.POST("/server/update", handler.UpdateServer)
			admin.POST("/server/delete", handler.DeleteServer)

			// 设备配置管理（管理员权限，可操作任意设备）
			admin.GET("/admin/devices/:id/config", handler.AdminGetDeviceConfig)
			admin.PUT("/admin/devices/:id/config", handler.AdminUpdateDeviceConfig)
			admin.POST("/admin/devices/:id/config/sync", handler.AdminSyncDeviceConfig)

			// 操作日志（需要管理员权限）
			admin.GET("/operatorlog/list", handler.GetOperatorLogs)
			admin.GET("/operatorlog/stats", handler.GetOperatorLogStats)

			// 通信记录（需要登录，管理员可查看所有记录，普通用户只能查看自己设备的记录）
			// 注意：具体路径必须在 :id 路由之前定义，否则会被 :id 匹配
			protected.GET("/comm-records", handler.GetCommRecords)
			protected.GET("/comm-records/user-stats", handler.GetUserCommStats)
			protected.GET("/comm-records/user-trend", handler.GetUserCommTrend)
			protected.GET("/comm-records/:id", handler.GetCommRecord)
			// 通信记录管理（需要管理员权限）
			admin.DELETE("/comm-records/:id", handler.DeleteCommRecord)
			admin.GET("/comm-records/stats", handler.GetCommRecorderStats)
			admin.GET("/comm-records/system-stats", handler.GetSystemCommStats)
			admin.GET("/comm-records/system-trend", handler.GetSystemCommTrend)

			// 通信设置（管理员权限）
			admin.GET("/config/comm-settings", handler.GetCommSettings)
			admin.PUT("/config/comm-settings", handler.UpdateCommSettings)

			// 缓存监控（需要管理员权限）
			cacheHandler := handler.NewCacheMetricsHandler()
			admin.GET("/cache/metrics", cacheHandler.GetCacheMetrics)
			admin.POST("/cache/metrics/reset", cacheHandler.ResetCacheMetrics)
			admin.POST("/cache/clear", cacheHandler.ClearAllCache)

			// 站点配置管理（读取需要登录，修改需要管理员权限）
			configHandler := handler.NewSiteConfigHandler()
			// 读取路由（已登录用户可访问）
			protected.GET("/config/category/:category", configHandler.GetConfigsByCategory)
			// 修改路由（需要管理员权限）
			admin.PUT("/config", configHandler.UpdateConfig)
			admin.PUT("/config/icp", configHandler.UpdateICPConfig)
			admin.PUT("/config/system", configHandler.UpdateSystemInfoConfig)
			admin.PUT("/config/aprs", configHandler.UpdateAPRSConfig)
			admin.PUT("/config/openai", configHandler.UpdateOpenAIConfig)
			admin.GET("/config/all", configHandler.GetAllConfigs)
			admin.GET("/config/system", configHandler.GetSystemInfoConfig)
			admin.GET("/config/aprs", configHandler.GetAPRSConfig)
			admin.GET("/config/openai", configHandler.GetOpenAIConfig)
			admin.GET("/config/aprs/logs", configHandler.GetAPRSLogs)
			// SMTP 配置（需要管理员权限）
			admin.GET("/config/smtp", configHandler.GetSMTPConfig)
			admin.PUT("/config/smtp", configHandler.UpdateSMTPConfig)

			// 资源管理（需要管理员权限）
			assetHandler := handler.NewAssetHandler()
			admin.GET("/assets", assetHandler.GetAssets)                // 获取资源列表
			admin.POST("/assets/folder", assetHandler.CreateFolder)     // 创建文件夹
			admin.POST("/assets/upload", assetHandler.UploadFile)       // 上传文件
			admin.PUT("/assets/:id", assetHandler.UpdateAsset)          // 更新资源（重命名、备注）
			admin.PUT("/assets/:id/move", assetHandler.MoveAsset)       // 移动资源
			admin.POST("/assets/:id/replace", assetHandler.ReplaceFile) // 覆盖文件
			admin.DELETE("/assets/:id", assetHandler.DeleteAsset)       // 删除资源

			// 资源公开接口（前台下载中心使用）
			api.GET("/assets/tree", assetHandler.GetAssetTree)           // 获取目录树
			api.GET("/assets/folder/:id", assetHandler.GetFolderFiles)   // 获取文件夹下的文件
			api.GET("/assets/:id/download", assetHandler.GetDownloadURL) // 获取下载链接

			// 在线收发 API（需要审核通过）
			radio := approved.Group("/radio")
			{
				radio.GET("/config", handler.GetRadioConfig)                   // 获取在线收发配置
				radio.PUT("/ssid", handler.UpdateRadioSSID)                    // 已废弃：Web 幽灵设备 SSID 固定为 105
				radio.GET("/status", handler.GetRadioStatus)                   // 获取幽灵设备状态
				radio.GET("/groups/stats", handler.GetRadioGroupStats)         // 获取所有群组实时统计（含 WS 设备）
				radio.GET("/groups/:id/devices", handler.GetRadioGroupDevices) // 获取群组在线设备
				radio.PUT("/group", handler.UpdateRadioGroup)                  // 【新增】切换幽灵设备群组
				radio.GET("/conflict", handler.CheckGhostDeviceConflict)       // 【新增】检查幽灵设备连接冲突
			}
		}
	}

	// WebSocket 路由（无需认证）
	s.engine.GET("/ws", func(c *gin.Context) {
		ws.HandleWebSocket(c.Writer, c.Request)
	})
}

// initSiteConfigs 初始化站点配置（如果数据库为空则创建默认值）
func initSiteConfigs(cfg *config.Configuration) {
	repo := gormdb.GetSiteConfigRepo()

	// 检查是否已有配置
	configs, err := repo.GetAll()
	if err != nil {
		log.Printf("检查站点配置失败: %v", err)
		return
	}

	// 如果已有配置，跳过初始化
	if len(configs) > 0 {
		log.Println("站点配置已存在，跳过初始化")
		return
	}

	log.Println("站点配置为空的初始化默认值")

	// 初始化默认配置（空值或最小默认值）
	if err := repo.InitDefaultConfigs(
		"",                   // ICP - 空
		common.SiteName,      // 系统名称
		common.SiteShortName, // 系统简称
		"",                   // Logo URL
		"zh",                 // 语言
		"china.aprs2.net",    // APRS 服务器
		"14580",              // APRS 端口
		"",                   // 本机地址
		"60050",              // 本机端口
		"",                   // 呼号
		"10",                 // SSID
		"000000",             // 海拔
		0,                    // 纬度
		0,                    // 经度
		"",                   // OpenAI BaseURL
		"",                   // OpenAI APIKey
		"",                   // OpenAI Engine
	); err != nil {
		log.Printf("初始化站点配置失败: %v", err)
		return
	}

	log.Println("站点配置初始化完成")

	// 初始化默认 SMTP 配置
	smtpConfig := gormdb.SMTPConfig{
		Host:        "smtp.qq.com",
		Port:        465,
		UseSSL:      true,
		SenderName:  "DraARL麟链",
		SenderEmail: "",
		Password:    "",
	}
	if err := repo.SetSMTPConfig(smtpConfig); err != nil {
		log.Printf("初始化 SMTP 配置失败: %v", err)
	} else {
		log.Println("SMTP 配置初始化完成")
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%s", s.config.Web.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	log.Printf("HTTP 服务器启动在 %s", addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown 优雅关闭 HTTP 服务
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	log.Println("正在关闭 HTTP 服务器...")
	if err := s.server.Shutdown(ctx); err != nil {
		log.Printf("HTTP 服务器关闭失败: %v", err)
		return err
	}
	log.Println("HTTP 服务器已关闭")
	return nil
}

func (s *Server) GetEngine() *gin.Engine {
	return s.engine
}

func originGuardMiddleware(allowedOriginSet map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestOrigin, hasBrowserSource := requestSourceOrigin(c)
		if hasBrowserSource {
			if requestOrigin == "" {
				abortOriginNotAllowed(c)
				return
			}
			if _, allowed := allowedOriginSet[requestOrigin]; !allowed {
				abortOriginNotAllowed(c)
				return
			}

			if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
				c.Writer.Header().Set("Vary", "Origin")
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
				c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			} else {
				c.Writer.Header().Set("Vary", "Origin, Referer")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if hasTokenLikeQuery(c.Request.URL.RawQuery) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"code":    400,
				"message": "token_query_not_allowed",
			})
			return
		}

		c.Next()
	}
}

func abortOriginNotAllowed(c *gin.Context) {
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"code":    403,
		"message": "origin_not_allowed",
	})
}

func requestSourceOrigin(c *gin.Context) (string, bool) {
	if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
		return normalizeOrigin(origin), true
	}

	if referer := strings.TrimSpace(c.GetHeader("Referer")); referer != "" {
		return originFromURL(referer), true
	}

	return "", false
}

func originFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return normalizeOrigin(parsed.Scheme + "://" + parsed.Host)
}

func normalizeOrigin(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed
}
