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

// ErrFileTypeNotAllowed 表示上传文件的内容类型或扩展名被安全策略拒绝。
var ErrFileTypeNotAllowed = fmt.Errorf("file type not allowed")

// blockedUploadContentTypes 上传内容嗅探黑名单。
// 【H13 安全修复】这些类型会被浏览器当作脚本/HTML/SVG 内联执行，
// 在任何业务场景都不应允许上传。
var blockedUploadContentTypes = map[string]struct{}{
	"text/html":                {},
	"application/xhtml+xml":    {},
	"image/svg+xml":            {},
	"application/javascript":   {},
	"text/javascript":          {},
	"application/x-javascript": {},
	"text/xml":                 {},
	"application/xml":          {},
}

// blockedUploadExtensions 上传扩展名黑名单（脚本/可执行/可被服务器解释的类型）。
var blockedUploadExtensions = map[string]struct{}{
	".html": {}, ".htm": {}, ".xhtml": {}, ".shtml": {},
	".svg": {}, ".svgz": {},
	".js": {}, ".mjs": {}, ".jsonp": {},
	".xml": {},
	".php": {}, ".php3": {}, ".php4": {}, ".php5": {}, ".phtml": {}, ".phar": {},
	".asp": {}, ".aspx": {}, ".jsp": {}, ".jspx": {}, ".cgi": {},
	".sh": {}, ".bash": {}, ".zsh": {}, ".bat": {}, ".cmd": {}, ".ps1": {}, ".vbs": {}, ".vbe": {},
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".msi": {}, ".scr": {}, ".com": {}, ".pif": {},
	".jar": {}, ".class": {}, ".swf": {}, ".apk": {}, ".dex": {},
}

const maxFaviconUploadBytes = 1 * 1024 * 1024

// detectFaviconContent validates the file signature and returns a canonical
// content type and extension. Client supplied MIME headers are deliberately
// ignored because a favicon is later rendered by the application shell.
func detectFaviconContent(data []byte) (contentType, extension string, ok bool) {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png", ".png", true
	}
	// ICO header: reserved=0, type=1, image count must be non-zero.
	if len(data) >= 6 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 && (data[4] != 0 || data[5] != 0) {
		return "image/x-icon", ".ico", true
	}
	return "", "", false
}

// isBlockedUploadContentType 判断嗅探出的内容类型是否属于危险类型。
func isBlockedUploadContentType(contentType string) bool {
	_, blocked := blockedUploadContentTypes[strings.ToLower(strings.TrimSpace(contentType))]
	return blocked
}

// isBlockedUploadExtension 判断上传文件名扩展名是否属于危险类型。
func isBlockedUploadExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	_, blocked := blockedUploadExtensions[ext]
	return blocked
}

// UploadMultipartFileContext 上传 multipart 文件，返回 object key 与大小。
// 【H13 安全修复】上传前做内容嗅探 + 危险类型/扩展名拒绝，且落盘 Content-Type
// 使用嗅探结果而非客户端声明，杜绝 Content-Type 混淆导致的存储型 XSS。
// 【性能/资源修复】接收调用方上下文：客户端断连即取消传输，避免继续传完整对象。
func UploadMultipartFileContext(ctx context.Context, fileHeader *multipart.FileHeader, _ int, fileType string) (string, int64, error) {
	objectName, size, _, err := UploadMultipartFileWithContentTypeContext(ctx, fileHeader, 0, fileType)
	return objectName, size, err
}

// UploadMultipartFileWithContentTypeContext uploads a multipart object and
// returns the canonical sniffed content type used for storage metadata.
func UploadMultipartFileWithContentTypeContext(ctx context.Context, fileHeader *multipart.FileHeader, _ int, fileType string) (string, int64, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if Get() == nil {
		return "", 0, "", fmt.Errorf("存储未初始化")
	}
	detected, err := DetectMultipartContentType(fileHeader)
	if err != nil {
		return "", 0, "", fmt.Errorf("检测文件内容失败: %w", err)
	}
	if isBlockedUploadContentType(detected) {
		return "", 0, "", fmt.Errorf("%w: %s", ErrFileTypeNotAllowed, detected)
	}
	if isBlockedUploadExtension(fileHeader.Filename) {
		return "", 0, "", fmt.Errorf("%w: %s", ErrFileTypeNotAllowed, filepath.Ext(fileHeader.Filename))
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	ext := filepath.Ext(fileHeader.Filename)
	objectName := NewObjectKey(fileType, ext)
	if err := Put(ctx, objectName, file, fileHeader.Size, detected); err != nil {
		return "", 0, "", fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, fileHeader.Size, detected, nil
}

// UploadMultipartFile 兼容入口（使用后台上下文，等价旧行为）。
func UploadMultipartFile(fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, error) {
	return UploadMultipartFileContext(context.Background(), fileHeader, userID, fileType)
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
	return UploadAvatarContext(context.Background(), 0, imageData, "")
}

// UploadAvatarContext uploads processed avatar data with request cancellation.
func UploadAvatarContext(ctx context.Context, _ int, imageData []byte, _ string) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	now := time.Now()
	objectName := fmt.Sprintf("uploads/avatar/%d/%02d/%s.jpg", now.Year(), int(now.Month()), newUUID())
	size := int64(len(imageData))
	if err := Put(ctx, objectName, bytes.NewReader(imageData), size, "image/jpeg"); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadLogo 上传处理后的 Logo。
func UploadLogo(imageData []byte, _ string) (string, int64, error) {
	return UploadLogoContext(context.Background(), imageData, "")
}

// UploadLogoContext uploads processed logo data with request cancellation.
func UploadLogoContext(ctx context.Context, imageData []byte, _ string) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	now := time.Now()
	objectName := fmt.Sprintf("uploads/logo/%d/%02d/%s.png", now.Year(), int(now.Month()), newUUID())
	size := int64(len(imageData))
	if err := Put(ctx, objectName, bytes.NewReader(imageData), size, "image/png"); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadFavicon 上传 favicon。
func UploadFavicon(fileHeader *multipart.FileHeader) (string, int64, error) {
	return UploadFaviconContext(context.Background(), fileHeader)
}

// UploadFaviconContext uploads a validated favicon with request cancellation.
func UploadFaviconContext(ctx context.Context, fileHeader *multipart.FileHeader) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if Get() == nil {
		return "", 0, fmt.Errorf("存储未初始化")
	}
	if fileHeader == nil || fileHeader.Size < 1 || fileHeader.Size > maxFaviconUploadBytes {
		return "", 0, fmt.Errorf("%w: favicon size", ErrFileTypeNotAllowed)
	}
	file, err := fileHeader.Open()
	if err != nil {
		return "", 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()
	fileData, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxFaviconUploadBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("读取文件失败: %w", err)
	}
	if int64(len(fileData)) != fileHeader.Size || int64(len(fileData)) > maxFaviconUploadBytes {
		return "", 0, fmt.Errorf("%w: favicon size", ErrFileTypeNotAllowed)
	}
	contentType, ext, ok := detectFaviconContent(fileData)
	if !ok {
		return "", 0, fmt.Errorf("%w: favicon signature", ErrFileTypeNotAllowed)
	}

	now := time.Now()
	objectName := fmt.Sprintf("uploads/favicon/%d/%02d/%s%s", now.Year(), int(now.Month()), newUUID(), ext)
	size := int64(len(fileData))
	if err := Put(ctx, objectName, bytes.NewReader(fileData), size, contentType); err != nil {
		return "", 0, fmt.Errorf("上传文件失败: %w", err)
	}
	return objectName, size, nil
}

// UploadThumbnail 上传缩略图。
func UploadThumbnail(objectName string, data []byte, contentType string) error {
	return UploadThumbnailContext(context.Background(), objectName, data, contentType)
}

// UploadThumbnailContext uploads a thumbnail with request cancellation.
func UploadThumbnailContext(ctx context.Context, objectName string, data []byte, contentType string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return Put(ctx, objectName, bytes.NewReader(data), int64(len(data)), contentType)
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
