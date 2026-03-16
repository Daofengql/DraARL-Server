//go:build embed
// +build embed

package server

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"nrllink/internal/gormdb"

	"github.com/gin-gonic/gin"
)

//go:embed web/dist
var webFS embed.FS

// 缓存的 index.html 内容（不含 title）
var indexHTMLTemplate string

// setupFrontend 设置前端静态文件服务（嵌入模式）
func setupFrontend(engine *gin.Engine) {
	webStaticFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Println("Frontend static files not found, running in API-only mode")
		return
	}

	// 预加载 index.html 模板
	indexContent, err := fs.ReadFile(webStaticFS, "index.html")
	if err != nil {
		log.Printf("Failed to read index.html: %v", err)
	} else {
		indexHTMLTemplate = string(indexContent)
	}

	// 禁用 Gin 的自动尾随斜杠重定向，避免重定向循环
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false

	// 静态资源路由（优先匹配）
	engine.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS("assets/"+c.Param("filepath"), http.FS(webStaticFS))
	})

	// 渲染 index.html 并动态替换 title
	renderIndex := func(c *gin.Context) {
		if indexHTMLTemplate == "" {
			c.String(500, "index.html not found")
			return
		}

		// 获取站点名称
		siteName := "麟云链路" // 默认值
		if repo := gormdb.GetSiteConfigRepo(); repo != nil {
			if systemConfig, err := repo.GetSystemInfoConfig(); err == nil && systemConfig.Name != "" {
				siteName = systemConfig.Name
			}
		}

		// 根据 path 确定页面标题后缀
		path := c.Request.URL.Path
		titleSuffix := ""
		switch {
		case path == "/login":
			titleSuffix = " - 登录"
		case path == "/register":
			titleSuffix = " - 注册"
		case path == "/dashboard" || path == "/admin/dashboard":
			titleSuffix = " - 仪表盘"
		case strings.HasPrefix(path, "/admin/"):
			titleSuffix = " - 管理后台"
		case path == "/devices":
			titleSuffix = " - 我的设备"
		case path == "/groups":
			titleSuffix = " - 我的群组"
		case path == "/profile":
			titleSuffix = " - 个人中心"
		case path == "/comm-records":
			titleSuffix = " - 通信记录"
		}

		// 动态替换模板占位符
		html := strings.Replace(indexHTMLTemplate, "{{siteName}}", siteName, -1)
		html = strings.Replace(html, "{{titleSuffix}}", titleSuffix, -1)

		c.Data(200, "text/html; charset=utf-8", []byte(html))
	}

	// 根路径返回 index.html
	engine.GET("/", renderIndex)

	// SPA fallback：所有非 API 和非 /ws 的路由都返回 index.html 页面内容
	engine.NoRoute(func(c *gin.Context) {
		// 跳过后端专属路由
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/ws") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}

		// 使用动态渲染的 index.html
		renderIndex(c)
	})

	log.Println("Frontend static files enabled (embedded)")
}
