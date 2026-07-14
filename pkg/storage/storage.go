package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"

	"github.com/google/uuid"
)

const (
	DriverMinIO = "minio"
	DriverLocal = "local"

	ModeS3    = "s3"
	ModeLocal = "local"
)

// 直传业务类型：无需服务端图像处理的大文件。
// 头像/Logo/Favicon 仍走 multipart 代理（需服务端处理）。
var presignFileTypes = map[string]struct{}{
	"assets":        {},
	"firmware":      {},
	"operator_cert": {},
}

// PresignPutResult 预签名上传结果。
// Mode: s3（MinIO/OSS/COS/R2 等 S3 兼容）或 local（后端 token PUT）。
type PresignPutResult struct {
	Mode      string            `json:"mode"` // s3 | local
	UploadURL string            `json:"upload_url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers,omitempty"`
	ObjectKey string            `json:"object_key"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// Storage 对象存储抽象。
// 新增云厂商（阿里云 OSS / 腾讯云 COS / Cloudflare R2 等）时：
//  1. 实现本接口
//  2. 在 init 中 RegisterDriver("oss"|"cos"|"r2", factory)
//
// 业务层只依赖本接口，不感知底层 SDK。
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	Promote(ctx context.Context, stagedKey, finalKey string) error
	CleanupStaging(ctx context.Context, olderThan time.Time) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (size int64, contentType string, err error)
	// Walk 遍历指定前缀下的对象，用于跨驱动迁移与运维。prefix 为空表示全部。
	Walk(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
	PublicURL(key string) string
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
	PresignPut(ctx context.Context, key string, expiry time.Duration, contentType string, size int64) (PresignPutResult, error)
	Driver() string
	SupportsDirectPut() bool
}

// ObjectInfo 描述一个存储对象（Walk 回调）。
type ObjectInfo struct {
	Key  string
	Size int64
}

var (
	mu          sync.RWMutex
	current     Storage
	cleanupOnce sync.Once
)

// Init 根据配置初始化存储驱动（经驱动注册表，便于扩展 OSS/COS/R2）。
func Init(cfg *config.Configuration) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	store, err := createDriver(cfg)
	if err != nil {
		return err
	}

	mu.Lock()
	current = store
	mu.Unlock()

	log.Printf("[STORAGE] 已启用驱动: %s (registered=%v)", store.Driver(), KnownDrivers())
	cleanupOnce.Do(startStagingCleanup)
	return nil
}

func startStagingCleanup() {
	go func() {
		cleanup := func() {
			store := Get()
			if store == nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := store.CleanupStaging(ctx, time.Now().Add(-24*time.Hour)); err != nil {
				log.Printf("[STORAGE] 清理过期 staging 对象失败: %v", err)
			}
		}

		cleanup()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanup()
		}
	}()
}

// StartInitInBackground 后台初始化（主要用于 minio 连接重试）。
func StartInitInBackground(cfg *config.Configuration) {
	if cfg == nil {
		cfg = config.Get()
	}
	driver := ResolveDriver(cfg)
	if driver == DriverLocal {
		if err := Init(cfg); err != nil {
			log.Printf("[STORAGE] local 初始化失败: %v", err)
		}
		return
	}

	go func() {
		for {
			if err := Init(cfg); err != nil {
				log.Printf("[STORAGE] 驱动 %s 初始化失败，30s 后重试: %v", driver, err)
				time.Sleep(30 * time.Second)
				continue
			}
			return
		}
	}()
}

// Get 返回当前存储实例（可能为 nil）。
func Get() Storage {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// IsEnabled 存储是否可用。
func IsEnabled() bool {
	return Get() != nil
}

// ResolveDriver 解析存储驱动名。
// 显式 Driver 优先；为空时：有 MinIO.Endpoint → minio，否则 local。
// 自定义驱动（oss/cos/r2）须在配置中显式写 Driver，并已 RegisterDriver。
func ResolveDriver(cfg *config.Configuration) string {
	if cfg == nil {
		return DriverLocal
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Storage.Driver))
	if driver != "" {
		return driver
	}
	if strings.TrimSpace(cfg.Storage.MinIO.Endpoint) != "" {
		return DriverMinIO
	}
	return DriverLocal
}

// IsAllowedPresignFileType 是否为允许直传的业务类型。
func IsAllowedPresignFileType(fileType string) bool {
	_, ok := presignFileTypes[strings.ToLower(strings.TrimSpace(fileType))]
	return ok
}

// ShouldPresignUpload 是否对指定业务类型使用直传。
// 固定策略：assets / firmware / operator_cert 直传；头像/Logo 等走代理。
func ShouldPresignUpload(fileType string) bool {
	return IsAllowedPresignFileType(fileType)
}

// NewObjectKey 生成对象键: uploads/{fileType}/{year}/{month}/{uuid}{ext}
func NewObjectKey(fileType, ext string) string {
	if fileType == "" {
		fileType = "other"
	}
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	now := time.Now()
	return fmt.Sprintf("uploads/%s/%d/%02d/%s%s", fileType, now.Year(), int(now.Month()), uuid.New().String(), strings.ToLower(ext))
}

// NewStagingObjectKey creates a user-scoped key that is never exposed as a public object.
func NewStagingObjectKey(fileType string, userID int, ext string) string {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	now := time.Now()
	return fmt.Sprintf("staging/%s/%d/%d/%02d/%s%s",
		strings.ToLower(strings.TrimSpace(fileType)), userID, now.Year(), int(now.Month()), uuid.New().String(), strings.ToLower(ext))
}

func IsStagingObjectKey(key, fileType string, userID int) bool {
	prefix := fmt.Sprintf("staging/%s/%d/", strings.ToLower(strings.TrimSpace(fileType)), userID)
	return strings.HasPrefix(strings.TrimLeft(key, "/"), prefix)
}

// ExtFromFilename 提取扩展名。
func ExtFromFilename(name string) string {
	return strings.ToLower(filepath.Ext(name))
}

// GuessContentType 根据扩展名推测 Content-Type。
func GuessContentType(ext, fallback string) string {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") && ext != "" {
		ext = "." + ext
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".bin", ".raw":
		return "application/octet-stream"
	default:
		if fallback != "" {
			return fallback
		}
		return "application/octet-stream"
	}
}

// PublicURL 便捷方法。
func PublicURL(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	// 已是绝对 URL（历史 logo/favicon）直接返回
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return key
	}
	s := Get()
	if s == nil {
		return ""
	}
	return s.PublicURL(key)
}

// ResolveAssetURL 解析可能是完整 URL 或 object key 的值。
func ResolveAssetURL(value string) string {
	return PublicURL(value)
}

// GetAvatarURL 头像完整 URL。
func GetAvatarURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	if strings.HasPrefix(avatarPath, "http://") || strings.HasPrefix(avatarPath, "https://") {
		return avatarPath
	}
	return PublicURL("uploads/avatar/" + strings.TrimLeft(avatarPath, "/"))
}

// GetAvatarThumbURL 头像缩略图 URL。
func GetAvatarThumbURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	return PublicURL("thumb/uploads/avatar/" + strings.TrimLeft(avatarPath, "/"))
}

// Put 便捷方法。
func Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	s := Get()
	if s == nil {
		return fmt.Errorf("存储未初始化")
	}
	return s.Put(ctx, key, r, size, contentType)
}

// Delete 便捷方法。
func Delete(ctx context.Context, key string) error {
	s := Get()
	if s == nil {
		return fmt.Errorf("存储未初始化")
	}
	return s.Delete(ctx, key)
}

// Promote moves a validated staging object to a new public object key.
func Promote(ctx context.Context, stagedKey, finalKey string) error {
	s := Get()
	if s == nil {
		return fmt.Errorf("存储未初始化")
	}
	return s.Promote(ctx, stagedKey, finalKey)
}

// Open 便捷方法。
func Open(ctx context.Context, key string) (io.ReadCloser, error) {
	s := Get()
	if s == nil {
		return nil, fmt.Errorf("存储未初始化")
	}
	return s.Open(ctx, key)
}

// Stat 便捷方法。
func Stat(ctx context.Context, key string) (int64, string, error) {
	s := Get()
	if s == nil {
		return 0, "", fmt.Errorf("存储未初始化")
	}
	return s.Stat(ctx, key)
}

// PresignGet 便捷方法。
func PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	s := Get()
	if s == nil {
		return "", fmt.Errorf("存储未初始化")
	}
	return s.PresignGet(ctx, key, expiry)
}

// PresignPut 便捷方法。
func PresignPut(ctx context.Context, key string, expiry time.Duration, contentType string, size int64) (PresignPutResult, error) {
	s := Get()
	if s == nil {
		return PresignPutResult{}, fmt.Errorf("存储未初始化")
	}
	return s.PresignPut(ctx, key, expiry, contentType, size)
}

// LocalRootPath 返回 local 驱动根路径（非 local 返回空）。
func LocalRootPath(cfg *config.Configuration) string {
	if cfg == nil {
		return ""
	}
	if ResolveDriver(cfg) != DriverLocal {
		return ""
	}
	root := strings.TrimSpace(cfg.Storage.Local.RootPath)
	if root == "" {
		return "./data/storage"
	}
	return root
}
