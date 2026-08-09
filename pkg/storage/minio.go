package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"draarl/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioStorage struct {
	client            *minio.Client
	publicClient      *minio.Client
	bucket            string
	downloadURLPrefix string
	driver            string
}

// s3Storage is the generic S3-compatible implementation. minioStorage remains
// an alias to preserve the old helper API while R2, COS, and OSS use the same
// implementation and object lifecycle.
type s3Storage = minioStorage

func newMinIOStorage(cfg *config.Configuration) (Storage, error) {
	mc := cfg.Storage.MinIO
	publicEndpoint, publicUseSSL := resolveMinIOPublicTarget(mc)
	return newS3StorageWithConfig(config.S3Config{
		Provider:          "minio",
		Endpoint:          mc.Endpoint,
		PresignEndpoint:   publicEndpoint,
		DownloadURLPrefix: firstNonEmpty(mc.DownloadURLPrefix, mc.BasePath),
		AccessKey:         mc.AccessKey,
		SecretKey:         mc.SecretKey,
		UseSSL:            mc.UseSSL,
		PresignUseSSL:     publicUseSSL,
		Bucket:            mc.Bucket,
		AutoCreateBucket:  true,
	}, DriverMinIO)
}

func newS3Storage(cfg *config.Configuration) (Storage, error) {
	return newS3StorageWithConfig(cfg.Storage.S3, DriverS3)
}

func newS3StorageWithConfig(sc config.S3Config, driver string) (Storage, error) {
	sc, bucketLookup, err := prepareS3Config(sc, driver)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	client, err := newS3APIClient(sc, sc.Endpoint, sc.UseSSL, bucketLookup, transport)
	if err != nil {
		return nil, fmt.Errorf("初始化 S3 客户端失败: %w", err)
	}
	publicEndpoint, publicUseSSL := sc.PresignEndpoint, sc.PresignUseSSL
	publicClient := client
	if publicEndpoint != sc.Endpoint || publicUseSSL != sc.UseSSL {
		publicClient, err = newS3APIClient(sc, publicEndpoint, publicUseSSL, bucketLookup, nil)
		if err != nil {
			return nil, fmt.Errorf("初始化 S3 对外签名客户端失败: %w", err)
		}
	}

	bucket := strings.TrimSpace(sc.Bucket)
	if bucket == "" {
		bucket = "draarl"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("检查 bucket 失败: %w", err)
	}
	if !exists && !sc.AutoCreateBucket {
		return nil, fmt.Errorf("S3 bucket 不存在: %s（请在对象存储侧预创建，或仅对 MinIO 开启 AutoCreateBucket）", bucket)
	}
	if !exists {
		createCtx, createCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer createCancel()
		if err := client.MakeBucket(createCtx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("创建 bucket 失败: %w", err)
		}
	}
	// Bucket policies are managed by the object-storage provider. Runtime
	// credentials only need bucket and object access, not policy administration.
	return &minioStorage{
		client:            client,
		publicClient:      publicClient,
		bucket:            bucket,
		downloadURLPrefix: sc.DownloadURLPrefix,
		driver:            normalizeS3Driver(driver),
	}, nil
}

func prepareS3Config(sc config.S3Config, driver string) (config.S3Config, minio.BucketLookupType, error) {
	var err error
	if sc.AccessKey, err = resolveCredentialReference(sc.AccessKey, "AccessKey"); err != nil {
		return config.S3Config{}, minio.BucketLookupAuto, err
	}
	if sc.SecretKey, err = resolveCredentialReference(sc.SecretKey, "SecretKey"); err != nil {
		return config.S3Config{}, minio.BucketLookupAuto, err
	}
	if sc.SessionToken, err = resolveCredentialReference(sc.SessionToken, "SessionToken"); err != nil {
		return config.S3Config{}, minio.BucketLookupAuto, err
	}
	if sc.AccessKey == "" {
		return config.S3Config{}, minio.BucketLookupAuto, fmt.Errorf("S3 AccessKey 为空")
	}
	if sc.SecretKey == "" {
		return config.S3Config{}, minio.BucketLookupAuto, fmt.Errorf("S3 SecretKey 为空")
	}

	sc.Endpoint, sc.UseSSL = resolveS3Endpoint(sc.Endpoint, sc.UseSSL)
	if sc.Endpoint == "" {
		return config.S3Config{}, minio.BucketLookupAuto, fmt.Errorf("S3 Endpoint 为空")
	}
	if strings.TrimSpace(sc.PresignEndpoint) == "" {
		sc.PresignEndpoint, sc.PresignUseSSL = sc.Endpoint, sc.UseSSL
	} else {
		sc.PresignEndpoint, sc.PresignUseSSL = resolveS3Endpoint(sc.PresignEndpoint, sc.PresignUseSSL)
		if sc.PresignEndpoint == "" {
			return config.S3Config{}, minio.BucketLookupAuto, fmt.Errorf("S3 PresignEndpoint 为空")
		}
	}
	sc.DownloadURLPrefix = normalizeDownloadURLPrefix(firstNonEmpty(sc.DownloadURLPrefix, sc.PublicBaseURL))
	if err := validateDownloadURLPrefix(sc.DownloadURLPrefix); err != nil {
		return config.S3Config{}, minio.BucketLookupAuto, err
	}

	sc.Provider = effectiveS3Provider(sc.Provider, driver)
	sc.Region = strings.TrimSpace(sc.Region)
	switch sc.Provider {
	case "r2":
		if sc.Region == "" {
			sc.Region = "auto"
		}
	case "cos", "oss":
		if sc.Region == "" {
			return config.S3Config{}, minio.BucketLookupAuto, fmt.Errorf("S3 provider %s 必须配置 Region", sc.Provider)
		}
	}
	bucketLookup, err := resolveS3BucketLookup(sc, driver)
	if err != nil {
		return config.S3Config{}, minio.BucketLookupAuto, err
	}
	return sc, bucketLookup, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeDownloadURLPrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "/" {
		return value
	}
	return strings.TrimRight(value, "/")
}

func validateDownloadURLPrefix(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	prefix, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("S3 DownloadURLPrefix 无效: %w", err)
	}
	if prefix.RawQuery != "" || prefix.Fragment != "" || prefix.User != nil {
		return fmt.Errorf("S3 DownloadURLPrefix 不能包含用户信息、查询参数或 fragment")
	}
	if prefix.IsAbs() {
		if (prefix.Scheme != "http" && prefix.Scheme != "https") || prefix.Host == "" {
			return fmt.Errorf("S3 DownloadURLPrefix 必须使用 http 或 https")
		}
		return nil
	}
	if prefix.Host != "" || !strings.HasPrefix(prefix.Path, "/") {
		return fmt.Errorf("S3 DownloadURLPrefix 必须是 http(s) URL 或以 / 开头的站内路径")
	}
	return nil
}

func newS3APIClient(sc config.S3Config, endpoint string, useSSL bool, bucketLookup minio.BucketLookupType, transport http.RoundTripper) (*minio.Client, error) {
	options := &minio.Options{
		Creds:        credentials.NewStaticV4(sc.AccessKey, sc.SecretKey, sc.SessionToken),
		Secure:       useSSL,
		Region:       sc.Region,
		BucketLookup: bucketLookup,
	}
	if transport != nil {
		options.Transport = transport
	}
	return minio.New(endpoint, options)
}

// resolveCredentialReference expands exact ${ENV_NAME} values at driver
// construction time. It deliberately operates on the driver's config copy so
// later config saves retain references instead of writing secrets to disk.
func resolveCredentialReference(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return value, nil
	}
	name := strings.TrimSpace(value[2 : len(value)-1])
	if !validCredentialEnvName(name) {
		return "", fmt.Errorf("S3 %s 环境变量引用无效: %s", field, value)
	}
	resolved, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(resolved) == "" {
		return "", fmt.Errorf("S3 %s 引用的环境变量未设置或为空: %s", field, name)
	}
	return strings.TrimSpace(resolved), nil
}

func validCredentialEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, ch := range value {
		if ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}

func normalizeS3Endpoint(value string) string {
	endpoint, _ := resolveS3Endpoint(value, false)
	return endpoint
}

func resolveS3Endpoint(value string, useSSL bool) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", useSSL
	}
	value = strings.TrimRight(value, "/")
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "https://") {
		value, useSSL = value[len("https://"):], true
	} else if strings.HasPrefix(lower, "http://") {
		value, useSSL = value[len("http://"):], false
	}
	return strings.TrimRight(value, "/"), useSSL
}

func normalizeS3Driver(driver string) string {
	driver = strings.ToLower(strings.TrimSpace(driver))
	if driver == "" {
		return DriverS3
	}
	return driver
}

func resolveMinIOPublicTarget(config config.MinIOConfig) (string, bool) {
	if endpoint := strings.TrimSpace(config.PublicEndpoint); endpoint != "" {
		return endpoint, config.PublicUseSSL
	}
	return strings.TrimSpace(config.Endpoint), config.UseSSL
}

func resolveS3PresignTarget(sc config.S3Config) (string, bool) {
	if endpoint := strings.TrimSpace(sc.PresignEndpoint); endpoint != "" {
		return resolveS3Endpoint(endpoint, sc.PresignUseSSL)
	}
	return resolveS3Endpoint(sc.Endpoint, sc.UseSSL)
}

func parseBucketLookup(value string) (minio.BucketLookupType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "path":
		return minio.BucketLookupPath, nil
	case "dns", "virtual", "virtual-host":
		return minio.BucketLookupDNS, nil
	case "", "auto":
		return minio.BucketLookupAuto, nil
	}
	return minio.BucketLookupAuto, fmt.Errorf("S3 BucketLookup 无效: %s（只支持 auto、path 或 dns）", value)
}

func effectiveS3Provider(provider, driver string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		return provider
	}
	provider = strings.ToLower(strings.TrimSpace(driver))
	if provider == "" {
		return DriverS3
	}
	return provider
}

func resolveS3BucketLookup(sc config.S3Config, driver string) (minio.BucketLookupType, error) {
	if strings.TrimSpace(sc.BucketLookup) != "" {
		return parseBucketLookup(sc.BucketLookup)
	}
	provider := effectiveS3Provider(sc.Provider, driver)
	switch provider {
	case "r2", DriverMinIO:
		return minio.BucketLookupPath, nil
	case "cos", "oss":
		return minio.BucketLookupDNS, nil
	default:
		return minio.BucketLookupAuto, nil
	}
}

func (s *minioStorage) Driver() string          { return s.driver }
func (s *minioStorage) SupportsDirectPut() bool { return true }
func (s *minioStorage) Capabilities() Capabilities {
	return Capabilities{
		DirectPut: true, PresignGet: true, PresignPut: true, ServerCopy: true,
	}
}

func (s *minioStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, strings.TrimLeft(key, "/"), r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (s *minioStorage) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, strings.TrimLeft(key, "/"), minio.RemoveObjectOptions{})
}

func (s *minioStorage) Promote(ctx context.Context, stagedKey, finalKey string) error {
	stagedKey = strings.TrimLeft(stagedKey, "/")
	finalKey = strings.TrimLeft(finalKey, "/")
	if stagedKey == "" || finalKey == "" {
		return fmt.Errorf("staging and final keys are required")
	}
	if _, err := s.client.StatObject(ctx, s.bucket, finalKey, minio.StatObjectOptions{}); err == nil {
		return fmt.Errorf("%w: %s", ErrFinalObjectAlreadyExists, finalKey)
	} else {
		code := minio.ToErrorResponse(err).Code
		if code != "NoSuchKey" && code != "NoSuchObject" && code != "NotFound" {
			return fmt.Errorf("check final object: %w", err)
		}
	}
	_, err := s.client.CopyObject(ctx, minio.CopyDestOptions{
		Bucket: s.bucket,
		Object: finalKey,
	}, minio.CopySrcOptions{
		Bucket: s.bucket,
		Object: stagedKey,
	})
	if err != nil {
		return fmt.Errorf("copy staging object: %w", err)
	}
	if err := s.client.RemoveObject(ctx, s.bucket, stagedKey, minio.RemoveObjectOptions{}); err != nil {
		// A successful CopyObject has already committed the immutable final key.
		// Do not delete it to compensate for a staging-cleanup failure: that can
		// destroy a package that has already been recorded by the caller.
		log.Printf("[STORAGE] final object promoted but S3 staging cleanup failed key=%s err=%v", stagedKey, err)
	}
	return nil
}

func (s *minioStorage) CleanupStaging(ctx context.Context, olderThan time.Time) error {
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    "staging/",
		Recursive: true,
	}) {
		if object.Err != nil {
			return object.Err
		}
		if object.LastModified.Before(olderThan) {
			if err := s.client.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *minioStorage) Walk(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    strings.TrimLeft(prefix, "/"),
		Recursive: true,
	}) {
		if object.Err != nil {
			return object.Err
		}
		if strings.HasSuffix(object.Key, "/") {
			continue
		}
		if err := fn(ObjectInfo{Key: object.Key, Size: object.Size}); err != nil {
			return err
		}
	}
	return nil
}

func (s *minioStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, strings.TrimLeft(key, "/"), minio.GetObjectOptions{})
}

func (s *minioStorage) Stat(ctx context.Context, key string) (int64, string, error) {
	info, err := s.client.StatObject(ctx, s.bucket, strings.TrimLeft(key, "/"), minio.StatObjectOptions{})
	if err != nil {
		return 0, "", err
	}
	return info.Size, info.ContentType, nil
}

func (s *minioStorage) PublicURL(key string) string {
	return ""
}

func (s *minioStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	cleanKey := strings.TrimLeft(key, "/")
	u, err := s.publicClient.PresignedGetObject(ctx, s.bucket, cleanKey, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名下载 URL 失败: %w", err)
	}
	return rewritePresignedDownloadURL(u.String(), s.downloadURLPrefix, cleanKey)
}

func rewritePresignedDownloadURL(rawURL, prefix, objectKey string) (string, error) {
	prefix = normalizeDownloadURLPrefix(prefix)
	if prefix == "" {
		return rawURL, nil
	}
	if err := validateDownloadURLPrefix(prefix); err != nil {
		return "", err
	}

	signedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("解析 S3 预签名下载 URL 失败: %w", err)
	}
	prefixURL, err := url.Parse(prefix)
	if err != nil {
		return "", fmt.Errorf("解析 S3 DownloadURLPrefix 失败: %w", err)
	}

	escapedPath := strings.TrimRight(prefixURL.EscapedPath(), "/") + "/" + escapeObjectKeyForURL(objectKey)
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", fmt.Errorf("解析 S3 DownloadURLPrefix 路径失败: %w", err)
	}
	prefixURL.Path = decodedPath
	prefixURL.RawPath = escapedPath
	prefixURL.RawQuery = signedURL.RawQuery
	prefixURL.ForceQuery = signedURL.ForceQuery
	return prefixURL.String(), nil
}

func (s *minioStorage) PresignPut(ctx context.Context, key string, expiry time.Duration, contentType string, size int64) (PresignPutResult, error) {
	_ = size // S3 PUT URLs cannot enforce length; completion verifies the signed upload grant.
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	u, err := s.publicClient.PresignedPutObject(ctx, s.bucket, strings.TrimLeft(key, "/"), expiry)
	if err != nil {
		return PresignPutResult{}, fmt.Errorf("生成预签名上传 URL 失败: %w", err)
	}
	headers := map[string]string{}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	return PresignPutResult{
		Mode:      ModeS3,
		UploadURL: u.String(),
		Method:    "PUT",
		Headers:   headers,
		ObjectKey: strings.TrimLeft(key, "/"),
		ExpiresAt: time.Now().Add(expiry),
	}, nil
}

// MinIOClient exposes the underlying client for legacy integrations.
func MinIOClient() *minio.Client {
	s, ok := Get().(*minioStorage)
	if !ok || s == nil {
		return nil
	}
	return s.client
}

// MinIOBucket 返回当前 bucket。
func MinIOBucket() string {
	s, ok := Get().(*minioStorage)
	if !ok || s == nil {
		cfg := config.Get()
		if cfg.Storage.MinIO.Bucket != "" {
			return cfg.Storage.MinIO.Bucket
		}
		return "draarl"
	}
	return s.bucket
}

// BucketName returns the active S3 bucket when the current driver is an S3
// compatible implementation. It is a compatibility helper for legacy model
// fields that still store the bucket alongside an object key.
func BucketName() string {
	if s, ok := Get().(*minioStorage); ok && s != nil {
		return s.bucket
	}
	if cfg := config.TryGet(); cfg != nil {
		if _, profile, ok := cfg.ActiveStorageProfile(); ok {
			if profile.S3.Bucket != "" {
				return profile.S3.Bucket
			}
		}
		if cfg.Storage.S3.Bucket != "" {
			return cfg.Storage.S3.Bucket
		}
		if cfg.Storage.MinIO.Bucket != "" {
			return cfg.Storage.MinIO.Bucket
		}
	}
	return "draarl"
}
