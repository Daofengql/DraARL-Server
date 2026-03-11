package handler

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
	"nrllink/internal/config"
	"nrllink/pkg/minio"
)

// UploadResponse 文件上传响应
type UploadResponse struct {
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
	FileType  string `json:"file_type"`
	MinioPath string `json:"minio_path"`
	FileURL   string `json:"file_url"`
}

// UploadFile 通用文件上传接口
func UploadFile(c *gin.Context) {
	// 获取当前用户
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取文件类型参数（avatar, cert等）
	fileType := c.PostForm("file_type")
	if fileType == "" {
		fileType = "other"
	}

	// 获取上传的文件
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "获取文件失败",
		})
		return
	}

	// 检查文件大小（10MB）
	if fileHeader.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "文件大小不能超过10MB",
		})
		return
	}

	// 上传到MinIO
	objectName, fileSize, err := minio.UploadMultipartFile(fileHeader, user.ID, fileType)
	if err != nil {
		log.Printf("上传文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "上传文件失败",
		})
		return
	}

	// 生成访问URL
	fileURL := minio.GetFileURL(objectName)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "上传成功",
		"data": UploadResponse{
			FileName:  fileHeader.Filename,
			FileSize:  fileSize,
			FileType:  fileType,
			MinioPath: objectName,
			FileURL:   fileURL,
		},
	})
}

// UploadOperatorCertificate 上传操作证
func UploadOperatorCertificate(c *gin.Context) {
	// 获取当前用户
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 获取上传的文件
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "获取文件失败",
		})
		return
	}

	// 检查文件大小（10MB）
	if fileHeader.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "文件大小不能超过10MB",
		})
		return
	}

	// 检查文件类型（只允许图片）
	allowedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"application/pdf": true,
	}
	contentType := fileHeader.Header.Get("Content-Type")
	if !allowedTypes[contentType] && contentType != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "只支持图片和PDF文件",
		})
		return
	}

	// 上传到MinIO
	objectName, fileSize, err := minio.UploadMultipartFile(fileHeader, user.ID, "operator_cert")
	if err != nil {
		log.Printf("上传操作证失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "上传文件失败",
		})
		return
	}

	// 获取bucket名称
	cfg := config.Get()
	bucket := cfg.MinIO.Bucket
	if bucket == "" {
		bucket = "nrllink"
	}

	// 创建待审核操作证记录
	certRepo := gormdb.NewOperatorCertRepository()
	cert, err := certRepo.CreatePendingCert(user.ID, fileHeader.Filename, bucket, objectName, fileSize, contentType)
	if err != nil {
		log.Printf("保存操作证记录失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "保存操作证记录失��",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "操作证上传成功，请等待管理员审核",
		"data": gin.H{
			"id":         cert.ID,
			"file_name":  cert.FileName,
			"file_size":  cert.FileSize,
			"upload_time": cert.UploadTime.Format("2006-01-02 15:04:05"),
		},
	})
}

// GetOperatorCertificate 获取用��的操作证信息
func GetOperatorCertificate(c *gin.Context) {
	// 获取当前用户
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	// 获取用户信息
	userRepo := gormdb.NewUserRepository()
	user, err := userRepo.GetUserByName(username.(string))
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 优先获取待审核或被拒绝的操作证，否则获取已通过的
	certRepo := gormdb.NewOperatorCertRepository()

	// 先尝试获取待审核的操作证
	cert, _ := certRepo.GetPendingByUserID(user.ID)
	if cert == nil {
		// 如果没有待审核的，获取已通过的
		cert, _ = certRepo.GetActiveByUserID(user.ID)
	}

	// 如果还是没有，获取最新的（可能是被拒绝的）
	if cert == nil {
		cert, _ = certRepo.GetLatestByUserID(user.ID)
	}

	if cert == nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    200,
			"message": "成功",
			"data":    nil,
		})
		return
	}

	// 生成访问URL
	fileURL := minio.GetFileURL(cert.MinioPath)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"id":          cert.ID,
			"file_name":   cert.FileName,
			"file_size":   cert.FileSize,
			"file_type":   cert.FileType,
			"upload_time": cert.UploadTime.Format("2006-01-02 15:04:05"),
			"file_url":    fileURL,
			"status":      cert.Status,
			"review_note": cert.ReviewNote,
		},
	})
}

// GetOperatorCertificateURL 获取操作证临时访问URL（带签名）
func GetOperatorCertificateURL(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的操作证ID",
		})
		return
	}

	// 获取操作证
	certRepo := gormdb.NewOperatorCertRepository()
	cert, err := certRepo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "操作证不存在",
		})
		return
	}

	// 生成临时访问URL（1小时有效）
	url, err := minio.PresignedURL(c.Request.Context(), cert.MinioPath, time.Hour)
	if err != nil {
		log.Printf("生成访问URL失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成访问URL失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"url":      url,
			"expires_in": int(time.Hour.Seconds()),
		},
	})
}

// GetPendingApprovals 获取待审批用户列表（管理员）
func GetPendingApprovals(c *gin.Context) {
	// 检查管理员权限
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, err := userRepo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	if !hasRoleGORM(currentUser, "admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理��权限",
		})
		return
	}

	// 获取分页参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	// 获取状态参数：0=待审核, 1=已通过, 2=已拒绝
	status, _ := strconv.Atoi(c.DefaultQuery("status", "0"))

	// 根据状态获取用户列表
	certRepo := gormdb.NewOperatorCertRepository()
	var userWithCerts []*gormdb.UserWithCert
	var total int64

	if status == 0 {
		// 待审核：有待审核操作证的用户
		userWithCerts, total, err = certRepo.ListPending(limit, (page-1)*limit)
		if err != nil {
			log.Printf("获取待审批用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取待审批用户失败",
			})
			return
		}
	} else if status == 2 {
		// 已拒绝：有被拒绝操作证的用户
		userWithCerts, total, err = certRepo.ListRejected(limit, (page-1)*limit)
		if err != nil {
			log.Printf("获取已拒绝用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取已拒绝用户失败",
			})
			return
		}
	} else {
		// 已通过：获取账户已通过的用户（从user表查询）
		userList, err := userRepo.ListByApprovalStatus(1, limit, (page-1)*limit)
		if err != nil {
			log.Printf("获取已通过用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取已通过用户失败",
			})
			return
		}

		// 获取总数
		count, _ := userRepo.CountByApprovalStatus(1)
		total = count

		// 转换为UserWithCert格式
		userWithCerts = make([]*gormdb.UserWithCert, 0, len(userList))
		for _, u := range userList {
			cert, _ := certRepo.GetLatestByUserID(u.ID)
			userWithCerts = append(userWithCerts, &gormdb.UserWithCert{
				User: u,
				Cert: cert,
			})
		}
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(userWithCerts))
	for _, uw := range userWithCerts {
		u := uw.User
		cert := uw.Cert

		item := gin.H{
			"id":              u.ID,
			"username":        u.Name,
			"nickname":        u.NickName,
			"callsign":        u.CallSign,
			"phone":           u.Phone,
			"address":         u.Address,
			"approval_status": u.ApprovalStatus,
			"created_at":      u.CreateTime.Format("2006-01-02 15:04:05"),
			"has_cert":        cert != nil,
		}

		if cert != nil {
			item["cert"] = gin.H{
				"id":          cert.ID,
				"file_name":   cert.FileName,
				"file_size":   cert.FileSize,
				"file_type":   cert.FileType,
				"upload_time": cert.UploadTime.Format("2006-01-02 15:04:05"),
				"file_url":    minio.GetFileURL(cert.MinioPath),
				"status":      cert.Status,
			}
		}

		if u.ReviewTime != nil {
			item["review_time"] = u.ReviewTime.Format("2006-01-02 15:04:05")
		}
		if u.ReviewerID != nil {
			item["reviewer_id"] = *u.ReviewerID
		}
		item["review_note"] = u.ReviewNote

		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total":     total,
			"items":     items,
			"page":      page,
			"page_size": limit,
		},
	})
}

// ApprovalRequest 审批请求
type ApprovalRequest struct {
	Status int    `json:"status" binding:"required"` // 1=通过, 2=拒绝
	Note   string `json:"note"`
}

// ApproveUser 审批用户（管理员）
func ApproveUser(c *gin.Context) {
	// 检查管理员权限
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, err := userRepo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	if !hasRoleGORM(currentUser, "admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取用户ID
	idStr := c.Param("id")
	userID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的用户ID",
		})
		return
	}

	// 获取审批请求
	var req ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查审批状态是否有效
	if req.Status != 1 && req.Status != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的审批状态",
		})
		return
	}

	// 获取目标用户
	targetUser, err := userRepo.GetUserByID(userID)
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	// 更新审批状态
	err = userRepo.UpdateUserApproval(userID, req.Status, currentUser.ID, req.Note)
	if err != nil {
		log.Printf("更新审批状态失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新审批状态失败",
		})
		return
	}

	statusText := "通过"
	if req.Status == 2 {
		statusText = "拒绝"
	}

	log.Printf("管理员 %s 审批用户 %s: %s", currentUser.Name, targetUser.Name, statusText)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "审批成功",
		"data": gin.H{
			"id":     userID,
			"status": req.Status,
		},
	})
}

// ApproveOperatorCertificate 审批操作证（管理员）
func ApproveOperatorCertificate(c *gin.Context) {
	// 检查管理员权限
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	userRepo := gormdb.NewUserRepository()
	currentUser, err := userRepo.GetUserByName(username.(string))
	if err != nil || currentUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户不存在",
		})
		return
	}

	if !hasRoleGORM(currentUser, "admin") {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "需要管理员权限",
		})
		return
	}

	// 获取操作证ID
	idStr := c.Param("id")
	certID, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的操作证ID",
		})
		return
	}

	// 获取审批请求
	var req ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 检查审批状态是否有效
	if req.Status != 1 && req.Status != 2 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的审批状态",
		})
		return
	}

	// 获取操作证
	certRepo := gormdb.NewOperatorCertRepository()
	_, err = certRepo.GetByID(certID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "操作证不存在",
		})
		return
	}

	// 更新操作证审批状态
	if req.Status == 1 {
		err = certRepo.ApproveCert(certID, currentUser.ID, req.Note)
	} else {
		err = certRepo.RejectCert(certID, currentUser.ID, req.Note)
	}

	if err != nil {
		log.Printf("更新操作证审批状态失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新审批状态失败",
		})
		return
	}

	statusText := "通过"
	if req.Status == 2 {
		statusText = "拒绝"
	}

	log.Printf("管理员 %s 审批操作证 %d: %s", currentUser.Name, certID, statusText)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "审批成功",
		"data": gin.H{
			"id":     certID,
			"status": req.Status,
		},
	})
}
