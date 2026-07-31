package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"draarl/internal/config"
)

// UploadMultipartFile 上传 multipart 文件，返回 object key 与大小。
func UploadMultipartFile(fileHeader *multipart.FileHeader, _ int, fileType string) (string, int64, error) {
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	ext := filepath.Ext(fileHeader.Filename)
	objectName := NewObjectKey(fileType, ext)
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = GuessContentType(ext, "application/octet-stream")
	}
	if err := Put(context.Background(), objectName, file, fileHeader.Size, contentType); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, fileHeader.Size, nil
}

// DetectMultipartContentType reads a small prefix and returns Go's detected content type.
func DetectMultipartContentType(fileHeader *multipart.FileHeader) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	return detectContentType(file)
}

// DetectObjectContentType reads a stored object prefix and returns Go's detected content type.
func DetectObjectContentType(ctx context.Context, objectName string) (string, error) {
	file, err := Open(ctx, objectName)
	if err != nil {
		return "", fmt.Errorf("打开对象失败: %w", err)
	}
	defer file.Close()

	return detectContentType(file)
}

func detectContentType(r io.Reader) (string, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("空文件")
	}
	return http.DetectContentType(buf[:n]), nil
}

func IsAllowedContentType(contentType string, allowed map[string]bool) bool {
	return allowed[strings.ToLower(strings.TrimSpace(contentType))]
}

// UploadAvatar 上传处理后的头像。
func UploadAvatar(_ int, imageData []byte, _ string) (string, int64, error) {
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	now := time.Now()
	objectName := fmt.Sprintf("uploads/avatar/%d/%02d/%s.jpg", now.Year(), int(now.Month()), newUUID())
	size := int64(len(imageData))
	if err := Put(context.Background(), objectName, bytes.NewReader(imageData), size, "image/jpeg"); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadLogo 上传处理后的 Logo。
func UploadLogo(imageData []byte, _ string) (string, int64, error) {
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	now := time.Now()
	objectName := fmt.Sprintf("uploads/logo/%d/%02d/%s.png", now.Year(), int(now.Month()), newUUID())
	size := int64(len(imageData))
	if err := Put(context.Background(), objectName, bytes.NewReader(imageData), size, "image/png"); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadFavicon 上传 favicon。
func UploadFavicon(fileHeader *multipart.FileHeader) (string, int64, error) {
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()
	fileData, err := io.ReadAll(file)
	if err != nil {
		return "", 0, fmt.Errorf("读取文件失败: %w", err)
	}

	contentType := fileHeader.Header.Get("Content-Type")
	ext := ".ico"
	switch contentType {
	case "image/png":
		ext = ".png"
	case "image/svg+xml":
		ext = ".svg"
	case "image/x-icon", "image/vnd.microsoft.icon":
		ext = ".ico"
	}

	now := time.Now()
	objectName := fmt.Sprintf("uploads/favicon/%d/%02d/%s%s", now.Year(), int(now.Month()), newUUID(), ext)
	size := int64(len(fileData))
	if err := Put(context.Background(), objectName, bytes.NewReader(fileData), size, contentType); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadThumbnail 上传缩略图。
func UploadThumbnail(objectName string, data []byte, contentType string) error {
	return Put(context.Background(), objectName, bytes.NewReader(data), int64(len(data)), contentType)
}

// UploadBytes 上传字节数据到指定 key。
func UploadBytes(ctx context.Context, objectName string, data []byte, contentType string) error {
	return Put(ctx, objectName, bytes.NewReader(data), int64(len(data)), contentType)
}

// DeleteFile 兼容旧命名。
func DeleteFile(ctx context.Context, objectName string) error {
	return Delete(ctx, objectName)
}

// GetFileURL 兼容旧命名。
func GetFileURL(objectName string) string {
	return readURLOrEmpty(objectName)
}

// MaxSizeForFileType 各类型大小上限。
func MaxSizeForFileType(fileType string) int64 {
	switch strings.ToLower(fileType) {
	case "firmware":
		return 16 * 1024 * 1024
	case "client_resource":
		if cfg := config.TryGet(); cfg != nil && cfg.Storage.UploadLimits.ClientResourceBytes > 0 {
			return cfg.Storage.UploadLimits.ClientResourceBytes
		}
		return config.DefaultClientResourceMaxBytes
	case "assets":
		return 100 * 1024 * 1024
	case "operator_cert":
		return 20 * 1024 * 1024
	case "favicon":
		return 1 * 1024 * 1024
	default:
		return 10 * 1024 * 1024
	}
}
