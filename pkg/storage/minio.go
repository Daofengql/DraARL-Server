package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"draarl/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type minioStorage struct {
	client   *minio.Client
	bucket   string
	basePath string
	endpoint string
	useSSL   bool
}

func newMinIOStorage(cfg *config.Configuration) (Storage, error) {
	mc := cfg.Storage.MinIO
	endpoint := strings.TrimSpace(mc.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("MinIO Endpoint 为空")
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

	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(mc.AccessKey, mc.SecretKey, ""),
		Secure:    mc.UseSSL,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 MinIO 客户端失败: %w", err)
	}

	bucket := mc.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("检查 bucket 失败: %w", err)
	}
	if !exists {
		createCtx, createCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer createCancel()
		if err := client.MakeBucket(createCtx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("创建 bucket 失败: %w", err)
		}
	}

	return &minioStorage{
		client:   client,
		bucket:   bucket,
		basePath: strings.TrimRight(strings.TrimSpace(mc.BasePath), "/"),
		endpoint: endpoint,
		useSSL:   mc.UseSSL,
	}, nil
}

func (s *minioStorage) Driver() string          { return DriverMinIO }
func (s *minioStorage) SupportsDirectPut() bool { return true }

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
		return fmt.Errorf("final object already exists")
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
		_ = s.client.RemoveObject(ctx, s.bucket, finalKey, minio.RemoveObjectOptions{})
		return fmt.Errorf("delete staging object: %w", err)
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
	clean := strings.TrimLeft(key, "/")
	if clean == "" {
		return ""
	}
	if s.basePath != "" {
		return s.basePath + "/" + clean
	}
	protocol := "http"
	if s.useSSL {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", protocol, s.endpoint, s.bucket, clean)
}

func (s *minioStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, strings.TrimLeft(key, "/"), expiry, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名下载 URL 失败: %w", err)
	}
	return u.String(), nil
}

func (s *minioStorage) PresignPut(ctx context.Context, key string, expiry time.Duration, contentType string, size int64) (PresignPutResult, error) {
	_ = size // S3 PUT URLs cannot enforce length; completion verifies the signed upload grant.
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	u, err := s.client.PresignedPutObject(ctx, s.bucket, strings.TrimLeft(key, "/"), expiry)
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

// MinIOClient 暴露底层 client（前端 CDN 同步等场景）。
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
