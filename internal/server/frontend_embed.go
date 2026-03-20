//go:build embed
// +build embed

package server

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"nrllink/internal/common"
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

	// 其他静态资源目录
	for _, dir := range []string{"css", "js", "fonts", "img", "docs"} {
		d := dir // 捕获循环变量
		engine.GET("/"+d+"/*filepath", func(c *gin.Context) {
			c.FileFromFS(d+c.Param("filepath"), http.FS(webStaticFS))
		})
	}

	// 渲染 index.html 并动态替换 title
	renderIndex := func(c *gin.Context) {
		if indexHTMLTemplate == "" {
			c.String(500, "index.html not found")
			return
		}

		// 获取站点名称
		siteName := common.SiteName // 默认值
		if repo := gormdb.GetSiteConfigRepo(); repo != nil {
			if systemConfig, err := repo.GetSystemInfoConfig(); err == nil && systemConfig.Name != "" {
				siteName = systemConfig.Name
			}
		}

		// 根据 path 确定页面标题后缀（与前端 routeTitleMap 保持一致）
		path := c.Request.URL.Path
		titleSuffix := ""
		switch path {
		case "/login":
			titleSuffix = " - 登录"
		case "/register":
			titleSuffix = " - 注册"
		case "/dashboard":
			titleSuffix = " - 仪表盘"
		case "/devices":
			titleSuffix = " - 我的设备"
		case "/groups":
			titleSuffix = " - 我的群组"
		case "/profile":
			titleSuffix = " - 个人中心"
		case "/comm-records":
			titleSuffix = " - 通信记录"
		case "/docs":
			titleSuffix = " - 技术支持"
		case "/admin/dashboard":
			titleSuffix = " - 仪表盘"
		case "/admin/users":
			titleSuffix = " - 用户管理"
		case "/admin/approvals":
			titleSuffix = " - 用户审批"
		case "/admin/certificate-approvals":
			titleSuffix = " - 操作证审批"
		case "/admin/devices":
			titleSuffix = " - 设备管理"
		case "/admin/relays":
			titleSuffix = " - 中继台"
		case "/admin/servers":
			titleSuffix = " - 服务器"
		case "/admin/groups":
			titleSuffix = " - 群组管理"
		case "/admin/group-links":
			titleSuffix = " - 互联管理"
		case "/admin/comm-records":
			titleSuffix = " - 通信记录"
		case "/admin/assets":
			titleSuffix = " - 资源管理"
		case "/admin/settings":
			titleSuffix = " - 站点配置"
		default:
			// 其他管理后台路由
			if strings.HasPrefix(path, "/admin/") {
				titleSuffix = " - 管理后台"
			}
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
