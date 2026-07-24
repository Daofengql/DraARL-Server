package storage

import (
	"context"
	"encoding/json"
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
	client         *minio.Client
	publicClient   *minio.Client
	bucket         string
	basePath       string
	publicEndpoint string
	publicUseSSL   bool
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
	publicEndpoint, publicUseSSL := resolveMinIOPublicTarget(mc)
	publicClient := client
	if publicEndpoint != endpoint || publicUseSSL != mc.UseSSL {
		publicClient, err = minio.New(publicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(mc.AccessKey, mc.SecretKey, ""),
			Secure: publicUseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("初始化 MinIO 对外签名客户端失败: %w", err)
		}
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
	policy, err := publicReadBucketPolicy(bucket)
	if err != nil {
		return nil, fmt.Errorf("生成 bucket 公共读策略失败: %w", err)
	}
	policyCtx, policyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer policyCancel()
	if err := client.SetBucketPolicy(policyCtx, bucket, policy); err != nil {
		return nil, fmt.Errorf("配置 bucket 公共读策略失败: %w", err)
	}

	return &minioStorage{
		client:         client,
		publicClient:   publicClient,
		bucket:         bucket,
		basePath:       strings.TrimRight(strings.TrimSpace(mc.BasePath), "/"),
		publicEndpoint: publicEndpoint,
		publicUseSSL:   publicUseSSL,
	}, nil
}

func publicReadBucketPolicy(bucket string) (string, error) {
	type statement struct {
		Effect    string              `json:"Effect"`
		Principal map[string][]string `json:"Principal"`
		Action    []string            `json:"Action"`
		Resource  []string            `json:"Resource"`
	}
	type policyDocument struct {
		Version   string      `json:"Version"`
		Statement []statement `json:"Statement"`
	}

	encoded, err := json.Marshal(policyDocument{
		Version: "2012-10-17",
		Statement: []statement{{
			Effect:    "Allow",
			Principal: map[string][]string{"AWS": {"*"}},
			Action:    []string{"s3:GetObject"},
			Resource:  []string{"arn:aws:s3:::" + bucket + "/*"},
		}},
	})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func resolveMinIOPublicTarget(config config.MinIOConfig) (string, bool) {
	if endpoint := strings.TrimSpace(config.PublicEndpoint); endpoint != "" {
		return endpoint, config.PublicUseSSL
	}
	return strings.TrimSpace(config.Endpoint), config.UseSSL
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
	if s.publicUseSSL {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", protocol, s.publicEndpoint, s.bucket, clean)
}

func (s *minioStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.publicClient.PresignedGetObject(ctx, s.bucket, strings.TrimLeft(key, "/"), expiry, nil)
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
