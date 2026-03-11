package minio

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"nrllink/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client MinIO客户端
var Client *minio.Client

// InitMinIO 初始化MinIO客户端
func InitMinIO() error {
	cfg := config.Get()
	if !cfg.MinIO.Enabled {
		log.Println("MinIO 未启用")
		return nil
	}

	var err error
	Client, err = minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})

	if err != nil {
		return fmt.Errorf("初始化MinIO客户端失败: %w", err)
	}

	// 检查bucket是否存在，不存在则创建
	ctx := context.Background()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "nrllink"
	}

	exists, err := Client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("检查bucket失败: %w", err)
	}

	if !exists {
		err = Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("创建bucket失败: %w", err)
		}
		log.Printf("创建MinIO bucket: %s", bucket)
	}

	log.Printf("MinIO 初始化成功: %s (bucket: %s)", cfg.MinIO.Endpoint, bucket)
	return nil
}

// UploadFile 上传文件到MinIO
func UploadFile(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	if Client == nil {
		return fmt.Errorf("MinIO客户端未初始化")
	}

	_, err := Client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// UploadMultipartFile 上传multipart文件
func UploadMultipartFile(fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, error) {
	if Client == nil {
		return "", 0, fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "nrllink"
	}

	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 生成文件路径: uploads/{fileType}/{year}/{month}/{userID}_{timestamp}{ext}
	ext := filepath.Ext(fileHeader.Filename)
	timestamp := time.Now().Format("20060102150405")
	objectName := fmt.Sprintf("uploads/%s/%d/%s_%s%s", fileType, userID, timestamp, generateRandomString(8), ext)

	// 获取内容类型
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		// 根据扩展名推测内容类型
		ext = strings.ToLower(ext)
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".pdf":
			contentType = "application/pdf"
		default:
			contentType = "application/octet-stream"
		}
	}

	// 上传文件
	err = UploadFile(context.Background(), bucket, objectName, file, fileHeader.Size, contentType)
	if err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}

	return objectName, fileHeader.Size, nil
}

// GetFileURL 获取文件的访问URL
func GetFileURL(objectName string) string {
	cfg := config.Get()
	if cfg.MinIO.BasePath != "" {
		return cfg.MinIO.BasePath + "/" + objectName
	}

	// 如果没有配置BasePath，返回MinIO的直链
	protocol := "http"
	if cfg.MinIO.UseSSL {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", protocol, cfg.MinIO.Endpoint, cfg.MinIO.Bucket, objectName)
}

// DeleteFile 删除文件
func DeleteFile(ctx context.Context, objectName string) error {
	if Client == nil {
		return fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "nrllink"
	}

	return Client.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
}

// PresignedURL 生成临时访问URL
func PresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	if Client == nil {
		return "", fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "nrllink"
	}

	url, err := Client.PresignedGetObject(ctx, bucket, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名URL失败: %w", err)
	}

	return url.String(), nil
}

// IsEnabled 检查MinIO是否启用
func IsEnabled() bool {
	cfg := config.Get()
	return cfg.MinIO.Enabled && Client != nil
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
