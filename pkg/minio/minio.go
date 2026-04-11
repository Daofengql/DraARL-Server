package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"image"
	_ "image/gif" // 注册 GIF 解码器
	"image/jpeg"
	_ "image/jpeg" // 注册 JPEG 解码器
	"image/png"
	_ "image/png" // 注册 PNG 解码器

	"draarl/internal/config"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client MinIO客户端
var Client *minio.Client

var (
	clientMu sync.RWMutex
	initOnce sync.Once
)

const (
	initTimeout       = 5 * time.Second
	initRetryInterval = 30 * time.Second
)

func getClient() *minio.Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return Client
}

func setClient(c *minio.Client) {
	clientMu.Lock()
	Client = c
	clientMu.Unlock()
}

// GetClient 返回当前 MinIO 客户端（可能为 nil）
func GetClient() *minio.Client {
	return getClient()
}

// InitMinIO 初始化MinIO客户端
func InitMinIO() error {
	cfg := config.Get()
	endpoint := strings.TrimSpace(cfg.MinIO.Endpoint)
	if endpoint == "" {
		return fmt.Errorf("MinIO Endpoint 为空")
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   initTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   initTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: initTimeout,
	}

	var err error
	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure:    cfg.MinIO.UseSSL,
		Transport: transport,
	})

	if err != nil {
		return fmt.Errorf("初始化MinIO客户端失败: %w", err)
	}

	// 检查bucket是否存在，不存在则创建
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("检查bucket失败: %w", err)
	}

	if !exists {
		createCtx, createCancel := context.WithTimeout(context.Background(), initTimeout)
		defer createCancel()

		err = client.MakeBucket(createCtx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("创建bucket失败: %w", err)
		}
		log.Printf("创建MinIO bucket: %s", bucket)
	}

	setClient(client)
	log.Printf("MinIO 初始化成功: %s (bucket: %s)", endpoint, bucket)
	return nil
}

// StartInitMinIOInBackground 后台初始化 MinIO，失败自动重试，不阻塞主服务启动
func StartInitMinIOInBackground() {
	cfg := config.Get()
	if strings.TrimSpace(cfg.MinIO.Endpoint) == "" {
		log.Printf("MinIO Endpoint 未配置，跳过初始化")
		return
	}

	initOnce.Do(func() {
		go func() {
			for {
				if err := InitMinIO(); err != nil {
					log.Printf("MinIO 初始化失败，%s 后重试: %v", initRetryInterval, err)
					time.Sleep(initRetryInterval)
					continue
				}
				return
			}
		}()
	})
}

// UploadFile 上传文件到MinIO
func UploadFile(ctx context.Context, bucket, objectName string, reader io.Reader, size int64, contentType string) error {
	client := getClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端未初始化")
	}

	_, err := client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// UploadMultipartFile 上传multipart文件
// 生成格式: uploads/{fileType}/{year}/{month}/{uuid}{ext}
// 例如: uploads/avatar/2026/03/a1b2c3d4-e5f6-7890-abcd-ef1234567890.jpg
func UploadMultipartFile(fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, error) {
	if getClient() == nil {
		return "", 0, fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 生成文件路径: uploads/{fileType}/{year}/{month}/{uuid}{ext}
	// 使用UUID作为文件名，更专业且避免冲突
	ext := filepath.Ext(fileHeader.Filename)
	now := time.Now()
	fileUUID := uuid.New().String()
	objectName := fmt.Sprintf("uploads/%s/%d/%02d/%s%s", fileType, now.Year(), int(now.Month()), fileUUID, ext)

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
	if strings.TrimSpace(objectName) == "" {
		return ""
	}

	cfg := config.Get()
	cleanObject := strings.TrimLeft(objectName, "/")

	if cfg.MinIO.BasePath != "" {
		return cfg.MinIO.BasePath + "/" + cleanObject
	}

	// 如果没有配置BasePath，返回MinIO的直链
	protocol := "http"
	if cfg.MinIO.UseSSL {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", protocol, cfg.MinIO.Endpoint, cfg.MinIO.Bucket, cleanObject)
}

// GetAvatarURL 根据头像相对路径生成原图完整URL
// 输入: 2026/03/uuid.jpg
// 输出: https://oss.example.com/bucket/uploads/avatar/2026/03/uuid.jpg
func GetAvatarURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	return GetFileURL("uploads/avatar/" + avatarPath)
}

// GetAvatarThumbURL 根据头像相对路径生成缩略图完整URL
// 输入: 2026/03/uuid.jpg
// 输出: https://oss.example.com/bucket/thumb/uploads/avatar/2026/03/uuid.jpg
func GetAvatarThumbURL(avatarPath string) string {
	if avatarPath == "" {
		return ""
	}
	return GetFileURL("thumb/uploads/avatar/" + avatarPath)
}

// DeleteFile 删除文件
func DeleteFile(ctx context.Context, objectName string) error {
	client := getClient()
	if client == nil {
		return fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	return client.RemoveObject(ctx, bucket, objectName, minio.RemoveObjectOptions{})
}

// PresignedURL 生成临时访问URL
func PresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	client := getClient()
	if client == nil {
		return "", fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	url, err := client.PresignedGetObject(ctx, bucket, objectName, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("生成预签名URL失败: %w", err)
	}

	return url.String(), nil
}

// IsEnabled 检查MinIO是否启用
func IsEnabled() bool {
	return getClient() != nil
}

// GenerateThumbnail 生成图片缩略图
// 返回缩略图的 objectName 和 thumbnailData
func GenerateThumbnail(originalObject string, width, height int, ext string) (string, []byte, error) {
	// 生成缩略图路径: 在原路径前加 thumb/
	// 例如: uploads/avatar/2026/03/uuid.jpg -> thumb/uploads/avatar/2026/03/uuid.jpg
	thumbObjectName := "thumb/" + originalObject

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	ctx := context.Background()
	client := getClient()
	if client == nil {
		return "", nil, fmt.Errorf("MinIO客户端未初始化")
	}

	// 从MinIO下载原图片
	reader, err := client.GetObject(ctx, bucket, originalObject, minio.GetObjectOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("下载原图片失败: %w", err)
	}
	defer reader.Close()

	// 解码图片
	img, _, err := image.Decode(reader)
	if err != nil {
		return "", nil, fmt.Errorf("解码图片失败: %w", err)
	}

	// 生成缩略图
	thumbnail := imaging.Resize(img, width, height, imaging.Lanczos)

	// 编码为字节
	var buf bytes.Buffer
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(&buf, thumbnail, &jpeg.Options{Quality: 85})
	case ".png":
		err = png.Encode(&buf, thumbnail)
	default:
		err = jpeg.Encode(&buf, thumbnail, &jpeg.Options{Quality: 85})
	}

	if err != nil {
		return "", nil, fmt.Errorf("编码缩略图失败: %w", err)
	}

	return thumbObjectName, buf.Bytes(), nil
}

// ProcessAvatar 处理头像图片：裁切为正方形、限制尺寸、重新编码
// 1. 限制最大尺寸为 2000x2000
// 2. 非正方形图片进行中心裁切为正方形
// 3. 重新编码为 JPEG 格式
// 返回处理后的图片数据
func ProcessAvatar(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 解码图片
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, "", fmt.Errorf("解码图片失败: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 检查尺寸是否超过限制
	const maxSize = 2000
	if width > maxSize || height > maxSize {
		// 如果超过限制，按比例缩小到最大尺寸内
		scale := float64(maxSize) / float64(max(width, height))
		newWidth := int(float64(width) * scale)
		newHeight := int(float64(height) * scale)
		img = imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)
		bounds = img.Bounds()
		width = bounds.Dx()
		height = bounds.Dy()
	}

	// 如果不是正方形，进行中心裁切
	var cropped image.Image
	if width != height {
		// 取较小的边作为正方形边长
		size := width
		if height < size {
			size = height
		}

		// 计算裁切区域（中心裁切）
		x := (width - size) / 2
		y := (height - size) / 2
		cropped = imaging.Crop(img, image.Rect(x, y, x+size, y+size))
	} else {
		cropped = img
	}

	// 重新编码为 JPEG 格式，质量 85
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, cropped, &jpeg.Options{Quality: 85})
	if err != nil {
		return nil, "", fmt.Errorf("编码图片失败: %w", err)
	}

	// 获取文件扩展名
	ext := filepath.Ext(fileHeader.Filename)
	if ext == "" {
		ext = ".jpg"
	}

	return buf.Bytes(), ext, nil
}

// UploadAvatar 上传处理后的头像图片
func UploadAvatar(userID int, imageData []byte, ext string) (string, int64, error) {
	client := getClient()
	if client == nil {
		return "", 0, fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	// 生成文件路径
	now := time.Now()
	fileUUID := uuid.New().String()
	objectName := fmt.Sprintf("uploads/avatar/%d/%02d/%s%s", now.Year(), int(now.Month()), fileUUID, ".jpg")

	// 上传文件
	ctx := context.Background()
	reader := bytes.NewReader(imageData)
	size := int64(len(imageData))

	_, err := client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: "image/jpeg",
	})
	if err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}

	return objectName, size, nil
}

// UploadThumbnail 上传缩略图
func UploadThumbnail(objectName string, data []byte, contentType string) error {
	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	ctx := context.Background()
	reader := bytes.NewReader(data)
	size := int64(len(data))

	return UploadFile(ctx, bucket, objectName, reader, size, contentType)
}

// ProcessLogo 处理 Logo 图片：限制宽度为 500px，保持原始比例
// 1. 限制最大尺寸为 500x500
// 2. 保持原始宽高比，按比例缩放
// 3. 重新编码为 PNG 格式（保留透明通道）
// 返回处理后的图片数据
func ProcessLogo(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 读取全部内容到内存
	buf := make([]byte, fileHeader.Size)
	_, err = io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, "", fmt.Errorf("读取文件失败: %w", err)
	}

	// 尝试多种方式解码图片
	var img image.Image
	var format string
	var decodeErr error

	// 1. 首先尝试标准库的 image.Decode
	reader := bytes.NewReader(buf)
	img, format, decodeErr = image.Decode(reader)
	if decodeErr != nil {
		// 2. 如果失败，尝试使用 imaging.Decode（支持更多格式）
		reader = bytes.NewReader(buf)
		img, decodeErr = imaging.Decode(reader, imaging.AutoOrientation(true))
		if decodeErr != nil {
			// 3. 如果仍然失败，尝试 PNG 和 JPEG 专用解码器
			reader = bytes.NewReader(buf)
			if img, decodeErr = png.Decode(reader); decodeErr == nil {
				format = "png"
			} else {
				reader = bytes.NewReader(buf)
				if img, decodeErr = jpeg.Decode(reader); decodeErr == nil {
					format = "jpeg"
				} else {
					return nil, "", fmt.Errorf("解码图片失败（尝试了所有解码器）: %w", decodeErr)
				}
			}
		}
	}

	// 如果没有检测到格式，使用原文件扩展名
	if format == "" {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		switch ext {
		case ".jpg", ".jpeg":
			format = "jpeg"
		case ".png":
			format = "png"
		case ".gif":
			format = "gif"
		default:
			format = "png" // 默认使用 PNG
		}
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// 检查尺寸是否超过限制（宽度限制500，高度也限制500）
	const maxLogoWidth = 500
	const maxLogoHeight = 500

	if width > maxLogoWidth || height > maxLogoHeight {
		// 按比例缩放到限制范围内
		scaleX := float64(maxLogoWidth) / float64(width)
		scaleY := float64(maxLogoHeight) / float64(height)
		scale := scaleX
		if scaleY < scaleX {
			scale = scaleY
		}
		newWidth := int(float64(width) * scale)
		newHeight := int(float64(height) * scale)
		img = imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)
	}

	// 重新编码为 PNG 格式（保留透明通道）
	var outputBuf bytes.Buffer
	err = png.Encode(&outputBuf, img)
	if err != nil {
		return nil, "", fmt.Errorf("编码图片失败: %w", err)
	}

	return outputBuf.Bytes(), ".png", nil
}

// UploadLogo 上传处理后的 Logo 图片
func UploadLogo(imageData []byte, ext string) (string, int64, error) {
	client := getClient()
	if client == nil {
		return "", 0, fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	// 生成文件路径
	now := time.Now()
	fileUUID := uuid.New().String()
	objectName := fmt.Sprintf("uploads/logo/%d/%02d/%s.png", now.Year(), int(now.Month()), fileUUID)

	// 上传文件
	ctx := context.Background()
	reader := bytes.NewReader(imageData)
	size := int64(len(imageData))

	_, err := client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: "image/png",
	})
	if err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}

	return objectName, size, nil
}

// UploadFavicon 上传站点favicon（直接上传，不做处理）
func UploadFavicon(fileHeader *multipart.FileHeader) (string, int64, error) {
	client := getClient()
	if client == nil {
		return "", 0, fmt.Errorf("MinIO客户端未初始化")
	}

	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "draarl"
	}

	// 打开文件
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 读取文件内容
	fileData, err := io.ReadAll(file)
	if err != nil {
		return "", 0, fmt.Errorf("读取文件失败: %w", err)
	}

	// 确定Content-Type和扩展名
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

	// 生成文件路径
	now := time.Now()
	fileUUID := uuid.New().String()
	objectName := fmt.Sprintf("uploads/favicon/%d/%02d/%s%s", now.Year(), int(now.Month()), fileUUID, ext)

	// 上传文件
	ctx := context.Background()
	reader := bytes.NewReader(fileData)
	size := int64(len(fileData))

	_, err = client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}

	return objectName, size, nil
}
