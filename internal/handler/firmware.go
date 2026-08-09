package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"draarl/internal/firmwareversion"
	gormdb "draarl/internal/gormdb"
	"draarl/internal/protocol"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

const maxFirmwareSize = 16 * 1024 * 1024 // 16MB

// Preserve the historical firmware-version contract. Client resources use a
// separate strict SemVer 2.0.0 validator in internal/clientversion.
var firmwareSemverRegex = regexp.MustCompile(`^\d+\.\d+\.\d+(-[\w.]+)?$`)

// UploadFirmware 上传固件（管理员权限）
func UploadFirmware(c *gin.Context) {
	// 解析表单字段
	devModelStr := c.PostForm("dev_model")
	version := c.PostForm("version")
	changelog := c.PostForm("changelog")

	if devModelStr == "" || version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dev_model 和 version 为必填字段"})
		return
	}

	devModel, err := strconv.Atoi(devModelStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dev_model 格式无效"})
		return
	}

	// 白名单校验
	if !isSupportedFirmwareDevModel(devModel) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("设备型号 %d 不支持固件升级", devModel),
		})
		return
	}

	// 版本格式校验
	if !firmwareSemverRegex.MatchString(version) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "版本号格式无效，应为 semver 格式如 1.0.0 或 1.0.0-beta.1",
		})
		return
	}

	// 获取上传文件
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请选择固件文件"})
		return
	}

	// 文件大小校验
	if fileHeader.Size <= 0 || fileHeader.Size > maxFirmwareSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("固件文件必须非空且不超过 %d MB", maxFirmwareSize/1024/1024),
		})
		return
	}
	fileName, err := normalizeFirmwareFileName(fileHeader.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	repo := gormdb.GetFirmwareRepo()

	// 检查版本号是否已存在
	exists, err := repo.ExistsVersion(devModel, version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "检查版本号失败"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": fmt.Sprintf("设备型号 %d 已存在版本 %s", devModel, version),
		})
		return
	}

	// 先流式计算哈希，再将 multipart 内容写入 staging 并提升到不可变 key。
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取文件失败"})
		return
	}
	defer file.Close()

	fileSize, fileHash, err := hashFirmwareReader(file)
	if err != nil {
		status := http.StatusInternalServerError
		message := "读取文件失败"
		if errors.Is(err, storage.ErrObjectTooLarge) {
			status, message = http.StatusBadRequest, "文件大小超过限制"
		}
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取文件失败"})
		return
	}

	adminUserID, _ := c.Get("user_id")
	userID := 0
	if id, ok := adminUserID.(int); ok {
		userID = id
	}

	stagingKey := storage.NewStagingObjectKey("firmware", userID, storage.ExtFromFilename(fileName))
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if err := storage.Put(c.Request.Context(), stagingKey, file, fileSize, contentType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "固件文件上传失败"})
		return
	}
	finalKey := firmwareObjectKey(devModel, version, fileHash, fileName)
	if err := promoteFirmwareObject(c.Request.Context(), stagingKey, finalKey, fileSize, fileHash); err != nil {
		_ = storage.Delete(c.Request.Context(), stagingKey)
		status := http.StatusInternalServerError
		message := "固件文件上传失败"
		if errors.Is(err, storage.ErrFinalObjectAlreadyExists) {
			status, message = http.StatusConflict, "不可变固件对象已存在"
		}
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}

	// 创建数据库记录
	fw := &gormdb.FirmwareRelease{
		DevModel:  devModel,
		Version:   version,
		Changelog: changelog,
		FileName:  fileName,
		MinioPath: finalKey,
		FileSize:  fileSize,
		FileHash:  fileHash,
		CreatedBy: userID,
	}

	if err := repo.Create(fw); err != nil {
		// 回滚 MinIO 文件
		if delErr := storage.Delete(c.Request.Context(), finalKey); delErr != nil {
			log.Printf("回滚删除 MinIO 固件文件失败: %v", delErr)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建固件记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "固件上传成功",
		"data":    fw,
	})
}

// ListFirmware 获取固件列表（管理员权限）
func ListFirmware(c *gin.Context) {
	devModel, _ := strconv.Atoi(c.DefaultQuery("dev_model", "0"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	repo := gormdb.GetFirmwareRepo()
	list, total, err := repo.ListByDevModel(devModel, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取固件列表失败"})
		return
	}

	// 为每条记录生成下载 URL
	type firmwareItem struct {
		*gormdb.FirmwareRelease
		DownloadURL string `json:"download_url"`
	}

	items := make([]firmwareItem, 0, len(list))
	for _, fw := range list {
		downloadURL, err := firmwareDownloadURL(c, fw.MinioPath)
		if err != nil {
			log.Printf("生成固件下载链接失败 (id=%d): %v", fw.ID, err)
			downloadURL = ""
		}
		items = append(items, firmwareItem{
			FirmwareRelease: fw,
			DownloadURL:     downloadURL,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// CompleteFirmwareUpload 直传完成后落库。
// POST /api/firmware/complete
func CompleteFirmwareUpload(c *gin.Context) {
	var req struct {
		DevModel    int    `json:"dev_model" binding:"required"`
		Version     string `json:"version" binding:"required"`
		Changelog   string `json:"changelog"`
		ObjectKey   string `json:"object_key" binding:"required"`
		FileName    string `json:"file_name" binding:"required"`
		UploadToken string `json:"upload_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请求参数错误"})
		return
	}
	if !isSupportedFirmwareDevModel(req.DevModel) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": fmt.Sprintf("设备型号 %d 不支持固件升级", req.DevModel)})
		return
	}
	if !firmwareSemverRegex.MatchString(req.Version) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "版本号格式无效"})
		return
	}
	fileName, err := normalizeFirmwareFileName(req.FileName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	objectKey := strings.TrimLeft(req.ObjectKey, "/")
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权"})
		return
	}
	user := userValue.(*gormdb.User)
	if !storage.IsStagingObjectKey(objectKey, "firmware", user.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "非法的 object_key"})
		return
	}

	repo := gormdb.GetFirmwareRepo()
	versionExists, err := repo.ExistsVersion(req.DevModel, req.Version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "检查版本号失败"})
		return
	}
	if versionExists {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": fmt.Sprintf("设备型号 %d 已存在版本 %s", req.DevModel, req.Version)})
		return
	}

	grant, _, err := storage.ValidateStagedUpload(
		c.Request.Context(), req.UploadToken, objectKey, "firmware", user.ID,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "上传授权或对象校验失败"})
		return
	}

	written, fileHash, err := storage.HashObjectSHA256(c.Request.Context(), objectKey, maxFirmwareSize)
	if err != nil {
		status := http.StatusInternalServerError
		message := "读取固件文件失败"
		if errors.Is(err, storage.ErrObjectTooLarge) {
			status, message = http.StatusBadRequest, "文件大小超过限制"
		}
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}
	if written != grant.Size {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "固件文件大小校验失败"})
		return
	}
	finalKey := firmwareObjectKey(req.DevModel, req.Version, fileHash, fileName)
	if err := promoteFirmwareObject(c.Request.Context(), objectKey, finalKey, written, fileHash); err != nil {
		log.Printf("提升固件对象失败: %v", err)
		status := http.StatusInternalServerError
		message := "完成固件上传失败"
		if errors.Is(err, storage.ErrFinalObjectAlreadyExists) {
			status, message = http.StatusConflict, "不可变固件对象已存在"
		}
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}

	fw := &gormdb.FirmwareRelease{
		DevModel:  req.DevModel,
		Version:   req.Version,
		Changelog: req.Changelog,
		FileName:  fileName,
		MinioPath: finalKey,
		FileSize:  written,
		FileHash:  fileHash,
		CreatedBy: user.ID,
	}
	if err := repo.Create(fw); err != nil {
		_ = storage.Delete(c.Request.Context(), finalKey)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建固件记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "固件上传成功", "data": fw})
}

// DeleteFirmware 删除固件（管理员权限）
func DeleteFirmware(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "无效的固件ID"})
		return
	}

	repo := gormdb.GetFirmwareRepo()
	fw, err := repo.Delete(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "固件记录不存在"})
		return
	}

	// 删除 MinIO 文件（失败不影响数据库删除结果）
	if err := storage.Delete(c.Request.Context(), fw.MinioPath); err != nil {
		log.Printf("删除 MinIO 固件文件失败 (path=%s): %v", fw.MinioPath, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "固件删除成功",
	})
}

// GetLatestFirmware 获取指定型号的最新固件（公开接口）
func GetLatestFirmware(c *gin.Context) {
	devModelStr := c.Query("dev_model")
	currentVersion := c.Query("current_version")
	if devModelStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dev_model 参数必填"})
		return
	}

	devModel, err := strconv.Atoi(devModelStr)
	if err != nil || devModel == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "dev_model 参数无效"})
		return
	}
	if currentVersion != "" && !firmwareSemverRegex.MatchString(currentVersion) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "current_version 参数无效"})
		return
	}

	repo := gormdb.GetFirmwareRepo()
	fw, err := repo.GetLatestByDevModel(devModel)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": fmt.Sprintf("设备型号 %d 暂无可用固件", devModel),
		})
		return
	}

	if currentVersion != "" && !firmwareversion.IsNewerVersion(fw.Version, currentVersion) {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "当前已是最新版本",
			"data":    nil,
		})
		return
	}

	// 所有驱动统一生成短期下载 URL；local 也使用签名 GET。
	downloadURL, err := firmwareDownloadURL(c, fw.MinioPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成下载链接失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":           fw.ID,
			"dev_model":    fw.DevModel,
			"version":      fw.Version,
			"changelog":    fw.Changelog,
			"file_name":    fw.FileName,
			"file_size":    fw.FileSize,
			"file_hash":    fw.FileHash,
			"hash_algo":    "sha256",
			"has_update":   true,
			"download_url": downloadURL,
			"create_time":  fw.CreateTime,
		},
	})
}

func firmwareDownloadURL(c *gin.Context, objectKey string) (string, error) {
	downloadURL, err := storage.PresignGet(c.Request.Context(), objectKey, time.Hour)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = publicAPIBase(c) + downloadURL
	}
	return downloadURL, nil
}

func hashFirmwareReader(reader io.Reader) (int64, string, error) {
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(reader, maxFirmwareSize+1))
	if err != nil {
		return written, "", err
	}
	if written > maxFirmwareSize {
		return written, "", storage.ErrObjectTooLarge
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), nil
}

func isSupportedFirmwareDevModel(devModel int) bool {
	return devModel >= 0 && devModel <= 255 && protocol.IsFirmwareSupportedDevModel(byte(devModel))
}

func normalizeFirmwareFileName(value string) (string, error) {
	fileName := path.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if fileName == "" || fileName == "." || fileName == "/" || utf8.RuneCountInString(fileName) > 255 {
		return "", fmt.Errorf("固件文件名无效")
	}
	return fileName, nil
}

func firmwareObjectKey(devModel int, version, digest, fileName string) string {
	fileName = path.Base(strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/"))
	return path.Join("firmware", strconv.Itoa(devModel), version, digest, fileName)
}

func promoteFirmwareObject(ctx context.Context, stagedKey, finalKey string, expectedSize int64, expectedDigest string) error {
	if err := storage.Promote(ctx, stagedKey, finalKey); err != nil {
		return err
	}
	actualSize, actualDigest, err := storage.HashObjectSHA256(ctx, finalKey, maxFirmwareSize)
	if err != nil || actualSize != expectedSize || actualDigest != expectedDigest {
		_ = storage.Delete(ctx, finalKey)
		if err != nil {
			return fmt.Errorf("verify promoted firmware: %w", err)
		}
		return fmt.Errorf("promoted firmware integrity mismatch")
	}
	return nil
}
