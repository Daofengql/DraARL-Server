//go:build !embed
// +build !embed

package server

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
)

// setupFrontend 设置前端静态文件服务（开发模式，从磁盘读取）
func setupFrontend(engine *gin.Engine) {
	// 静态文件服务（前端）- 从磁盘读取
	engine.Static("/assets", "./www/dist/assets")
	engine.StaticFile("/", "./www/dist/index.html")

	// SPA fallback：所有非 API 和非 /ws 的路由都返回 index.html
	engine.NoRoute(func(c *gin.Context) {
		// 跳过 API 路由
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/ws") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}

		// 跳过静态资源请求（带扩展名的文件）
		if strings.Contains(c.Request.URL.Path, ".") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}

		c.File("./www/dist/index.html")
	})

	log.Println("Frontend static files enabled (from disk, development mode)")
}
