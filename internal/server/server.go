package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"nrllink/internal/config"
	"nrllink/internal/handler"
	"nrllink/internal/middleware"
)

type Server struct {
	config *config.Configuration
	engine *gin.Engine
	server *http.Server
}

func New(cfg *config.Configuration) *Server {
	engine := gin.Default()

	// CORS 中间件
	engine.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	s := &Server{
		config: cfg,
		engine: engine,
	}

	s.setupRoutes()

	return s
}

func (s *Server) setupRoutes() {
	// 静态文件服务（前端）
	s.engine.Static("/assets", "./www/dist/assets")
	s.engine.StaticFile("/", "./www/dist/index.html")
	s.engine.NoRoute(func(c *gin.Context) {
		c.File("./www/dist/index.html")
	})

	// API 路由
	api := s.engine.Group("/api")
	{
		// 认证路由（无需 JWT）
		auth := api.Group("/auth")
		{
			auth.POST("/login", handler.Login)
			auth.POST("/logout", handler.Logout)
			auth.POST("/register", handler.Register)
		}

		// 平台信息（无需认证）
		api.GET("/platform/info", handler.GetPlatformInfo)
		api.GET("/platform/totalstats", handler.GetTotalStats)

		// 需要认证的路由
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware())
		{
			// 用户相关
			protected.GET("/me", handler.GetCurrentUser)
			protected.GET("/users", handler.GetUsers)
			protected.POST("/users", handler.CreateUser)
			protected.PUT("/users/:id", handler.UpdateUser)
			protected.DELETE("/users/:id", handler.DeleteUser)
			protected.PUT("/users/:id/password", handler.UpdateUserPassword)
			protected.GET("/users/:id", handler.GetUserDetail)

			// 设备相关
			protected.GET("/devices", handler.GetDevices)
			protected.GET("/devices/list", handler.GetDevices) // 兼容旧接口
			protected.GET("/device/get", handler.GetDevice)
			protected.GET("/device/qths", handler.GetDeviceQTHs)
			protected.POST("/devices", handler.CreateDevice)
			protected.PUT("/devices/:id", handler.UpdateDevice)
			protected.DELETE("/devices/:id", handler.DeleteDevice)
			protected.POST("/device/changegroupnrl", handler.ChangeDeviceGroup)

			// 群组相关
			protected.GET("/groups", handler.GetGroups)
			protected.GET("/group/list", handler.GetGroups) // 兼容旧接口
			protected.GET("/groups/:id", handler.GetGroup)
			protected.GET("/groups/:id/devices", handler.GetGroupDevices)
			protected.POST("/groups", handler.CreateGroup)
			protected.POST("/group/create", handler.CreateGroup) // 兼容旧接口
			protected.PUT("/groups/:id", handler.UpdateGroup)
			protected.POST("/group/update", handler.UpdateGroup) // 兼容旧接口
			protected.DELETE("/groups/:id", handler.DeleteGroup)
			protected.POST("/group/delete", handler.DeleteGroup) // 兼容旧接口

			// 中继台和服务器
			protected.GET("/relays", handler.GetRelays)
			protected.GET("/relay/list", handler.GetRelays) // 兼容旧接口
			protected.GET("/servers", handler.GetServers)
			protected.GET("/server/list", handler.GetServers) // 兼容旧接口
		}
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%s", s.config.Web.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	log.Printf("HTTP 服务器启动在 %s", addr)

	// 在 goroutine 中启动服务器
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务器启动失败: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭 HTTP 服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
