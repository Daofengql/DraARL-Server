package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"

	"draarl/internal/firmwareversion"
	gormdb "draarl/internal/gormdb"
	"draarl/internal/protocol"
	"draarl/pkg/minio"

	"github.com/gin-gonic/gin"
)

const maxFirmwareSize = 16 * 1024 * 1024 // 16MB

var semverRegex = regexp.MustCompile(`^\d+\.\d+\.\d+(-[\w.]+)?$`)

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
	if !protocol.IsFirmwareSupportedDevModel(byte(devModel)) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("设备型号 %d 不支持固件升级", devModel),
		})
		return
	}

	// 版本格式校验
	if !semverRegex.MatchString(version) {
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
	if fileHeader.Size > maxFirmwareSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": fmt.Sprintf("文件大小超过限制（最大 %d MB）", maxFirmwareSize/1024/1024),
		})
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

	// 计算 SHA-256
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取文件失败"})
		return
	}
	defer file.Close()

	hasher := sha256.New()
	fileBytes := make([]byte, fileHeader.Size)
	if _, err := file.Read(fileBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取文件失败"})
		return
	}
	hasher.Write(fileBytes)
	fileHash := hex.EncodeToString(hasher.Sum(nil))

	// 上传到 MinIO
	adminUserID, _ := c.Get("user_id")
	userID := 0
	if id, ok := adminUserID.(int); ok {
		userID = id
	}

	minioPath, fileSize, err := minio.UploadMultipartFile(fileHeader, userID, "firmware")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "固件文件上传失败"})
		return
	}

	// 创建数据库记录
	fw := &gormdb.FirmwareRelease{
		DevModel:  devModel,
		Version:   version,
		Changelog: changelog,
		FileName:  fileHeader.Filename,
		MinioPath: minioPath,
		FileSize:  fileSize,
		FileHash:  fileHash,
		CreatedBy: userID,
	}

	if err := repo.Create(fw); err != nil {
		// 回滚 MinIO 文件
		if delErr := minio.DeleteFile(c.Request.Context(), minioPath); delErr != nil {
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
		downloadURL := minio.GetFileURL(fw.MinioPath)
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
	if err := minio.DeleteFile(c.Request.Context(), fw.MinioPath); err != nil {
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
	if currentVersion != "" && !semverRegex.MatchString(currentVersion) {
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

	// 生成下载 URL（优先走 BasePath）
	downloadURL := minio.GetFileURL(fw.MinioPath)
	if downloadURL == "" {
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
