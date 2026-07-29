package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"

	"github.com/google/uuid"
)

// ErrFinalObjectAlreadyExists is returned when Promote is asked to write an
// immutable final key that has already been committed. Callers may safely
// treat it as a conflict and must never overwrite the existing object.
var ErrFinalObjectAlreadyExists = errors.New("final object already exists")

const (
	DriverMinIO = "minio"
	DriverS3    = "s3"
	DriverLocal = "local"

	ModeS3    = "s3"
	ModeLocal = "local"

	// DefaultReadURLExpiry is used by compatibility response fields that do
	// not carry an explicit URL expiry. Long-lived pages should refresh their
	// API data instead of persisting a signed object URL.
	DefaultReadURLExpiry = time.Hour
)

// 直传业务类型：无需服务端图像处理的大文件。
// 头像/Logo/Favicon 仍走 multipart 代理（需服务端处理）。
var presignFileTypes = map[string]struct{}{
	"assets":         {},
	"client_package": {},
	"firmware":       {},
	"operator_cert":  {},
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

// Capabilities describe the optional operations exposed by a storage driver.
// All built-in drivers support the complete release-upload workflow, but
// callers can inspect this instead of relying on driver-name checks.
type Capabilities struct {
	DirectPut  bool `json:"direct_put"`
	PresignGet bool `json:"presign_get"`
	PresignPut bool `json:"presign_put"`
	ServerCopy bool `json:"server_copy"`
	PublicURL  bool `json:"public_url"`
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
	Capabilities() Capabilities
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
	if profileName, profile, ok := cfg.ActiveStorageProfile(); ok {
		driver = profile.Driver
		if driver == "" {
			driver = DriverLocal
		}
		log.Printf("[STORAGE] 启动 profile=%s driver=%s", profileName, driver)
	}
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

// ResolveDriver 解析旧版存储驱动名。启用 ActiveProfile 时，调用方应通过
// NewDriver/Init 解析 profile；这里保留用于旧配置和兼容调用。
func ResolveDriver(cfg *config.Configuration) string {
	if cfg == nil {
		return DriverLocal
	}
	if _, profile, ok := cfg.ActiveStorageProfile(); ok {
		driver := strings.ToLower(strings.TrimSpace(profile.Driver))
		if driver == "" {
			return DriverLocal
		}
		return driver
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Storage.Driver))
	if driver != "" {
		return driver
	}
	if strings.TrimSpace(cfg.Storage.MinIO.Endpoint) != "" {
		return DriverMinIO
	}
	if strings.TrimSpace(cfg.Storage.S3.Endpoint) != "" {
		return DriverS3
	}
	return DriverLocal
}

// IsAllowedPresignFileType 是否为允许直传的业务类型。
func IsAllowedPresignFileType(fileType string) bool {
	_, ok := presignFileTypes[strings.ToLower(strings.TrimSpace(fileType))]
	return ok
}

// ShouldPresignUpload 是否对指定业务类型使用直传。
// 固定策略：assets / client_package / firmware / operator_cert 直传；头像/Logo 等走代理。
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

// escapeObjectKeyForURL encodes each object-key segment while preserving the
// slash hierarchy. Public/CDN URLs must not let spaces, fragments, or query
// delimiters in legacy object keys alter the requested object.
func escapeObjectKeyForURL(key string) string {
	segments := strings.Split(strings.TrimLeft(key, "/"), "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return strings.Join(segments, "/")
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
	if !s.Capabilities().PublicURL {
		return ""
	}
	return s.PublicURL(key)
}

// ReadURL returns a browser-readable object URL without assuming the active
// bucket is public. Public/CDN storage uses its stable URL; private storage
// falls back to a short-lived signed GET URL.
func ReadURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return key, nil
	}
	s := Get()
	if s == nil {
		return "", fmt.Errorf("存储未初始化")
	}
	if s.Capabilities().PublicURL {
		if publicURL := strings.TrimSpace(s.PublicURL(key)); publicURL != "" {
			return publicURL, nil
		}
	}
	if !s.Capabilities().PresignGet {
		return "", fmt.Errorf("当前存储不支持可读 URL")
	}
	if expiry <= 0 {
		expiry = DefaultReadURLExpiry
	}
	return s.PresignGet(ctx, key, expiry)
}

func readURLOrEmpty(key string) string {
	url, err := ReadURL(context.Background(), key, DefaultReadURLExpiry)
	if err != nil {
		log.Printf("[STORAGE] 生成对象读取 URL 失败 key=%s err=%v", key, err)
		return ""
	}
	return url
}

// ResolveAssetURL 解析可能是完整 URL 或 object key 的值。
func ResolveAssetURL(value string) string {
	return readURLOrEmpty(value)
}

// GetAvatarURL 头像完整 URL。
func GetAvatarURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	if strings.HasPrefix(avatarPath, "http://") || strings.HasPrefix(avatarPath, "https://") {
		return avatarPath
	}
	return readURLOrEmpty("uploads/avatar/" + strings.TrimLeft(avatarPath, "/"))
}

// GetAvatarThumbURL 头像缩略图 URL。
func GetAvatarThumbURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	return readURLOrEmpty("thumb/uploads/avatar/" + strings.TrimLeft(avatarPath, "/"))
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

// CurrentCapabilities returns the active storage driver's capabilities.
func CurrentCapabilities() Capabilities {
	if s := Get(); s != nil {
		return s.Capabilities()
	}
	return Capabilities{}
}

// LocalRootPath 返回 local 驱动根路径（非 local 返回空）。
func LocalRootPath(cfg *config.Configuration) string {
	if cfg == nil {
		return ""
	}
	if ResolveDriver(cfg) != DriverLocal {
		return ""
	}
	local := cfg.Storage.Local
	if _, profile, ok := cfg.ActiveStorageProfile(); ok {
		local = profile.Local
	}
	root := strings.TrimSpace(local.RootPath)
	if root == "" {
		return "./data/storage"
	}
	return root
}
