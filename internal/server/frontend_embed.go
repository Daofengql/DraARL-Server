//go:build embed
// +build embed

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"draarl/internal/common"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	miniohelper "draarl/pkg/minio"

	"github.com/gin-gonic/gin"
	minioapi "github.com/minio/minio-go/v7"
)

//go:embed web/dist
var webFS embed.FS

// 缓存的 index.html 内容（不含 title）
var indexHTMLTemplate string

const (
	defaultFrontendCDNManifest = ".draarl-frontend-manifest.json"
	frontendCDNSyncTimeout     = 2 * time.Minute
	frontendIndexFile          = "index.html"
)

type embeddedFrontendAsset struct {
	Path        string
	Data        []byte
	ContentType string
	Digest      string
}

type frontendCDNManifest struct {
	Checksum string                    `json:"checksum"`
	Files    []frontendCDNManifestFile `json:"files"`
}

type frontendCDNManifestFile struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Digest      string `json:"digest"`
}

type frontendCDNVersionEntry struct {
	Size        int64
	ContentType string
	Digest      string
}

// setupFrontend 设置前端静态文件服务（嵌入模式）
func setupFrontend(engine *gin.Engine, cfg *config.Configuration) {
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

	assetBaseURL := "/"
	if cfg != nil && cfg.Web.FrontendCDN.Enabled {
		cdnBaseURL, err := ensureFrontendAssetsInMinIO(webStaticFS, cfg)
		if err != nil {
			log.Printf("Frontend CDN sync failed, fallback to embedded assets: %v", err)
		} else {
			assetBaseURL = normalizeAssetBaseURL(cdnBaseURL)
			log.Printf("Frontend static assets served by MinIO: %s", assetBaseURL)
		}
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

	// 根目录静态文件（如默认 favicon）
	for _, fileName := range []string{"vite.svg"} {
		staticFile := fileName
		engine.GET("/"+staticFile, func(c *gin.Context) {
			c.FileFromFS(staticFile, http.FS(webStaticFS))
		})
	}

	// 渲染 index.html 并动态替换 title 和 favicon
	renderIndex := func(c *gin.Context) {
		if indexHTMLTemplate == "" {
			c.String(500, "index.html not found")
			return
		}

		// 获取站点名称和 favicon URL
		siteName := common.SiteName // 默认值
		faviconURL := assetBaseURL + "vite.svg"
		if repo := gormdb.GetSiteConfigRepo(); repo != nil {
			if systemConfig, err := repo.GetSystemInfoConfig(); err == nil {
				if systemConfig.Name != "" {
					siteName = systemConfig.Name
				}
				if systemConfig.FaviconURL != "" {
					faviconURL = systemConfig.FaviconURL
				}
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
		html = strings.Replace(html, "{{faviconURL}}", faviconURL, -1)
		html = strings.Replace(html, "{{assetBaseURL}}", assetBaseURL, -1)
		html = strings.ReplaceAll(html, "./assets/", assetBaseURL+"assets/")

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

func ensureFrontendAssetsInMinIO(webStaticFS fs.FS, cfg *config.Configuration) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("configuration is nil")
	}

	prefix := normalizeObjectPrefix(cfg.Web.FrontendCDN.ObjectPrefix)
	if prefix == "" {
		return "", fmt.Errorf("frontend CDN object prefix is empty")
	}

	if !miniohelper.IsEnabled() {
		if err := miniohelper.InitMinIO(); err != nil {
			return "", fmt.Errorf("init minio: %w", err)
		}
	}

	client := miniohelper.GetClient()
	if client == nil {
		return "", fmt.Errorf("minio client is not ready")
	}

	assets, manifest, err := loadEmbeddedFrontendAssets(webStaticFS)
	if err != nil {
		return "", fmt.Errorf("load embedded frontend assets: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), frontendCDNSyncTimeout)
	defer cancel()

	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	manifestObject := joinObjectPath(prefix, defaultFrontendCDNManifest)
	indexObject := joinObjectPath(prefix, frontendIndexFile)

	// index.html 始终由 Go 动态渲染，CDN 前缀下不保留它。
	if err := deleteObjectIfExists(ctx, client, bucket, indexObject); err != nil {
		log.Printf("Frontend CDN cleanup warning (%s): %v", indexObject, err)
	}

	storedManifest, err := readFrontendManifest(ctx, client, bucket, manifestObject)
	if err != nil {
		return "", fmt.Errorf("read frontend CDN manifest: %w", err)
	}

	if !hasFrontendManifestChanged(storedManifest, manifest) {
		return miniohelper.GetFileURL(prefix), nil
	}

	if err := purgeFrontendPrefix(ctx, client, bucket, prefix); err != nil {
		return "", fmt.Errorf("purge frontend CDN prefix: %w", err)
	}
	if storedManifest != nil {
		log.Printf("Frontend CDN manifest changed, purged MinIO prefix %s before re-upload", prefix)
	}

	uploadedFiles := 0
	var uploadedBytes int64
	for _, asset := range assets {
		objectName := joinObjectPath(prefix, asset.Path)
		if err := miniohelper.UploadFile(ctx, bucket, objectName, bytes.NewReader(asset.Data), int64(len(asset.Data)), asset.ContentType); err != nil {
			return "", fmt.Errorf("upload %s: %w", asset.Path, err)
		}
		uploadedFiles++
		uploadedBytes += int64(len(asset.Data))
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal frontend CDN manifest: %w", err)
	}
	if err := miniohelper.UploadFile(ctx, bucket, manifestObject, bytes.NewReader(manifestData), int64(len(manifestData)), "application/json"); err != nil {
		return "", fmt.Errorf("upload frontend CDN manifest: %w", err)
	}

	log.Printf("Frontend CDN assets synced to MinIO prefix %s (%d files, %d bytes)", prefix, uploadedFiles, uploadedBytes)
	return miniohelper.GetFileURL(prefix), nil
}

func hasFrontendManifestChanged(stored, current *frontendCDNManifest) bool {
	if current == nil {
		return false
	}
	if stored == nil {
		return true
	}
	if stored.Checksum != "" && current.Checksum != "" && stored.Checksum == current.Checksum {
		return false
	}

	storedMap := buildFrontendManifestVersionMap(stored)
	currentMap := buildFrontendManifestVersionMap(current)
	if len(storedMap) != len(currentMap) {
		return true
	}
	for filePath, currentEntry := range currentMap {
		storedEntry, ok := storedMap[filePath]
		if !ok || storedEntry != currentEntry {
			return true
		}
	}

	return false
}

func buildFrontendManifestVersionMap(manifest *frontendCDNManifest) map[string]frontendCDNVersionEntry {
	if manifest == nil {
		return nil
	}

	versionMap := make(map[string]frontendCDNVersionEntry, len(manifest.Files))
	for _, file := range manifest.Files {
		versionMap[file.Path] = frontendCDNVersionEntry{
			Size:        file.Size,
			ContentType: file.ContentType,
			Digest:      file.Digest,
		}
	}
	return versionMap
}

func loadEmbeddedFrontendAssets(webStaticFS fs.FS) ([]embeddedFrontendAsset, *frontendCDNManifest, error) {
	assets := make([]embeddedFrontendAsset, 0, 16)
	manifest := &frontendCDNManifest{
		Files: make([]frontendCDNManifestFile, 0, 16),
	}
	checksumHasher := sha256.New()

	err := fs.WalkDir(webStaticFS, ".", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filePath == frontendIndexFile {
			return nil
		}

		data, err := fs.ReadFile(webStaticFS, filePath)
		if err != nil {
			return fmt.Errorf("read embedded file %s: %w", filePath, err)
		}

		digestBytes := sha256.Sum256(data)
		digest := hex.EncodeToString(digestBytes[:])
		contentType := detectFrontendContentType(filePath, data)

		assets = append(assets, embeddedFrontendAsset{
			Path:        filePath,
			Data:        data,
			ContentType: contentType,
			Digest:      digest,
		})
		manifest.Files = append(manifest.Files, frontendCDNManifestFile{
			Path:        filePath,
			Size:        int64(len(data)),
			ContentType: contentType,
			Digest:      digest,
		})

		checksumHasher.Write([]byte(filePath))
		checksumHasher.Write([]byte{0})
		checksumHasher.Write([]byte(digest))
		checksumHasher.Write([]byte{0})

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	manifest.Checksum = hex.EncodeToString(checksumHasher.Sum(nil))
	return assets, manifest, nil
}

func readFrontendManifest(ctx context.Context, client *minioapi.Client, bucket, objectName string) (*frontendCDNManifest, error) {
	object, err := client.GetObject(ctx, bucket, objectName, minioapi.GetObjectOptions{})
	if err != nil {
		resp := minioapi.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NotFound" {
			return nil, nil
		}
		return nil, err
	}
	defer object.Close()

	data, err := io.ReadAll(object)
	if err != nil {
		resp := minioapi.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NotFound" {
			return nil, nil
		}
		return nil, err
	}

	manifest := &frontendCDNManifest{}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func purgeFrontendPrefix(ctx context.Context, client *minioapi.Client, bucket, prefix string) error {
	objectPrefix := normalizeObjectPrefix(prefix)
	if objectPrefix == "" {
		return fmt.Errorf("frontend CDN object prefix is empty")
	}

	return purgeFrontendObjects(ctx, client, bucket, objectPrefix+"/", false)
}

func deleteObjectIfExists(ctx context.Context, client *minioapi.Client, bucket, objectName string) error {
	if objectName == "" {
		return nil
	}

	return purgeFrontendObjects(ctx, client, bucket, objectName, true)
}

func purgeFrontendObjects(ctx context.Context, client *minioapi.Client, bucket, prefix string, exactMatch bool) error {
	objectsCh := make(chan minioapi.ObjectInfo)
	go func() {
		defer close(objectsCh)

		for objectInfo := range client.ListObjects(ctx, bucket, minioapi.ListObjectsOptions{
			Prefix:       prefix,
			Recursive:    true,
			WithVersions: true,
		}) {
			if objectInfo.Err != nil {
				objectsCh <- minioapi.ObjectInfo{Err: objectInfo.Err}
				return
			}
			if exactMatch && objectInfo.Key != prefix {
				continue
			}
			objectsCh <- minioapi.ObjectInfo{
				Key:       objectInfo.Key,
				VersionID: objectInfo.VersionID,
			}
		}
	}()

	for removeErr := range client.RemoveObjects(ctx, bucket, objectsCh, minioapi.RemoveObjectsOptions{}) {
		if removeErr.Err != nil {
			resp := minioapi.ToErrorResponse(removeErr.Err)
			if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NotFound" {
				continue
			}
			return removeErr.Err
		}
	}

	return nil
}

func detectFrontendContentType(filePath string, data []byte) string {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	}

	if contentType := mime.TypeByExtension(ext); contentType != "" {
		return contentType
	}
	return http.DetectContentType(data)
}

func joinObjectPath(prefix, filePath string) string {
	cleanPrefix := normalizeObjectPrefix(prefix)
	cleanPath := strings.TrimPrefix(path.Clean(filePath), "./")
	if cleanPrefix == "" {
		return cleanPath
	}
	return strings.TrimPrefix(path.Join(cleanPrefix, cleanPath), "/")
}

func normalizeObjectPrefix(prefix string) string {
	clean := strings.TrimSpace(prefix)
	clean = strings.ReplaceAll(clean, "\\", "/")
	clean = strings.Trim(clean, "/")
	if clean == "." {
		return ""
	}
	return clean
}

func normalizeAssetBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasSuffix(trimmed, "/") {
		trimmed += "/"
	}
	return trimmed
}
