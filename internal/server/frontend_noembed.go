//go:build !embed
// +build !embed

package server

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
)

// setupFrontend 设置前端静态文件服务（API 模式，无嵌入）
func setupFrontend(engine *gin.Engine) {
	// SPA fallback：所有非 API 和非 /ws 的路由都返回 404
	engine.NoRoute(func(c *gin.Context) {
		// 跳过后端专属路由
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/ws") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		c.JSON(404, gin.H{"error": "frontend not embedded, build with -tags=embed"})
	})

	log.Println("Running in API-only mode (no embedded frontend)")
}
