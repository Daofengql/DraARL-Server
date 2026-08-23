// Package minio 为兼容层，业务请优先使用 draarl/pkg/storage。
package minio

import (
	"context"
	"io"
	"mime/multipart"
	"time"

	"draarl/internal/config"
	"draarl/pkg/storage"

	miniogo "github.com/minio/minio-go/v7"
)

// Client 兼容旧全局变量（仅 minio 驱动时非 nil）。
var Client *miniogo.Client

// InitMinIO 初始化存储（兼容旧调用）。
func InitMinIO() error {
	err := storage.Init(config.Get())
	Client = storage.MinIOClient()
	return err
}

// GetClient 返回底层 minio client（可能为 nil）。
func GetClient() *miniogo.Client {
	return storage.MinIOClient()
}

// StartInitMinIOInBackground 兼容旧启动逻辑。
func StartInitMinIOInBackground() {
	storage.StartInitInBackground(config.Get())
	Client = storage.MinIOClient()
}

// UploadFile 上传到当前存储。
func UploadFile(ctx context.Context, _ string, objectName string, reader io.Reader, size int64, contentType string) error {
	return storage.Put(ctx, objectName, reader, size, contentType)
}

// UploadMultipartFileContext 兼容（带请求上下文）。
func UploadMultipartFileContext(ctx context.Context, fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, error) {
	return storage.UploadMultipartFileContext(ctx, fileHeader, userID, fileType)
}

func UploadMultipartFileWithContentTypeContext(ctx context.Context, fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, string, error) {
	return storage.UploadMultipartFileWithContentTypeContext(ctx, fileHeader, userID, fileType)
}

// UploadMultipartFile 兼容。
func UploadMultipartFile(fileHeader *multipart.FileHeader, userID int, fileType string) (string, int64, error) {
	return storage.UploadMultipartFile(fileHeader, userID, fileType)
}

// GetFileURL 兼容。
func GetFileURL(objectName string) string {
	return storage.GetFileURL(objectName)
}

// GetAvatarURL 兼容。
func GetAvatarURL(avatarPath string) string {
	return storage.GetAvatarURL(avatarPath)
}

// GetAvatarThumbURL 兼容。
func GetAvatarThumbURL(avatarPath string) string {
	return storage.GetAvatarThumbURL(avatarPath)
}

// DeleteFile 兼容。
func DeleteFile(ctx context.Context, objectName string) error {
	return storage.DeleteFile(ctx, objectName)
}

// PresignedURL 兼容。
func PresignedURL(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	return storage.PresignGet(ctx, objectName, expiry)
}

// IsEnabled 兼容。
func IsEnabled() bool {
	return storage.IsEnabled()
}

// GenerateThumbnail 兼容。
func GenerateThumbnail(originalObject string, width, height int, ext string) (string, []byte, error) {
	return storage.GenerateThumbnail(originalObject, width, height, ext)
}

func GenerateThumbnailContext(ctx context.Context, originalObject string, width, height int, ext string) (string, []byte, error) {
	return storage.GenerateThumbnailContext(ctx, originalObject, width, height, ext)
}

// ProcessAvatar 兼容。
func ProcessAvatar(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	return storage.ProcessAvatar(fileHeader)
}

func ProcessAvatarContext(ctx context.Context, fileHeader *multipart.FileHeader) ([]byte, string, error) {
	return storage.ProcessAvatarContext(ctx, fileHeader)
}

// UploadAvatar 兼容。
func UploadAvatar(userID int, imageData []byte, ext string) (string, int64, error) {
	return storage.UploadAvatar(userID, imageData, ext)
}

func UploadAvatarContext(ctx context.Context, userID int, imageData []byte, ext string) (string, int64, error) {
	return storage.UploadAvatarContext(ctx, userID, imageData, ext)
}

// UploadThumbnail 兼容。
func UploadThumbnail(objectName string, data []byte, contentType string) error {
	return storage.UploadThumbnail(objectName, data, contentType)
}

func UploadThumbnailContext(ctx context.Context, objectName string, data []byte, contentType string) error {
	return storage.UploadThumbnailContext(ctx, objectName, data, contentType)
}

// ProcessLogo 兼容。
func ProcessLogo(fileHeader *multipart.FileHeader) ([]byte, string, error) {
	return storage.ProcessLogo(fileHeader)
}

func ProcessLogoContext(ctx context.Context, fileHeader *multipart.FileHeader) ([]byte, string, error) {
	return storage.ProcessLogoContext(ctx, fileHeader)
}

// UploadLogo 兼容。
func UploadLogo(imageData []byte, ext string) (string, int64, error) {
	return storage.UploadLogo(imageData, ext)
}

func UploadLogoContext(ctx context.Context, imageData []byte, ext string) (string, int64, error) {
	return storage.UploadLogoContext(ctx, imageData, ext)
}

// UploadFavicon 兼容。
func UploadFavicon(fileHeader *multipart.FileHeader) (string, int64, error) {
	return storage.UploadFavicon(fileHeader)
}

func UploadFaviconContext(ctx context.Context, fileHeader *multipart.FileHeader) (string, int64, error) {
	return storage.UploadFaviconContext(ctx, fileHeader)
}
