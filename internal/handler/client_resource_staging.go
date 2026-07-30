package handler

import (
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

const clientResourceStagingGrantExpiry = 15 * time.Minute

type clientResourceStagingItem struct {
	ObjectKey   string `json:"object_key"`
	FileName    string `json:"file_name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
}

type clientResourceStagingRetryRequest struct {
	ObjectKey string `json:"object_key" binding:"required"`
}

type clientResourceStagingRetryResponse struct {
	ObjectKey   string    `json:"object_key"`
	FileName    string    `json:"file_name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type,omitempty"`
	UploadToken string    `json:"upload_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ListClientResourceStaging lists the current administrator's completed
// staging objects. It is intentionally scoped by the user ID embedded in the
// key; another administrator's upload cannot be adopted accidentally.
func ListClientResourceStaging(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	if !storage.IsEnabled() {
		writeClientResourceError(c, http.StatusServiceUnavailable, "存储服务不可用")
		return
	}
	prefix := fmt.Sprintf("staging/client_resource/%d/", user.ID)
	items := make([]clientResourceStagingItem, 0)
	if err := storage.Get().Walk(c.Request.Context(), prefix, func(object storage.ObjectInfo) error {
		if !storage.IsStagingObjectKey(object.Key, "client_resource", user.ID) || object.Size <= 0 {
			return nil
		}
		contentType := ""
		if _, statType, err := storage.Stat(c.Request.Context(), object.Key); err == nil {
			contentType = statType
		}
		items = append(items, clientResourceStagingItem{
			ObjectKey: object.Key, FileName: path.Base(object.Key), Size: object.Size, ContentType: contentType,
		})
		return nil
	}); err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "读取待完成上传失败")
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ObjectKey > items[j].ObjectKey })
	writeClientResourceSuccess(c, "成功", gin.H{"items": items, "total": len(items)})
}

// RetryClientResourceStaging issues a fresh completion grant for an existing
// staging object. It never promotes or deletes the object, so a page refresh
// can safely resume the normal artifact-complete flow.
func RetryClientResourceStaging(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	if !storage.IsEnabled() {
		writeClientResourceError(c, http.StatusServiceUnavailable, "存储服务不可用")
		return
	}
	var req clientResourceStagingRetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	objectKey := strings.TrimLeft(strings.TrimSpace(req.ObjectKey), "/")
	if !storage.IsStagingObjectKey(objectKey, "client_resource", user.ID) {
		writeClientResourceError(c, http.StatusBadRequest, "非法的 staging 对象")
		return
	}
	size, contentType, err := storage.Stat(c.Request.Context(), objectKey)
	if err != nil {
		writeClientResourceError(c, http.StatusNotFound, "staging 对象不存在")
		return
	}
	maxSize := storage.MaxSizeForFileType("client_resource")
	if size <= 0 || size > maxSize {
		writeClientResourceError(c, http.StatusBadRequest, "staging 对象大小无效或超过限制")
		return
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}
	token, err := storage.CreateUploadGrant(objectKey, "client_resource", user.ID, size, contentType, clientResourceStagingGrantExpiry)
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "生成重试凭证失败")
		return
	}
	writeClientResourceSuccess(c, "已生成新的完成凭证", clientResourceStagingRetryResponse{
		ObjectKey: objectKey, FileName: path.Base(objectKey), Size: size, ContentType: contentType,
		UploadToken: token, ExpiresAt: time.Now().Add(clientResourceStagingGrantExpiry).UTC(),
	})
}
