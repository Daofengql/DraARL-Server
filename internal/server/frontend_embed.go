//go:build embed
// +build embed

package server

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"draarl/internal/common"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

//go:embed web/dist
var webFS embed.FS

// 缓存的 index.html 内容（不含 title）
var indexHTMLTemplate string

// setupFrontend 设置前端静态文件服务（嵌入模式）。前端资源始终由主程序
// 提供；如需 CDN，部署方可以直接在主站 HTTP 层外置配置。
func setupFrontend(engine *gin.Engine, _ *config.Configuration) {
	webStaticFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Println("Frontend static files not found, running in API-only mode")
		return
	}

	indexContent, err := fs.ReadFile(webStaticFS, "index.html")
	if err != nil {
		log.Printf("Failed to read index.html: %v", err)
	} else {
		indexHTMLTemplate = string(indexContent)
	}

	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false

	engine.GET("/assets/*filepath", func(c *gin.Context) {
		setFrontendAssetCache(c, true)
		c.FileFromFS("assets/"+strings.TrimPrefix(c.Param("filepath"), "/"), http.FS(webStaticFS))
	})

	for _, dir := range []string{"css", "js", "fonts", "img", "docs"} {
		d := dir
		engine.GET("/"+d+"/*filepath", func(c *gin.Context) {
			setFrontendAssetCache(c, false)
			c.FileFromFS(d+c.Param("filepath"), http.FS(webStaticFS))
		})
	}

	for _, fileName := range []string{"vite.svg"} {
		staticFile := fileName
		engine.GET("/"+staticFile, func(c *gin.Context) {
			setFrontendAssetCache(c, false)
			c.FileFromFS(staticFile, http.FS(webStaticFS))
		})
	}

	renderIndex := func(c *gin.Context) {
		if indexHTMLTemplate == "" {
			c.String(http.StatusInternalServerError, "index.html not found")
			return
		}

		siteName := common.SiteName
		faviconURL := "/vite.svg"
		if repo := gormdb.GetSiteConfigRepo(); repo != nil {
			if systemConfig, err := repo.GetSystemInfoConfig(); err == nil {
				if systemConfig.Name != "" {
					siteName = systemConfig.Name
				}
				if systemConfig.FaviconURL != "" {
					faviconURL = storage.ResolveAssetURL(systemConfig.FaviconURL)
				}
			}
		}

		titleSuffix := frontendTitleSuffix(c.Request.URL.Path)
		html := strings.ReplaceAll(indexHTMLTemplate, "{{siteName}}", siteName)
		html = strings.ReplaceAll(html, "{{titleSuffix}}", titleSuffix)
		html = strings.ReplaceAll(html, "{{faviconURL}}", faviconURL)
		html = strings.ReplaceAll(html, "{{assetBaseURL}}", "/")
		html = strings.ReplaceAll(html, "./assets/", "/assets/")

		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
	}

	engine.GET("/", renderIndex)
	engine.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/ws") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		renderIndex(c)
	})

	log.Println("Frontend static files enabled (embedded)")
}

func setFrontendAssetCache(c *gin.Context, immutable bool) {
	if immutable {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
}

func frontendTitleSuffix(requestPath string) string {
	switch requestPath {
	case "/login":
		return " - 登录"
	case "/register":
		return " - 注册"
	case "/dashboard":
		return " - 仪表盘"
	case "/devices":
		return " - 我的设备"
	case "/groups":
		return " - 我的群组"
	case "/profile":
		return " - 个人中心"
	case "/comm-records":
		return " - 通信记录"
	case "/relays":
		return " - 中继台查询"
	case "/tools":
		return " - 工具"
	case "/docs":
		return " - 技术支持"
	case "/admin/dashboard":
		return " - 仪表盘"
	case "/admin/users":
		return " - 用户管理"
	case "/admin/approvals":
		return " - 用户审批"
	case "/admin/certificate-approvals":
		return " - 操作证审批"
	case "/admin/devices":
		return " - 设备管理"
	case "/admin/relays":
		return " - 中继台"
	case "/admin/servers":
		return " - 服务器"
	case "/admin/groups":
		return " - 群组管理"
	case "/admin/group-links":
		return " - 互联管理"
	case "/admin/comm-records":
		return " - 通信记录"
	case "/admin/assets":
		return " - 资源管理"
	case "/admin/settings":
		return " - 站点配置"
	default:
		if strings.HasPrefix(requestPath, "/admin/") {
			return " - 管理后台"
		}
		return ""
	}
}
