package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"draarl/internal/config"
)

type localStorage struct {
	root    string
	baseURL string
	secret  string
}

func newLocalStorage(cfg *config.Configuration) (Storage, error) {
	return newLocalStorageWithConfig(cfg.Storage.Local, cfg.JWT.Secret)
}

func newLocalStorageWithConfig(local config.LocalStorageConfig, secret string) (Storage, error) {
	root := strings.TrimSpace(local.RootPath)
	if root == "" {
		root = "./data/storage"
	}
	if !filepath.IsAbs(root) && config.GetConfigPath() != "" {
		root = filepath.Join(filepath.Dir(config.GetConfigPath()), root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析本地存储路径失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地存储目录失败: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("解析本地存储真实路径失败: %w", err)
	}
	abs, err = filepath.Abs(realRoot)
	if err != nil {
		return nil, fmt.Errorf("解析本地存储真实路径失败: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(local.BaseURL), "/")
	if baseURL == "" {
		baseURL = "/files"
	}

	if secret == "" {
		return nil, fmt.Errorf("JWT Secret 为空，无法初始化本地存储签名")
	}

	return &localStorage{
		root:    abs,
		baseURL: baseURL,
		secret:  secret,
	}, nil
}

func (s *localStorage) Driver() string          { return DriverLocal }
func (s *localStorage) SupportsDirectPut() bool { return true }
func (s *localStorage) Capabilities() Capabilities {
	return Capabilities{
		DirectPut:  true,
		PresignGet: true,
		PresignPut: true,
		ServerCopy: true,
		PublicURL:  true,
	}
}

func (s *localStorage) resolvePath(key string) (string, error) {
	if key == "" || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("非法对象键")
	}
	// 统一为 / 分隔，去掉前导 /
	clean := strings.TrimLeft(strings.ReplaceAll(key, "\\", "/"), "/")
	if clean == "" || clean == "." {
		return "", fmt.Errorf("非法对象键")
	}
	// 拒绝任何 .. 段
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("非法对象键")
		}
	}
	// path.Clean 折叠冗余分隔；若结果仍逃逸则拒绝
	cleaned := path.Clean("/" + clean)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("非法对象键")
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", fmt.Errorf("非法对象键")
		}
	}

	full := filepath.Join(s.root, filepath.FromSlash(cleaned))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(s.root, absFull) {
		return "", fmt.Errorf("路径越界")
	}
	// Resolve the closest existing ancestor as well as an existing target. A
	// missing final file beneath a symlinked directory must not bypass the root
	// check merely because EvalSymlinks on the full path returns ENOENT.
	probe := absFull
	for {
		real, evalErr := filepath.EvalSymlinks(probe)
		if evalErr == nil {
			real, evalErr = filepath.Abs(real)
			if evalErr != nil {
				return "", evalErr
			}
			if !pathWithinRoot(s.root, real) {
				return "", fmt.Errorf("路径越界")
			}
			if probe == absFull {
				return real, nil
			}
			break
		}
		if !os.IsNotExist(evalErr) {
			return "", evalErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("路径越界")
		}
		probe = parent
	}
	return absFull, nil
}

func pathWithinRoot(root, candidate string) bool {
	return candidate == root || strings.HasPrefix(candidate, root+string(os.PathSeparator))
}

func (s *localStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_ = contentType
	filePath, err := s.resolvePath(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".draarl-upload-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	written, copyErr := io.Copy(tmp, &contextReader{ctx: ctx, reader: r})
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("写入文件失败: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}
	if size >= 0 && written != size {
		return fmt.Errorf("文件大小不匹配: got %d want %d", written, size)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("设置文件权限失败: %w", err)
	}
	// Staging uploads may be retried with the same signed URL.
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("替换旧文件失败: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("提交文件失败: %w", err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

func (s *localStorage) Delete(ctx context.Context, key string) error {
	_ = ctx
	path, err := s.resolvePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *localStorage) Promote(ctx context.Context, stagedKey, finalKey string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	source, err := s.resolvePath(stagedKey)
	if err != nil {
		return err
	}
	destination, err := s.resolvePath(finalKey)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("staging object does not exist: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("staging object is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	// Link creation is atomic and fails when destination already exists. Rename
	// would silently replace destination on both Linux and Windows, which breaks
	// the immutability guarantee for published artifacts. Source and destination
	// live beneath the same local-storage root, so a hard link is available.
	if err := os.Link(source, destination); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: %s", ErrFinalObjectAlreadyExists, finalKey)
		}
		return fmt.Errorf("promote staging object: %w", err)
	}
	// The final object is already atomically committed. Leave a duplicate
	// staging object for scheduled cleanup if removal is temporarily unavailable.
	if err := os.Remove(source); err != nil {
		log.Printf("[STORAGE] final object promoted but local staging cleanup failed key=%s err=%v", stagedKey, err)
	}
	return nil
}

func (s *localStorage) CleanupStaging(ctx context.Context, olderThan time.Time) error {
	stagingRoot, err := s.resolvePath("staging")
	if err != nil {
		return err
	}
	if _, err := os.Stat(stagingRoot); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(stagingRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(olderThan) {
			return os.Remove(filePath)
		}
		return nil
	})
}

func (s *localStorage) Walk(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	prefix = strings.TrimLeft(filepath.ToSlash(prefix), "/")
	base := s.root
	if prefix != "" {
		resolved, err := s.resolvePath(prefix)
		if err != nil {
			return err
		}
		base = resolved
	}
	if info, err := os.Stat(base); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		// prefix 指向单个文件
		rel, relErr := filepath.Rel(s.root, base)
		if relErr != nil {
			return relErr
		}
		return fn(ObjectInfo{Key: filepath.ToSlash(rel), Size: info.Size()})
	}
	return filepath.WalkDir(base, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		// 跳过写入过程中的临时文件
		if strings.HasPrefix(entry.Name(), ".draarl-upload-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, filePath)
		if err != nil {
			return err
		}
		return fn(ObjectInfo{Key: filepath.ToSlash(rel), Size: info.Size()})
	})
}

func (s *localStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	_ = ctx
	path, err := s.resolvePath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *localStorage) Stat(ctx context.Context, key string) (int64, string, error) {
	_ = ctx
	path, err := s.resolvePath(key)
	if err != nil {
		return 0, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, "", err
	}
	return info.Size(), GuessContentType(filepath.Ext(path), "application/octet-stream"), nil
}

func (s *localStorage) PublicURL(key string) string {
	clean := strings.TrimLeft(filepath.ToSlash(key), "/")
	if clean == "" || !IsLocalPublicObjectKey(clean) {
		return ""
	}
	return s.baseURL + "/" + escapeObjectKeyForURL(clean)
}

// IsLocalPublicObjectKey defines the narrow set of objects intentionally
// exposed through the permanent /files route. User documents, recordings,
// Firmware, client resources, and staging objects require signed GET URLs.
func IsLocalPublicObjectKey(key string) bool {
	clean := strings.TrimLeft(filepath.ToSlash(key), "/")
	if clean == "" {
		return false
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, prefix := range []string{
		"uploads/avatar/",
		"thumb/uploads/avatar/",
		"uploads/logo/",
		"uploads/favicon/",
		"frontend/",
	} {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}

func (s *localStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	_ = ctx
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	cleanKey := strings.TrimLeft(filepath.ToSlash(key), "/")
	if _, err := s.resolvePath(cleanKey); err != nil {
		return "", err
	}
	exp := time.Now().Add(expiry)
	token, err := s.signGetToken(cleanKey, exp)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("token", token)
	q.Set("key", cleanKey)
	return "/api/storage/get?" + q.Encode(), nil
}

func (s *localStorage) PresignPut(ctx context.Context, key string, expiry time.Duration, contentType string, size int64) (PresignPutResult, error) {
	_ = ctx
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	exp := time.Now().Add(expiry)
	token, err := s.signPutToken(key, exp, contentType, size)
	if err != nil {
		return PresignPutResult{}, err
	}
	q := url.Values{}
	q.Set("token", token)
	q.Set("key", key)
	if contentType != "" {
		q.Set("content_type", contentType)
	}
	return PresignPutResult{
		Mode:      ModeLocal,
		UploadURL: "/api/storage/put?" + q.Encode(),
		Method:    "PUT",
		Headers: map[string]string{
			"Content-Type": contentType,
		},
		ObjectKey: key,
		ExpiresAt: exp,
	}, nil
}

type localPutGrant struct {
	ObjectKey   string `json:"object_key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	ExpiresAt   int64  `json:"expires_at"`
}

type localGetGrant struct {
	ObjectKey string `json:"object_key"`
	ExpiresAt int64  `json:"expires_at"`
}

func (s *localStorage) signPutToken(key string, exp time.Time, contentType string, size int64) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("invalid upload size")
	}
	return signToken(s.secret, localPutGrant{
		ObjectKey:   key,
		Size:        size,
		ContentType: contentType,
		ExpiresAt:   exp.Unix(),
	})
}

func (s *localStorage) signGetToken(key string, exp time.Time) (string, error) {
	return signToken(s.secret, localGetGrant{
		ObjectKey: key,
		ExpiresAt: exp.Unix(),
	})
}

// VerifyLocalPutToken 校验 local 直传 token。
func VerifyLocalPutToken(token, key, contentType string) (int64, error) {
	cfg := config.Get()
	secret := cfg.JWT.Secret
	if secret == "" {
		return 0, fmt.Errorf("storage signing secret is empty")
	}
	var grant localPutGrant
	if err := verifyToken(secret, token, &grant); err != nil {
		return 0, fmt.Errorf("无效 token")
	}
	if grant.ObjectKey != key {
		return 0, fmt.Errorf("token 与 key 不匹配")
	}
	if contentType != "" && grant.ContentType != "" && grant.ContentType != contentType {
		return 0, fmt.Errorf("content-type 不匹配")
	}
	if time.Now().Unix() > grant.ExpiresAt {
		return 0, fmt.Errorf("token 已过期")
	}
	if grant.Size <= 0 {
		return 0, fmt.Errorf("token 文件大小无效")
	}
	return grant.Size, nil
}

// VerifyLocalGetToken validates a short-lived local download URL. Unlike the
// public /files compatibility endpoint it can safely serve private release
// artifacts without exposing a permanent object URL.
func VerifyLocalGetToken(token, key string) error {
	cfg := config.Get()
	if cfg.JWT.Secret == "" {
		return fmt.Errorf("storage signing secret is empty")
	}
	var grant localGetGrant
	if err := verifyToken(cfg.JWT.Secret, token, &grant); err != nil {
		return fmt.Errorf("无效 token")
	}
	if grant.ObjectKey != strings.TrimLeft(filepath.ToSlash(key), "/") {
		return fmt.Errorf("token 与 key 不匹配")
	}
	if grant.ExpiresAt <= 0 || time.Now().Unix() > grant.ExpiresAt {
		return fmt.Errorf("token 已过期")
	}
	return nil
}

// AbsoluteLocalPath 供文件服务使用。
func AbsoluteLocalPath(key string) (string, error) {
	s, ok := Get().(*localStorage)
	if !ok || s == nil {
		return "", fmt.Errorf("当前不是 local 存储")
	}
	return s.resolvePath(key)
}
