//go:build embed
// +build embed

package server

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed web/dist
var webFS embed.FS

// setupFrontend 设置前端静态文件服务（嵌入模式）
func setupFrontend(engine *gin.Engine) {
	webStaticFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Println("Frontend static files not found, running in API-only mode")
		return
	}

	// 禁用 Gin 的自动尾随斜杠重定向，避免重定向循环
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false

	// 静态资源路由（优先匹配）
	engine.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS("assets/"+c.Param("filepath"), http.FS(webStaticFS))
	})

	// 根路径返回 index.html
	engine.GET("/", func(c *gin.Context) {
		c.FileFromFS("/", http.FS(webStaticFS))
	})

	// SPA fallback：所有非 API 和非 /ws 的路由都返回 index.html 页面内容
	engine.NoRoute(func(c *gin.Context) {
		// 跳过后端专属路由
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/ws") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}

		// 使用 "/" 获取目录下的默认索引文件，支持 Vue/React 等单页应用的路由回退
		c.FileFromFS("/", http.FS(webStaticFS))
	})

	log.Println("Frontend static files enabled (embedded)")
}
