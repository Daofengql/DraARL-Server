package handler

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"draarl/internal/config"
	gormdb "draarl/internal/gormdb"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

type presignPutRequest struct {
	FileType    string `json:"file_type" binding:"required"`
	FileName    string `json:"file_name" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
	ContentType string `json:"content_type"`
}

// PresignPut 生成直传上传凭证。
// POST /api/storage/presign-put
func PresignPut(c *gin.Context) {
	if !storage.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "存储服务不可用"})
		return
	}

	var req presignPutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}

	fileType := strings.ToLower(strings.TrimSpace(req.FileType))
	if !storage.IsAllowedPresignFileType(fileType) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "不支持的 file_type"})
		return
	}
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}
	user, err := gormdb.NewUserRepository().GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "用户不存在"})
		return
	}
	if (fileType == "assets" || fileType == "firmware" || fileType == "client_package") && !hasRoleGORM(user, "admin") {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "该文件类型需要管理员权限"})
		return
	}

	maxSize := storage.MaxSizeForFileType(fileType)
	if req.Size <= 0 || req.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("文件大小无效或超过限制（最大 %d 字节）", maxSize),
		})
		return
	}

	ext := storage.ExtFromFilename(req.FileName)
	contentType := req.ContentType
	if contentType == "" {
		contentType = storage.GuessContentType(ext, "application/octet-stream")
	}
	objectKey := storage.NewStagingObjectKey(fileType, user.ID, ext)
	expiry := 15 * time.Minute
	uploadToken, err := storage.CreateUploadGrant(objectKey, fileType, user.ID, req.Size, contentType, expiry)
	if err != nil {
		log.Printf("生成上传授权失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成上传授权失败"})
		return
	}

	result, err := storage.PresignPut(c.Request.Context(), objectKey, expiry, contentType, req.Size)
	if err != nil {
		log.Printf("生成预签名上传失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成上传凭证失败"})
		return
	}

	if result.Mode == storage.ModeLocal && strings.HasPrefix(result.UploadURL, "/") {
		result.UploadURL = publicAPIBase(c) + result.UploadURL
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"mode":         result.Mode,
			"upload_url":   result.UploadURL,
			"method":       result.Method,
			"headers":      result.Headers,
			"object_key":   result.ObjectKey,
			"expires_at":   result.ExpiresAt.Format(time.RFC3339),
			"max_size":     maxSize,
			"content_type": contentType,
			"upload_token": uploadToken,
		},
	})
}

// StorageDirectPut local 驱动直传入口。
// PUT /api/storage/put?token=...&key=...
func StorageDirectPut(c *gin.Context) {
	if !storage.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "存储服务不可用"})
		return
	}
	if storage.ResolveDriver(config.Get()) != storage.DriverLocal {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "当前存储驱动不支持此接口"})
		return
	}

	token := c.Query("token")
	key := c.Query("key")
	contentType := c.Query("content_type")
	if contentType == "" {
		contentType = c.GetHeader("Content-Type")
	}
	if token == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 token 或 key"})
		return
	}
	expectedSize, err := storage.VerifyLocalPutToken(token, key, contentType)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": err.Error()})
		return
	}
	if c.Request.ContentLength >= 0 && c.Request.ContentLength != expectedSize {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "上传内容大小与凭证不匹配"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, expectedSize)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if err := storage.Put(c.Request.Context(), key, c.Request.Body, expectedSize, contentType); err != nil {
		log.Printf("local 直传失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "上传内容与凭证不匹配"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "上传成功",
		"data": gin.H{
			"object_key": key,
			"size":       expectedSize,
		},
	})
}

// StorageDirectGet serves a short-lived local-storage download URL.
// GET /api/storage/get?token=...&key=...
func StorageDirectGet(c *gin.Context) {
	if !storage.IsEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "存储服务不可用"})
		return
	}
	if storage.ResolveDriver(config.Get()) != storage.DriverLocal {
		c.Status(http.StatusNotFound)
		return
	}
	token := c.Query("token")
	key := strings.TrimLeft(c.Query("key"), "/")
	if token == "" || key == "" || strings.ContainsRune(key, 0) || strings.Contains(key, "..") {
		c.Status(http.StatusBadRequest)
		return
	}
	if err := storage.VerifyLocalGetToken(token, key); err != nil {
		c.Status(http.StatusForbidden)
		return
	}
	fullPath, err := storage.AbsoluteLocalPath(key)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if strings.HasPrefix(key, "client-releases/") || strings.HasPrefix(key, "uploads/firmware/") {
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(key)}))
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(fullPath)
}

// ServeLocalFile 提供 local 存储文件访问。
// GET /files/*key
func ServeLocalFile(c *gin.Context) {
	if storage.ResolveDriver(config.Get()) != storage.DriverLocal {
		c.Status(http.StatusNotFound)
		return
	}
	key := strings.TrimPrefix(c.Param("key"), "/")
	if key == "" || strings.ContainsRune(key, 0) {
		c.Status(http.StatusNotFound)
		return
	}
	// 先拒绝显式 .. 段，再 Clean
	if strings.Contains(key, "..") {
		c.Status(http.StatusBadRequest)
		return
	}
	key = path.Clean("/" + key)
	key = strings.TrimPrefix(key, "/")
	if key == "." || key == "" || strings.Contains(key, "..") {
		c.Status(http.StatusBadRequest)
		return
	}
	if !storage.IsLocalPublicObjectKey(key) {
		c.Status(http.StatusNotFound)
		return
	}

	fullPath, err := storage.AbsoluteLocalPath(key)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'")
	c.File(fullPath)
}

func publicAPIBase(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
