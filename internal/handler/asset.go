package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"
	"draarl/pkg/minio"

	"github.com/gin-gonic/gin"
)

// AssetHandler 资源管理处理器
type AssetHandler struct {
	repo       *gormdb.AssetRepository
	assetCache *cache.AssetCache
}

// NewAssetHandler 创建资源管理处理器
func NewAssetHandler() *AssetHandler {
	return &AssetHandler{
		repo:       gormdb.GetAssetRepo(),
		assetCache: cache.GetAssetCache(),
	}
}

// AssetResponse 资源响应结构
type AssetResponse struct {
	ID          uint   `json:"id"`
	ParentID    *uint  `json:"parent_id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type,omitempty"`
	Remark      string `json:"remark,omitempty"`
	SortOrder   int    `json:"sort_order"`
	FileCount   int64  `json:"file_count,omitempty"`   // 文件夹下的文件数
	FolderCount int64  `json:"folder_count,omitempty"` // 文件夹下的子文件夹数
	DownloadURL string `json:"download_url,omitempty"` // 文件下载链接
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CreateFolderRequest 创建文件夹请求
type CreateFolderRequest struct {
	Name     string `json:"name" binding:"required"`
	ParentID *uint  `json:"parent_id"`
	Remark   string `json:"remark"`
}

// UpdateAssetRequest 更新资源请求
type UpdateAssetRequest struct {
	Name      string  `json:"name"`
	Remark    *string `json:"remark"`
	SortOrder *int    `json:"sort_order"`
}

// MoveAssetRequest 移动资源请求
type MoveAssetRequest struct {
	TargetParentID *uint `json:"target_parent_id"` // null 表示移动到根目录
}

// invalidateAssetCache 使资源缓存失效
// parentID 为 nil 时表示根目录，需要刷新目录树缓存
func (h *AssetHandler) invalidateAssetCache(ctx interface{ Done() <-chan struct{} }, parentID *uint) {
	if h.assetCache == nil {
		return
	}

	// 转换 context
	var goCtx context.Context
	if c, ok := ctx.(context.Context); ok {
		goCtx = c
	} else {
		goCtx = context.Background()
	}

	// 如果是根目录下的变更，需要刷新目录树缓存
	if parentID == nil {
		_ = h.assetCache.InvalidateTree(goCtx)
	}

	// 刷新父文件夹的内容缓存
	if parentID != nil {
		_ = h.assetCache.InvalidateFolder(goCtx, *parentID)
	} else {
		// 根目录变更，清除所有文件夹缓存
		_ = h.assetCache.InvalidateAll(goCtx)
	}
}

// GetAssets 获取资源列表
// GET /api/admin/assets?parent_id=xx
func (h *AssetHandler) GetAssets(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	var parentID *uint
	if parentIDStr := c.Query("parent_id"); parentIDStr != "" {
		id, err := strconv.ParseUint(parentIDStr, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "无效的 parent_id",
			})
			return
		}
		uid := uint(id)
		parentID = &uid
	}

	assets, err := h.repo.GetByParentID(parentID)
	if err != nil {
		log.Printf("获取资源列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取资源列表失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]AssetResponse, 0, len(assets))
	for _, asset := range assets {
		item := AssetResponse{
			ID:        asset.ID,
			ParentID:  asset.ParentID,
			Name:      asset.Name,
			Type:      asset.Type,
			Size:      asset.Size,
			Remark:    asset.Remark,
			SortOrder: asset.SortOrder,
			CreatedAt: asset.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: asset.UpdatedAt.Format("2006-01-02 15:04:05"),
		}

		// 如果是文件，添加下载链接
		if asset.IsFile() {
			item.Path = asset.Path
			item.MimeType = asset.MimeType
			item.DownloadURL = minio.GetFileURL(asset.Path)
		}

		// 如果是文件夹，添加子项数量
		if asset.IsFolder() {
			item.FileCount, _ = h.repo.GetFileCount(asset.ID)
			item.FolderCount, _ = h.repo.GetSubFolderCount(asset.ID)
		}

		items = append(items, item)
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    items,
	})
}

// CreateFolder 创建文件夹
// POST /api/admin/assets/folder
func (h *AssetHandler) CreateFolder(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	var req CreateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 检查同名资源是否存在
	exists2, err := h.repo.ExistsByName(req.Name, req.ParentID)
	if err != nil {
		log.Printf("检查资源是否存在失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "创建文件夹失败",
		})
		return
	}
	if exists2 {
		c.JSON(http.StatusConflict, Response{
			Code:    409,
			Message: "同名资源已存在",
		})
		return
	}

	// 创建文件夹
	folder := &gormdb.Asset{
		ParentID: req.ParentID,
		Name:     req.Name,
		Type:     "folder",
		Remark:   req.Remark,
	}

	if err := h.repo.Create(folder); err != nil {
		log.Printf("创建文件夹失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "创建文件夹失败",
		})
		return
	}

	// 记录操作日志
	parentPath := "根目录"
	if req.ParentID != nil {
		parentPath, _ = h.repo.GetPath(*req.ParentID)
	}
	oplog.AddLog(
		fmt.Sprintf("创建文件夹: %s (位置: %s)", req.Name, parentPath),
		"asset_create",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效
	h.invalidateAssetCache(c.Request.Context(), req.ParentID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "创建成功",
		Data: AssetResponse{
			ID:        folder.ID,
			ParentID:  folder.ParentID,
			Name:      folder.Name,
			Type:      folder.Type,
			Remark:    folder.Remark,
			SortOrder: folder.SortOrder,
			CreatedAt: folder.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: folder.UpdatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// UploadFile 上传文件
// POST /api/admin/assets/upload
func (h *AssetHandler) UploadFile(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	// 获取父目录ID
	parentIDStr := c.PostForm("parent_id")
	if parentIDStr == "" {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "缺少 parent_id 参数",
		})
		return
	}
	parentIDUint, err := strconv.ParseUint(parentIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的 parent_id",
		})
		return
	}
	parentID := uint(parentIDUint)

	// 验证父目录存在且是文件夹
	parent, err := h.repo.GetByID(parentID)
	if err != nil || parent == nil || !parent.IsFolder() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "父目录不存在或不是文件夹",
		})
		return
	}

	// 获取上传的文件
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "获取文件失败",
		})
		return
	}

	// 检查文件大小（100MB）
	if fileHeader.Size > 100*1024*1024 {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "文件大小不能超过100MB",
		})
		return
	}

	// 获取显示名称
	displayName := c.PostForm("name")
	if displayName == "" {
		displayName = fileHeader.Filename
	}

	// 获取备注
	remark := c.PostForm("remark")

	// 检查同名资源是否存在
	exists2, err := h.repo.ExistsByName(displayName, &parentID)
	if err != nil {
		log.Printf("检查资源是否存在失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "上传文件失败",
		})
		return
	}
	if exists2 {
		c.JSON(http.StatusConflict, Response{
			Code:    409,
			Message: "同名资源已存在",
		})
		return
	}

	// 上传文件到 MinIO
	objectName, fileSize, err := minio.UploadMultipartFile(fileHeader, int(userModel.ID), "assets")
	if err != nil {
		log.Printf("上传文件到MinIO失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "上传文件失败",
		})
		return
	}

	// 获取 MIME 类型
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// 创建资源记录
	asset := &gormdb.Asset{
		ParentID: &parentID,
		Name:     displayName,
		Type:     "file",
		Path:     objectName,
		Size:     fileSize,
		MimeType: mimeType,
		Remark:   remark,
	}

	if err := h.repo.Create(asset); err != nil {
		// 回滚：删除已上传的文件
		minio.DeleteFile(c.Request.Context(), objectName)
		log.Printf("创建资源记录失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "上传文件失败",
		})
		return
	}

	// 记录操作日志
	parentPath, _ := h.repo.GetPath(parentID)
	oplog.AddLog(
		fmt.Sprintf("上传文件: %s (位置: %s, 大小: %d 字节)", displayName, parentPath, fileSize),
		"asset_upload",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效
	h.invalidateAssetCache(c.Request.Context(), &parentID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "上传成功",
		Data: AssetResponse{
			ID:          asset.ID,
			ParentID:    asset.ParentID,
			Name:        asset.Name,
			Type:        asset.Type,
			Path:        asset.Path,
			Size:        asset.Size,
			MimeType:    asset.MimeType,
			Remark:      asset.Remark,
			SortOrder:   asset.SortOrder,
			DownloadURL: minio.GetFileURL(asset.Path),
			CreatedAt:   asset.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:   asset.UpdatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// UpdateAsset 更新资源（重命名、备注）
// PUT /api/admin/assets/:id
func (h *AssetHandler) UpdateAsset(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	// 获取资源ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的资源ID",
		})
		return
	}

	// 获取资源
	asset, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "资源不存在",
		})
		return
	}

	var req UpdateAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 构建更新数据
	updates := make(map[string]interface{})
	if req.Name != "" && req.Name != asset.Name {
		// 检查新名称是否已存在
		exists2, err := h.repo.ExistsByName(req.Name, asset.ParentID)
		if err != nil {
			log.Printf("检查资源名称失败: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "更新资源失败",
			})
			return
		}
		if exists2 {
			c.JSON(http.StatusConflict, Response{
				Code:    409,
				Message: "同名资源已存在",
			})
			return
		}
		updates["name"] = req.Name
	}
	if req.Remark != nil {
		updates["remark"] = *req.Remark
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "没有需要更新的内容",
		})
		return
	}

	// 执行更新
	if err := h.repo.UpdatePartial(uint(id), updates); err != nil {
		log.Printf("更新资源失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "更新资源失败",
		})
		return
	}

	// 记录操作日志
	oldName := asset.Name
	if req.Name != "" {
		oldName = fmt.Sprintf("%s -> %s", asset.Name, req.Name)
	}
	oplog.AddLog(
		fmt.Sprintf("更新资源: %s (类型: %s)", oldName, asset.Type),
		"asset_update",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效
	h.invalidateAssetCache(c.Request.Context(), asset.ParentID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// MoveAsset 移动资源
// PUT /api/admin/assets/:id/move
func (h *AssetHandler) MoveAsset(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	// 获取资源ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的资源ID",
		})
		return
	}

	// 获取资源
	asset, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "资源不存在",
		})
		return
	}

	var req MoveAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 验证目标父目录
	if req.TargetParentID != nil {
		targetParent, err := h.repo.GetByID(*req.TargetParentID)
		if err != nil || targetParent == nil || !targetParent.IsFolder() {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "目标目录不存在或不是文件夹",
			})
			return
		}
	}

	// 验证父目录有效性（不能移动到自己或子目录下）
	if asset.IsFolder() {
		if err := h.repo.ValidateParent(uint(id), req.TargetParentID); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: err.Error(),
			})
			return
		}
	}

	// 检查目标目录下是否已存在同名资源
	exists2, err := h.repo.ExistsByName(asset.Name, req.TargetParentID)
	if err != nil {
		log.Printf("检查资源名称失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "移动资源失败",
		})
		return
	}
	if exists2 {
		c.JSON(http.StatusConflict, Response{
			Code:    409,
			Message: "目标目录下已存在同名资源",
		})
		return
	}

	// 执行移动
	if err := h.repo.Move(uint(id), req.TargetParentID); err != nil {
		log.Printf("移动资源失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "移动资源失败",
		})
		return
	}

	// 记录操作日志
	targetPath := "根目录"
	if req.TargetParentID != nil {
		targetPath, _ = h.repo.GetPath(*req.TargetParentID)
	}
	oplog.AddLog(
		fmt.Sprintf("移动资源: %s -> %s", asset.Name, targetPath),
		"asset_move",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效（源目录和目标目录都需要刷新）
	ctx := c.Request.Context()
	h.invalidateAssetCache(ctx, asset.ParentID)
	if req.TargetParentID != nil && (asset.ParentID == nil || *req.TargetParentID != *asset.ParentID) {
		h.invalidateAssetCache(ctx, req.TargetParentID)
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "移动成功",
	})
}

// ReplaceFile 覆盖文件
// POST /api/admin/assets/:id/replace
func (h *AssetHandler) ReplaceFile(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	// 获取资源ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的资源ID",
		})
		return
	}

	// 获取资源
	asset, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "资源不存在",
		})
		return
	}

	// 验证是文件类型
	if !asset.IsFile() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "只能覆盖文件类型的资源",
		})
		return
	}

	// 获取上传的文件
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "获取文件失败",
		})
		return
	}

	// 检查文件大小（100MB）
	if fileHeader.Size > 100*1024*1024 {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "文件大小不能超过100MB",
		})
		return
	}

	// 保存旧文件路径用于删除
	oldPath := asset.Path

	// 上传新文件到 MinIO
	objectName, fileSize, err := minio.UploadMultipartFile(fileHeader, int(userModel.ID), "assets")
	if err != nil {
		log.Printf("上传文件到MinIO失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "上传文件失败",
		})
		return
	}

	// 获取 MIME 类型
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// 更新资源记录
	updates := map[string]interface{}{
		"path":      objectName,
		"size":      fileSize,
		"mime_type": mimeType,
	}
	if err := h.repo.UpdatePartial(uint(id), updates); err != nil {
		// 回滚：删除新上传的文件
		minio.DeleteFile(c.Request.Context(), objectName)
		log.Printf("更新资源记录失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "覆盖文件失败",
		})
		return
	}

	// 删除旧文件
	minio.DeleteFile(c.Request.Context(), oldPath)

	// 记录操作日志
	oplog.AddLog(
		fmt.Sprintf("覆盖文件: %s (旧大小: %d, 新大小: %d)", asset.Name, asset.Size, fileSize),
		"asset_replace",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效
	h.invalidateAssetCache(c.Request.Context(), asset.ParentID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "覆盖成功",
		Data: AssetResponse{
			ID:          asset.ID,
			ParentID:    asset.ParentID,
			Name:        asset.Name,
			Type:        asset.Type,
			Path:        objectName,
			Size:        fileSize,
			MimeType:    mimeType,
			Remark:      asset.Remark,
			SortOrder:   asset.SortOrder,
			DownloadURL: minio.GetFileURL(objectName),
			CreatedAt:   asset.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:   asset.UpdatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

// DeleteAsset 删除资源
// DELETE /api/admin/assets/:id
func (h *AssetHandler) DeleteAsset(c *gin.Context) {
	// 路由已通过 RequireAdmin 中间件验证权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}
	userModel := user.(*gormdb.User)

	// 获取资源ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的资源ID",
		})
		return
	}

	// 获取资源及其所有子资源
	asset, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "资源不存在",
		})
		return
	}

	// 收集所有需要删除的文件路径
	var filePaths []string
	collectFilePaths := func(assets []gormdb.Asset) {
		for _, a := range assets {
			if a.IsFile() && a.Path != "" {
				filePaths = append(filePaths, a.Path)
			}
		}
	}

	// 如果是文件夹，获取所有子资源
	if asset.IsFolder() {
		children, err := h.repo.GetAllChildrenRecursive(uint(id))
		if err != nil {
			log.Printf("获取子资源失败: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "删除资源失败",
			})
			return
		}
		collectFilePaths(children)
	}
	// 添加当前资源（如果是文件）
	if asset.IsFile() && asset.Path != "" {
		filePaths = append(filePaths, asset.Path)
	}

	// 从数据库删除资源（包括子资源）
	if err := h.repo.DeleteWithChildren(uint(id)); err != nil {
		log.Printf("删除资源失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "删除资源失败",
		})
		return
	}

	// 从 MinIO 删除所有文件
	for _, path := range filePaths {
		if err := minio.DeleteFile(c.Request.Context(), path); err != nil {
			log.Printf("删除MinIO文件失败: %s, %v", path, err)
			// 不中断流程，继续删除其他文件
		}
	}

	// 记录操作日志
	assetType := "文件"
	if asset.IsFolder() {
		assetType = fmt.Sprintf("文件夹 (包含 %d 个子项)", len(filePaths))
	}
	oplog.AddLog(
		fmt.Sprintf("删除%s: %s", assetType, asset.Name),
		"asset_delete",
		userModel.ID,
		userModel.Name,
		userModel.CallSign,
		c.ClientIP(),
	)

	// 使缓存失效
	h.invalidateAssetCache(c.Request.Context(), asset.ParentID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "删除成功",
	})
}

// GetAssetTree 获取资源目录树（前台公开接口）
// GET /api/assets/tree
func (h *AssetHandler) GetAssetTree(c *gin.Context) {
	// 尝试从缓存获取
	if h.assetCache != nil {
		items, err := h.assetCache.GetAssetTree(c.Request.Context())
		if err == nil && len(items) > 0 {
			// 转换为响应格式
			responses := make([]AssetResponse, 0, len(items))
			for _, item := range items {
				responses = append(responses, AssetResponse{
					ID:        item.ID,
					Name:      item.Name,
					Type:      item.Type,
					Remark:    item.Remark,
					SortOrder: item.SortOrder,
					FileCount: item.FileCount,
					CreatedAt: item.CreatedAt,
					UpdatedAt: item.UpdatedAt,
				})
			}
			c.JSON(http.StatusOK, Response{
				Code:    200,
				Message: "获取成功",
				Data:    responses,
			})
			return
		}
	}

	// 缓存未命中，从数据库获取
	folders, err := h.repo.GetRootFolders()
	if err != nil {
		log.Printf("获取资源目录树失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "获取资源目录树失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]AssetResponse, 0, len(folders))
	for _, folder := range folders {
		fileCount, _ := h.repo.GetFileCountRecursive(folder.ID)
		items = append(items, AssetResponse{
			ID:        folder.ID,
			Name:      folder.Name,
			Type:      folder.Type,
			Remark:    folder.Remark,
			SortOrder: folder.SortOrder,
			FileCount: fileCount,
			CreatedAt: folder.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: folder.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    items,
	})
}

// GetFolderFiles 获取文件夹下的文件列表（前台公开接口）
// GET /api/assets/folder/:id
func (h *AssetHandler) GetFolderFiles(c *gin.Context) {
	// 获取文件夹ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的文件夹ID",
		})
		return
	}

	// 验证文件夹存在
	folder, err := h.repo.GetByID(uint(id))
	if err != nil || folder == nil || !folder.IsFolder() {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "文件夹不存在",
		})
		return
	}

	// 尝试从缓存获取
	var items []AssetResponse
	if h.assetCache != nil {
		cachedItems, err := h.assetCache.GetFolderFiles(c.Request.Context(), uint(id))
		if err == nil && len(cachedItems) > 0 {
			// 转换为响应格式
			items = make([]AssetResponse, 0, len(cachedItems))
			for _, child := range cachedItems {
				item := AssetResponse{
					ID:        child.ID,
					Name:      child.Name,
					Type:      child.Type,
					Size:      child.Size,
					MimeType:  child.MimeType,
					Remark:    child.Remark,
					SortOrder: child.SortOrder,
					FileCount: child.FileCount,
					CreatedAt: child.CreatedAt,
					UpdatedAt: child.UpdatedAt,
				}
				// 如果是文件，添加下载链接
				if child.Type == "file" {
					item.DownloadURL = minio.GetFileURL(child.Path)
				}
				items = append(items, item)
			}
		}
	}

	// 缓存未命中，从数据库获取
	if len(items) == 0 {
		children, err := h.repo.GetChildrenByParentID(uint(id))
		if err != nil {
			log.Printf("获取内容列表失败: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "获取内容列表失败",
			})
			return
		}

		// 转换为响应格式
		items = make([]AssetResponse, 0, len(children))
		for _, child := range children {
			item := AssetResponse{
				ID:        child.ID,
				Name:      child.Name,
				Type:      child.Type,
				Size:      child.Size,
				MimeType:  child.MimeType,
				Remark:    child.Remark,
				SortOrder: child.SortOrder,
				CreatedAt: child.CreatedAt.Format("2006-01-02 15:04:05"),
				UpdatedAt: child.UpdatedAt.Format("2006-01-02 15:04:05"),
			}

			// 如果是文件夹，递归获取子文件数量
			if child.IsFolder() {
				fileCount, _ := h.repo.GetFileCountRecursive(child.ID)
				item.FileCount = fileCount
			} else if child.IsFile() {
				// 如果是文件，添加下载链接
				item.DownloadURL = minio.GetFileURL(child.Path)
			}

			items = append(items, item)
		}
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data: gin.H{
			"folder": AssetResponse{
				ID:        folder.ID,
				Name:      folder.Name,
				Remark:    folder.Remark,
				CreatedAt: folder.CreatedAt.Format("2006-01-02 15:04:05"),
			},
			"files": items,
		},
	})
}

// GetDownloadURL 获取文件下载链接（前台公开接口）
// GET /api/assets/:id/download
func (h *AssetHandler) GetDownloadURL(c *gin.Context) {
	// 获取资源ID
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的资源ID",
		})
		return
	}

	// 获取资源
	asset, err := h.repo.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "资源不存在",
		})
		return
	}

	// 验证是文件类型
	if !asset.IsFile() {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "只能下载文件类型的资源",
		})
		return
	}

	// 生成临时下载链接（7天有效期）
	downloadURL, err := minio.PresignedURL(c.Request.Context(), asset.Path, 7*24*60*60*time.Second)
	if err != nil {
		log.Printf("生成下载链接失败: %v", err)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "生成下载链接失败",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data: gin.H{
			"name":         asset.Name,
			"size":         asset.Size,
			"mime_type":    asset.MimeType,
			"download_url": downloadURL,
		},
	})
}

// FormatFileSize 格式化文件大小显示
func FormatFileSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// IsAllowedMimeType 检查 MIME 类型是否允许上传
func IsAllowedMimeType(mimeType string) bool {
	// 允许的 MIME 类型列表
	allowedTypes := map[string]bool{
		// 图片
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
		"image/bmp":  true,
		// 文档
		"application/pdf":    true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-excel": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
		"application/vnd.ms-powerpoint":                                             true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		// 压缩包
		"application/zip":              true,
		"application/x-zip-compressed": true,
		"application/x-rar-compressed": true,
		"application/x-7z-compressed":  true,
		// 可执行文件（客户端安装包等）
		"application/octet-stream": true,
		"application/x-msdownload": true,
		"application/x-dosexec":    true,
		// 音频
		"audio/mpeg": true,
		"audio/wav":  true,
		"audio/ogg":  true,
		"audio/flac": true,
		// 视频
		"video/mp4":       true,
		"video/webm":      true,
		"video/x-msvideo": true,
		"video/quicktime": true,
		// 文本
		"text/plain":      true,
		"text/html":       true,
		"text/css":        true,
		"text/javascript": true,
	}

	return allowedTypes[mimeType] || strings.HasPrefix(mimeType, "image/") ||
		strings.HasPrefix(mimeType, "video/") || strings.HasPrefix(mimeType, "audio/")
}
