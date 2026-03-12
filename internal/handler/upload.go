package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
	"nrllink/internal/config"
	oplog "nrllink/internal/log"
	"nrllink/pkg/minio"
)

// UploadResponse 文件上传响应
type UploadResponse struct {
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	FileType     string `json:"file_type"`
	MinioPath    string `json:"minio_path"`
	FileURL      string `json:"file_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"` // 缩略图URL
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

	// 检查文件类型（avatar, cert等）
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

	var objectName string
	var finalFileSize int64
	var thumbnailURL string

	// 处理头像上传：验证格式、尺寸、裁切、重新编码
	if fileType == "avatar" {
		// 处理头像图片：裁切为正方形、限制2000x2000、重新编码
		avatarData, ext, err := minio.ProcessAvatar(fileHeader)
		if err != nil {
			log.Printf("处理头像图片失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "处理头像图片失败",
			})
			return
		}

		// 上传处理后的头像
		objectName, finalFileSize, err = minio.UploadAvatar(user.ID, avatarData, ext)
		if err != nil {
			log.Printf("上传头像失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "上传头像失败",
			})
			return
		}

		// 更新用户头像字段
		userRepo.UpdateUserAvatar(user.ID, objectName)

		// 生成240x240缩略图
		thumbObjectName, thumbData, err := minio.GenerateThumbnail(objectName, 240, 240, ".jpg")
		if err != nil {
			log.Printf("生成缩略图失败: %v", err)
			// 缩略图生成失败不影响，继续处理
		} else {
			contentType := "image/jpeg"
			if err := minio.UploadThumbnail(thumbObjectName, thumbData, contentType); err != nil {
				log.Printf("上传缩略图失败: %v", err)
			} else {
				userRepo.UpdateUserAvatarThumb(user.ID, thumbObjectName)
				thumbnailURL = minio.GetFileURL(thumbObjectName)
			}
		}
	} else {
		// 其他文件类型：直接上传
		objectName, finalFileSize, err = minio.UploadMultipartFile(fileHeader, user.ID, fileType)
		if err != nil {
			log.Printf("上传文件失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "上传文件失败",
			})
			return
		}
	}

	// 生成访问URL
	fileURL := minio.GetFileURL(objectName)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "上传成功",
		"data": UploadResponse{
			FileName:     fileHeader.Filename,
			FileSize:     finalFileSize,
			FileType:     fileType,
			MinioPath:    objectName,
			FileURL:      fileURL,
			ThumbnailURL: thumbnailURL,
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
	// 严格校验 Content-Type，只要不在白名单中立即拦截
	// 移除了对空字符串的宽容处理，封堵空 Header 绕过漏洞
	if !allowedTypes[contentType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "非法的��件类型，只支持图片和PDF文件",
		})
		return
	}

	// 检查用户是否已有待审核的操作证
	certRepo := gormdb.NewOperatorCertRepository()
	pendingCert, err := certRepo.GetPendingByUserID(user.ID)

	var oldMinioPath string
	if pendingCert != nil {
		// 用户已有待审核的操作证，记录旧文件路径用于删除
		oldMinioPath = pendingCert.MinioPath
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

	var cert *gormdb.OperatorCert
	if pendingCert != nil {
		// 替换待审核记录
		cert, err = certRepo.UpdatePendingCert(pendingCert.ID, fileHeader.Filename, bucket, objectName, fileSize, contentType)
		if err != nil {
			log.Printf("更新操作证记录失败: %v", err)
			// 删除刚上传的文件
			minio.DeleteFile(c.Request.Context(), objectName)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "更新操作证记录失败",
			})
			return
		}
		// 删除旧文件
		if oldMinioPath != "" {
			if err := minio.DeleteFile(c.Request.Context(), oldMinioPath); err != nil {
				log.Printf("删除旧操作证文件失败: %v", err)
			}
		}
	} else {
		// 创建新的待审核操作证记录
		cert, err = certRepo.CreatePendingCert(user.ID, fileHeader.Filename, bucket, objectName, fileSize, contentType)
		if err != nil {
			log.Printf("保存操作证记录失败: %v", err)
			// 删除刚上传的文件
			minio.DeleteFile(c.Request.Context(), objectName)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "保存操作证记录失败",
			})
			return
		}
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

	// 返回两个操作证：active_cert 和 pending_cert
	certRepo := gormdb.NewOperatorCertRepository()

	// 获取当前有效的操作证（已通过）
	activeCert, _ := certRepo.GetActiveByUserID(user.ID)

	// 获取待审核或被拒绝的最新操作证
	pendingCert, _ := certRepo.GetPendingByUserID(user.ID)
	if pendingCert == nil {
		// 获取最新的操作证（可能是被拒绝的）
		latestCert, _ := certRepo.GetLatestByUserID(user.ID)
		// 只有当最新操作证不是已通过状态时才返回（避免重复��回 activeCert）
		if latestCert != nil && latestCert.Status != 1 {
			pendingCert = latestCert
		}
	}

	// 构建响应
	response := gin.H{
		"active_cert":  nil,
		"pending_cert": nil,
	}

	if activeCert != nil {
		response["active_cert"] = gin.H{
			"id":          activeCert.ID,
			"file_name":   activeCert.FileName,
			"file_size":   activeCert.FileSize,
			"file_type":   activeCert.FileType,
			"upload_time": activeCert.UploadTime.Format("2006-01-02 15:04:05"),
			"file_url":    minio.GetFileURL(activeCert.MinioPath),
			"status":      activeCert.Status,
			"review_note": activeCert.ReviewNote,
		}
	}

	if pendingCert != nil {
		response["pending_cert"] = gin.H{
			"id":          pendingCert.ID,
			"file_name":   pendingCert.FileName,
			"file_size":   pendingCert.FileSize,
			"file_type":   pendingCert.FileType,
			"upload_time": pendingCert.UploadTime.Format("2006-01-02 15:04:05"),
			"file_url":    minio.GetFileURL(pendingCert.MinioPath),
			"status":      pendingCert.Status,
			"review_note": pendingCert.ReviewNote,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    response,
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

	// 根据状态获取用户列表（包含所有操作证）
	certRepo := gormdb.NewOperatorCertRepository()
	var userWithCerts []*gormdb.UserWithCerts
	var total int64

	if status == 0 {
		// 待审核：从用户表获取所有 approval_status=0 的用户
		userList, err := userRepo.ListByApprovalStatus(0, limit, (page-1)*limit)
		if err != nil {
			log.Printf("获取待审批用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "获取待审批用户失败",
			})
			return
		}

		// 获取总数
		count, _ := userRepo.CountByApprovalStatus(0)
		total = count

		// 【性能优化】收集用户 ID 并批量查询证书
		var userIDs []int
		for _, u := range userList {
			userIDs = append(userIDs, u.ID)
		}

		// 利用 IN 语句批量查询所有用户的证书
		var allCerts []*gormdb.OperatorCert
		if len(userIDs) > 0 {
			gormdb.Get().Where("user_id IN ?", userIDs).Order("id DESC").Find(&allCerts)
		}

		// 在内存中按 userID 进行映射组装
		certMap := make(map[int][]*gormdb.OperatorCert)
		for _, cert := range allCerts {
			certMap[cert.UserID] = append(certMap[cert.UserID], cert)
		}

		// 转换为UserWithCerts格式
		userWithCerts = make([]*gormdb.UserWithCerts, 0, len(userList))
		for _, u := range userList {
			userWithCerts = append(userWithCerts, &gormdb.UserWithCerts{
				User:  u,
				Certs: certMap[u.ID],
			})
		}
	} else if status == 2 {
		// 已拒绝：有被拒绝操作证的用户
		userWithCerts, total, err = certRepo.ListRejectedWithCerts(limit, (page-1)*limit)
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

		// 【性能优化 步骤1】收集当前这一批查询结果的所有用户 ID
		var userIDs []int
		for _, u := range userList {
			userIDs = append(userIDs, u.ID)
		}

		// 【性能优化 步骤2】利用 IN 语句，使用 1 条 SQL 查出所有用户的证书
		var allCerts []*gormdb.OperatorCert
		if len(userIDs) > 0 {
			gormdb.Get().Where("user_id IN ?", userIDs).Order("id DESC").Find(&allCerts)
		}

		// 【性能优化 步骤3】在内存中按 userID 进行映射组装
		certMap := make(map[int][]*gormdb.OperatorCert)
		for _, cert := range allCerts {
			certMap[cert.UserID] = append(certMap[cert.UserID], cert)
		}

		userWithCerts = make([]*gormdb.UserWithCerts, 0, len(userList))
		for _, u := range userList {
			// 直接从 HashMap 中提取，摒弃了循环查库
			userWithCerts = append(userWithCerts, &gormdb.UserWithCerts{
				User:  u,
				Certs: certMap[u.ID],
			})
		}
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(userWithCerts))
	for _, uw := range userWithCerts {
		u := uw.User
		certs := uw.Certs

		item := gin.H{
			"id":              u.ID,
			"username":        u.Name,
			"nickname":        u.NickName,
			"callsign":        u.CallSign,
			"phone":           u.Phone,
			"address":         u.Address,
			"approval_status": u.ApprovalStatus,
			"created_at":      u.CreateTime.Format("2006-01-02 15:04:05"),
			"has_cert":        len(certs) > 0,
			"certs":           make([]gin.H, 0, len(certs)), // 新增：所有操作证列表
		}

		// 遍历所有操作证，添加到响应中
		for _, cert := range certs {
			certData := gin.H{
				"id":          cert.ID,
				"file_name":   cert.FileName,
				"file_size":   cert.FileSize,
				"file_type":   cert.FileType,
				"upload_time": cert.UploadTime.Format("2006-01-02 15:04:05"),
				"file_url":    minio.GetFileURL(cert.MinioPath),
				"status":      cert.Status,
				"review_note": cert.ReviewNote,
			}
			if cert.ReviewTime != nil {
				certData["review_time"] = cert.ReviewTime.Format("2006-01-02 15:04:05")
			}
			if cert.ReviewerID != nil {
				certData["reviewer_id"] = *cert.ReviewerID
			}
			item["certs"] = append(item["certs"].([]gin.H), certData)
		}

		// 保留兼容性：cert 字段指向最新或指定状态的证书
		var targetCert *gormdb.OperatorCert
		if status == 0 {
			targetCert, _ = certRepo.GetPendingByUserID(u.ID)
		} else if status == 2 {
			targetCert, _ = certRepo.GetLatestByUserID(u.ID)
		} else {
			targetCert, _ = certRepo.GetActiveByUserID(u.ID)
		}

		if targetCert != nil {
			item["cert"] = gin.H{
				"id":          targetCert.ID,
				"file_name":   targetCert.FileName,
				"file_size":   targetCert.FileSize,
				"file_type":   targetCert.FileType,
				"upload_time": targetCert.UploadTime.Format("2006-01-02 15:04:05"),
				"file_url":    minio.GetFileURL(targetCert.MinioPath),
				"status":      targetCert.Status,
				"review_note": targetCert.ReviewNote,
			}
		}

		// 审核时间和审核人：优先使用目标证书的，否则使用用户的
		var reviewTime *time.Time
		var reviewerID *int
		var reviewNote string

		if targetCert != nil && targetCert.ReviewTime != nil {
			reviewTime = targetCert.ReviewTime
			reviewerID = targetCert.ReviewerID
			reviewNote = targetCert.ReviewNote
		} else {
			reviewTime = u.ReviewTime
			reviewerID = u.ReviewerID
			reviewNote = u.ReviewNote
		}

		if reviewTime != nil {
			item["review_time"] = reviewTime.Format("2006-01-02 15:04:05")
		}
		if reviewerID != nil {
			item["reviewer_id"] = *reviewerID
		}
		item["review_note"] = reviewNote

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

	// 记录审计日志
	oplog.AddLog(
		fmt.Sprintf("审批用户: %s (%s) - %s (备注: %s)", targetUser.Name, targetUser.CallSign, statusText, req.Note),
		"user_approval",
		currentUser.ID,
		currentUser.Name,
		currentUser.CallSign,
		c.ClientIP(),
	)

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

// GetCertificateApprovals 获取操作证审批列表（管理员）
func GetCertificateApprovals(c *gin.Context) {
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

	// 获取分页参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	// 获取状态参数：0=待审核, 1=已通过, 2=已拒绝, -1=全部
	status, _ := strconv.Atoi(c.DefaultQuery("status", "-1"))

	certRepo := gormdb.NewOperatorCertRepository()
	approvals, total, err := certRepo.ListCertificateApprovals(status, limit, (page-1)*limit)
	if err != nil {
		log.Printf("获取操作证审批列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取操作证审批列表失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(approvals))
	for _, a := range approvals {
		// is_replaced: status=3 表示被新证替换（之前是通过的）
		// 直接使用 a.IsReplaced，它已经在 repo 层正确计算
		isReplaced := a.IsReplaced

		item := gin.H{
			"id":          a.ID,
			"user_id":     a.UserID,
			"username":    a.UserName,
			"nickname":    a.NickName,
			"callsign":    a.CallSign,
			"file_name":   a.FileName,
			"file_size":   a.FileSize,
			"file_type":   a.FileType,
			"upload_time": a.UploadTime.Format("2006-01-02 15:04:05"),
			"file_url":    minio.GetFileURL(a.MinioPath),
			"status":      a.Status,
			"review_note": a.ReviewNote,
			"is_update":   a.IsUpdate,
			"is_replaced": isReplaced,
		}
		if a.ReviewTime != nil {
			item["review_time"] = a.ReviewTime.Format("2006-01-02 15:04:05")
		}
		if a.ReviewerID != nil {
			item["reviewer_id"] = *a.ReviewerID
		}
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
