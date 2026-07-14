package storage

import (
	"context"
	"fmt"
	"io"
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
	root := strings.TrimSpace(cfg.Storage.Local.RootPath)
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

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Storage.Local.BaseURL), "/")
	if baseURL == "" {
		baseURL = "/files"
	}

	secret := cfg.JWT.Secret
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
	root := s.root
	if absFull != root && !strings.HasPrefix(absFull, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径越界")
	}
	// 已存在路径再解析符号链接，防止 root 内 symlink 指向外部
	if real, err := filepath.EvalSymlinks(absFull); err == nil {
		if real != root && !strings.HasPrefix(real, root+string(os.PathSeparator)) {
			return "", fmt.Errorf("路径越界")
		}
		return real, nil
	}
	return absFull, nil
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
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("final object already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("promote staging object: %w", err)
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
	if clean == "" {
		return ""
	}
	return s.baseURL + "/" + clean
}

func (s *localStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	_ = ctx
	// local 下公开路径即可；若需鉴权可扩展 token，首版与公开桶对齐
	_ = expiry
	return s.PublicURL(key), nil
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

// AbsoluteLocalPath 供文件服务使用。
func AbsoluteLocalPath(key string) (string, error) {
	s, ok := Get().(*localStorage)
	if !ok || s == nil {
		return "", fmt.Errorf("当前不是 local 存储")
	}
	return s.resolvePath(key)
}
