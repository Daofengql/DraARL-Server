package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"draarl/internal/clientcontract"
	"draarl/internal/clientversion"
	"draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	clientResourceDefaultChannel        = "stable"
	clientResourceFinalizeTimeout       = 30 * time.Minute
	clientResourceDeleteCleanupTimeout  = 2 * time.Minute
	clientResourceMaxKeyLength          = 191
	clientResourceMaxNameLength         = 255
	clientResourceMaxCategoryLength     = 64
	clientResourceMaxDescriptionLength  = 1 << 20
	clientResourceMaxVersionLength      = 64
	clientResourceMaxTitleLength        = 255
	clientResourceMaxChangelogLength    = 1 << 20
	clientResourceMaxFileNameLength     = 255
	clientResourceMaxBuildNumberLength  = 64
	clientResourceMaxExternalURLLength  = 1024
	clientResourceMaxSignatureLength    = 65535
	clientResourceMaxSignatureAlgorithm = 64
	clientResourceMaxMetadataLength     = 64 * 1024
	clientResourceMaxCapabilities       = 32
	clientResourceManifestSchemaVersion = 1
)

var clientResourceDownloadURLExpiry = 15 * time.Minute

var errClientResourceInvalidContractRequirement = errors.New("invalid client resource contract requirement")

var (
	clientResourceKeySegmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	clientResourceSlugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)
	clientResourceTargetPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	clientResourceCapabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

type clientResourceCreateRequest struct {
	ResourceKey string `json:"resource_key" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Enabled     *bool  `json:"enabled"`
}

type clientResourceUpdateRequest struct {
	ResourceKey *string `json:"resource_key"`
	Name        *string `json:"name"`
	Category    *string `json:"category"`
	Description *string `json:"description"`
	Required    *bool   `json:"required"`
	Enabled     *bool   `json:"enabled"`
}

type clientResourceReleaseCreateRequest struct {
	Version                 string   `json:"version" binding:"required"`
	Channel                 string   `json:"channel"`
	Title                   string   `json:"title"`
	Changelog               string   `json:"changelog"`
	ForceUpdate             bool     `json:"force_update"`
	MinClientVersion        string   `json:"min_client_version"`
	MinServerVersion        string   `json:"min_server_version"`
	RequiredProtocolVersion int      `json:"required_protocol_version"`
	RequiredCapabilities    []string `json:"required_capabilities"`
}

type clientResourceArtifactTargetRequest struct {
	Platform      string `json:"platform" binding:"required"`
	Arch          string `json:"arch" binding:"required"`
	MinOSVersion  string `json:"min_os_version"`
	MinAndroidAPI int    `json:"min_android_api"`
}

type clientResourceArtifactCompleteRequest struct {
	Format             string                                `json:"format" binding:"required"`
	Runtime            string                                `json:"runtime"`
	Variant            string                                `json:"variant"`
	BuildNumber        string                                `json:"build_number"`
	FileName           string                                `json:"file_name"`
	ObjectKey          string                                `json:"object_key"`
	UploadToken        string                                `json:"upload_token"`
	ExternalURL        string                                `json:"external_url"`
	ContentSignature   string                                `json:"content_signature"`
	SignatureAlgorithm string                                `json:"signature_algorithm"`
	Metadata           json.RawMessage                       `json:"metadata"`
	Targets            []clientResourceArtifactTargetRequest `json:"targets" binding:"required,min=1"`
}

type clientResourceListResponse struct {
	Items    []clientResourceResponse `json:"items"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

type clientResourceReleaseListResponse struct {
	Items    []clientResourceReleaseResponse `json:"items"`
	Total    int64                           `json:"total"`
	Page     int                             `json:"page"`
	PageSize int                             `json:"page_size"`
}

type clientResourceDeleteResponse struct {
	DeletedReleases       int `json:"deleted_releases"`
	DeletedArtifacts      int `json:"deleted_artifacts"`
	DeletedObjects        int `json:"deleted_objects"`
	ObjectCleanupFailures int `json:"object_cleanup_failures"`
}

type clientResourceReleaseDeleteResponse struct {
	DeletedArtifacts      int `json:"deleted_artifacts"`
	DeletedObjects        int `json:"deleted_objects"`
	ObjectCleanupFailures int `json:"object_cleanup_failures"`
}

type clientResourceResponse struct {
	ID                   int       `json:"id"`
	ResourceKey          string    `json:"resource_key"`
	Name                 string    `json:"name"`
	Category             string    `json:"category,omitempty"`
	Description          string    `json:"description,omitempty"`
	Required             bool      `json:"required"`
	Enabled              bool      `json:"enabled"`
	CurrentStableVersion string    `json:"current_stable_version,omitempty"`
	CurrentBetaVersion   string    `json:"current_beta_version,omitempty"`
	CreatedBy            int       `json:"created_by"`
	CreateTime           time.Time `json:"create_time"`
	UpdateTime           time.Time `json:"update_time"`
}

type clientResourceReleaseResponse struct {
	ID                      int                              `json:"id"`
	ResourceID              int                              `json:"resource_id"`
	Resource                clientResourceSummary            `json:"resource"`
	Version                 string                           `json:"version"`
	Channel                 string                           `json:"channel"`
	Title                   string                           `json:"title"`
	Changelog               string                           `json:"changelog"`
	Status                  string                           `json:"status"`
	ForceUpdate             bool                             `json:"force_update"`
	MinClientVersion        string                           `json:"min_client_version,omitempty"`
	MinServerVersion        string                           `json:"min_server_version,omitempty"`
	RequiredProtocolVersion uint16                           `json:"required_protocol_version,omitempty"`
	RequiredCapabilities    []string                         `json:"required_capabilities,omitempty"`
	PublishedAt             *time.Time                       `json:"published_at,omitempty"`
	CreatedBy               int                              `json:"created_by"`
	CreateTime              time.Time                        `json:"create_time"`
	UpdateTime              time.Time                        `json:"update_time"`
	Artifacts               []clientResourceArtifactResponse `json:"artifacts"`
}

type clientResourceSummary struct {
	ID          int    `json:"id"`
	ResourceKey string `json:"resource_key"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Required    bool   `json:"required"`
}

type clientResourceArtifactTargetResponse struct {
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	MinOSVersion  string `json:"min_os_version,omitempty"`
	MinAndroidAPI int    `json:"min_android_api,omitempty"`
}

type clientResourceArtifactResponse struct {
	ID                 int                                    `json:"id"`
	ReleaseID          int                                    `json:"release_id"`
	Format             string                                 `json:"format"`
	Runtime            string                                 `json:"runtime"`
	Variant            string                                 `json:"variant"`
	BuildNumber        string                                 `json:"build_number,omitempty"`
	FileName           string                                 `json:"file_name"`
	FileSize           int64                                  `json:"file_size"`
	SHA256             string                                 `json:"sha256,omitempty"`
	ContentSignature   string                                 `json:"content_signature,omitempty"`
	SignatureAlgorithm string                                 `json:"signature_algorithm,omitempty"`
	ExternalURL        string                                 `json:"external_url,omitempty"`
	StorageKey         string                                 `json:"storage_key,omitempty"`
	Metadata           json.RawMessage                        `json:"metadata,omitempty"`
	Targets            []clientResourceArtifactTargetResponse `json:"targets"`
}

type clientResourceManifestResponse struct {
	SchemaVersion   int                          `json:"schema_version"`
	ServerVersion   string                       `json:"server_version"`
	ProtocolVersion uint16                       `json:"protocol_version"`
	Capabilities    []string                     `json:"capabilities"`
	Resources       []clientResourceManifestItem `json:"resources"`
}

type clientResourceManifestItem struct {
	Resource  clientResourceSummary            `json:"resource"`
	Release   clientResourceManifestRelease    `json:"release"`
	Artifacts []clientResourceArtifactResponse `json:"artifacts"`
}

type clientResourceManifestRelease struct {
	ID                      int        `json:"id"`
	Version                 string     `json:"version"`
	Channel                 string     `json:"channel"`
	Title                   string     `json:"title,omitempty"`
	Changelog               string     `json:"changelog,omitempty"`
	ForceUpdate             bool       `json:"force_update"`
	MinClientVersion        string     `json:"min_client_version,omitempty"`
	MinServerVersion        string     `json:"min_server_version,omitempty"`
	RequiredProtocolVersion uint16     `json:"required_protocol_version,omitempty"`
	RequiredCapabilities    []string   `json:"required_capabilities,omitempty"`
	PublishedAt             *time.Time `json:"published_at,omitempty"`
}

type clientResourceManifestRequest struct {
	Platform      string
	Arch          string
	Channel       string
	ClientVersion string
	OSVersion     string
	AndroidAPI    int
}

// ListClientResources lists managed resource identities.
func ListClientResources(c *gin.Context) {
	if _, ok := requireClientResourceAdmin(c); !ok {
		return
	}
	page, pageSize := clientResourcePagination(c)
	var enabled *bool
	if raw := strings.TrimSpace(c.Query("enabled")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeClientResourceError(c, http.StatusBadRequest, "enabled 参数无效")
			return
		}
		enabled = &value
	}
	resources, total, err := gormdb.NewClientResourceRepository().ListResources(gormdb.ClientResourceListFilter{
		ResourceKey: c.Query("resource_key"), Name: c.Query("name"), Category: c.Query("category"),
		Enabled: enabled, Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "获取客户端资源失败")
		return
	}
	items := make([]clientResourceResponse, 0, len(resources))
	for _, resource := range resources {
		items = append(items, clientResourceToResponse(resource))
	}
	writeClientResourceSuccess(c, "成功", clientResourceListResponse{Items: items, Total: total, Page: page, PageSize: pageSize})
}

// CreateClientResource registers a selectable resource identity.
func CreateClientResource(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	var req clientResourceCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	key, name, category, description, err := normalizeClientResourceDefinition(req.ResourceKey, req.Name, req.Category, req.Description)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	resource := &gormdb.ClientResource{
		ResourceKey: key, Name: name, Category: category, Description: description,
		Required: req.Required, Enabled: enabled, CreatedBy: user.ID,
	}
	if err := gormdb.NewClientResourceRepository().CreateResource(resource); err != nil {
		if gormdb.IsDuplicateKeyError(err) {
			writeClientResourceError(c, http.StatusConflict, "resource_key 已存在")
			return
		}
		writeClientResourceError(c, http.StatusInternalServerError, "创建客户端资源失败")
		return
	}
	oplog.AddLog(fmt.Sprintf("创建客户端资源: %s (%s)", resource.Name, resource.ResourceKey), "client_resource_create", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientResourceSuccess(c, "客户端资源创建成功", clientResourceToResponse(resource))
}

func GetClientResource(c *gin.Context) {
	if _, ok := requireClientResourceAdmin(c); !ok {
		return
	}
	id, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	resource, err := gormdb.NewClientResourceRepository().GetResourceByID(id)
	if err != nil {
		writeClientResourceRepositoryError(c, err, "获取客户端资源失败")
		return
	}
	writeClientResourceSuccess(c, "成功", clientResourceToResponse(resource))
}

func UpdateClientResource(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	id, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	repo := gormdb.NewClientResourceRepository()
	existing, err := repo.GetResourceByID(id)
	if err != nil {
		writeClientResourceRepositoryError(c, err, "获取客户端资源失败")
		return
	}
	var req clientResourceUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	key, name, category, description := existing.ResourceKey, existing.Name, existing.Category, existing.Description
	required, enabled := existing.Required, existing.Enabled
	if req.ResourceKey != nil {
		key = *req.ResourceKey
	}
	if req.Name != nil {
		name = *req.Name
	}
	if req.Category != nil {
		category = *req.Category
	}
	if req.Description != nil {
		description = *req.Description
	}
	if req.Required != nil {
		required = *req.Required
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	key, name, category, description, err = normalizeClientResourceDefinition(key, name, category, description)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := repo.UpdateResource(id, gormdb.ClientResourceUpdate{
		ResourceKey: key, Name: name, Category: category, Description: description, Required: required, Enabled: enabled,
	})
	if err != nil {
		switch {
		case errors.Is(err, gormdb.ErrClientResourceKeyImmutable):
			writeClientResourceError(c, http.StatusConflict, "已有发布记录后不能修改 resource_key")
		case gormdb.IsDuplicateKeyError(err):
			writeClientResourceError(c, http.StatusConflict, "resource_key 已存在")
		default:
			writeClientResourceRepositoryError(c, err, "更新客户端资源失败")
		}
		return
	}
	action := "client_resource_update"
	if existing.Enabled && !updated.Enabled {
		action = "client_resource_disable"
	}
	oplog.AddLog(fmt.Sprintf("更新客户端资源: %s (%s)", updated.Name, updated.ResourceKey), action, user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientResourceSuccess(c, "客户端资源更新成功", clientResourceToResponse(updated))
}

// DeleteClientResource removes a resource identity, all release metadata, and
// every managed object referenced by its artifacts.
func DeleteClientResource(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	resourceID, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	deleted, err := gormdb.NewClientResourceRepository().DeleteResource(resourceID)
	if err != nil {
		writeClientResourceRepositoryError(c, err, "删除客户端资源失败")
		return
	}

	result := cleanupDeletedClientResourceObjects(c, deleted)
	message := "客户端资源及关联版本已删除"
	if result.ObjectCleanupFailures > 0 {
		message = fmt.Sprintf("客户端资源已删除，%d 个对象清理失败，请通过对象审计检查", result.ObjectCleanupFailures)
	}
	oplog.AddLog(
		fmt.Sprintf("删除客户端资源: resource_id=%d resource_key=%s releases=%d artifacts=%d objects=%d cleanup_failures=%d", deleted.ID, deleted.ResourceKey, result.DeletedReleases, result.DeletedArtifacts, result.DeletedObjects, result.ObjectCleanupFailures),
		"client_resource_delete", user.ID, user.Name, user.CallSign, c.ClientIP(),
	)
	writeClientResourceSuccess(c, message, result)
}

func ListClientResourceReleases(c *gin.Context) {
	if _, ok := requireClientResourceAdmin(c); !ok {
		return
	}
	resourceID, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	page, pageSize := clientResourcePagination(c)
	releases, total, err := gormdb.NewClientResourceRepository().ListReleases(gormdb.ClientResourceReleaseListFilter{
		ResourceID: resourceID, Version: c.Query("version"), Channel: c.Query("channel"), Status: c.Query("status"),
		Platform: normalizeClientTargetValue(c.Query("platform")), Arch: normalizeClientArch(c.Query("arch")), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "获取资源发布列表失败")
		return
	}
	items := make([]clientResourceReleaseResponse, 0, len(releases))
	for _, release := range releases {
		items = append(items, clientResourceReleaseToResponse(release))
	}
	writeClientResourceSuccess(c, "成功", clientResourceReleaseListResponse{Items: items, Total: total, Page: page, PageSize: pageSize})
}

func CreateClientResourceRelease(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	resourceID, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return
	}
	var req clientResourceReleaseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	version, err := validateClientResourceVersion(req.Version)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	channel, err := normalizeClientResourceChannel(req.Channel)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	minClientVersion := strings.TrimSpace(req.MinClientVersion)
	if minClientVersion != "" {
		minClientVersion, err = validateClientResourceVersion(minClientVersion)
		if err != nil {
			writeClientResourceError(c, http.StatusBadRequest, "min_client_version "+err.Error())
			return
		}
	}
	minServerVersion, requiredProtocolVersion, requiredCapabilitiesJSON, err := normalizeClientResourceReleaseRequirements(
		req.MinServerVersion,
		req.RequiredProtocolVersion,
		req.RequiredCapabilities,
	)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	changelog := strings.TrimSpace(req.Changelog)
	if utf8.RuneCountInString(title) > clientResourceMaxTitleLength || len(changelog) > clientResourceMaxChangelogLength {
		writeClientResourceError(c, http.StatusBadRequest, "标题或更新说明过长")
		return
	}
	release := &gormdb.ClientResourceRelease{
		ResourceID: resourceID, Version: version, Channel: channel, Title: title, Changelog: changelog,
		Status: gormdb.ClientResourceReleaseStatusDraft, ForceUpdate: req.ForceUpdate,
		MinClientVersion: minClientVersion, MinServerVersion: minServerVersion,
		RequiredProtocolVersion: requiredProtocolVersion, RequiredCapabilitiesJSON: requiredCapabilitiesJSON,
		CreatedBy: user.ID,
	}
	if err := gormdb.NewClientResourceRepository().CreateRelease(release); err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientResourceError(c, http.StatusNotFound, "资源不存在")
		case errors.Is(err, gormdb.ErrClientResourceDisabled):
			writeClientResourceError(c, http.StatusConflict, "已停用资源不能创建发布")
		case gormdb.IsDuplicateKeyError(err):
			writeClientResourceError(c, http.StatusConflict, "相同频道和版本的发布已存在")
		default:
			writeClientResourceError(c, http.StatusInternalServerError, "创建资源发布失败")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("创建客户端资源发布草稿: resource_id=%d %s/%s", resourceID, channel, version), "client_resource_release_create", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientResourceSuccess(c, "资源发布草稿创建成功", clientResourceReleaseToResponse(release))
}

func GetClientResourceRelease(c *gin.Context) {
	if _, ok := requireClientResourceAdmin(c); !ok {
		return
	}
	release, ok := loadNestedClientResourceRelease(c)
	if !ok {
		return
	}
	writeClientResourceSuccess(c, "成功", clientResourceReleaseToResponse(release))
}

func CompleteClientResourceArtifact(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	release, ok := loadNestedClientResourceRelease(c)
	if !ok {
		return
	}
	if release.Status != gormdb.ClientResourceReleaseStatusDraft && release.Status != gormdb.ClientResourceReleaseStatusPublished {
		writeClientResourceError(c, http.StatusConflict, "当前版本状态不允许添加文件")
		return
	}
	if release.Resource == nil || !release.Resource.Enabled {
		writeClientResourceError(c, http.StatusConflict, "资源已停用")
		return
	}
	var req clientResourceArtifactCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	format, runtimeName, variant, buildNumber, metadata, err := normalizeClientResourceArtifact(req)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	targets, err := normalizeClientResourceTargets(format, req.Targets)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	fileName := strings.TrimSpace(req.FileName)
	if utf8.RuneCountInString(fileName) > clientResourceMaxFileNameLength || strings.ContainsAny(fileName, `/\`) {
		writeClientResourceError(c, http.StatusBadRequest, "file_name 无效")
		return
	}
	artifact := &gormdb.ClientResourceArtifact{
		ReleaseID: release.ID, Format: format, Runtime: runtimeName, Variant: variant, BuildNumber: buildNumber,
		FileName: fileName, ContentSignature: strings.TrimSpace(req.ContentSignature),
		SignatureAlgorithm: strings.TrimSpace(req.SignatureAlgorithm), Metadata: metadata, Targets: targets,
	}
	var finalize func(*gormdb.ClientResourceRelease) error
	if strings.TrimSpace(req.ExternalURL) != "" || format == "app_store" {
		externalURL := strings.TrimSpace(req.ExternalURL)
		if release.Resource.Category != "application" || format != "app_store" || !isClientResourceHTTPSURL(externalURL) || utf8.RuneCountInString(externalURL) > clientResourceMaxExternalURLLength {
			writeClientResourceError(c, http.StatusBadRequest, "只有 application/app_store 可以提供有效的 HTTPS external_url")
			return
		}
		if strings.TrimSpace(req.ObjectKey) != "" || strings.TrimSpace(req.UploadToken) != "" {
			writeClientResourceError(c, http.StatusBadRequest, "外部商店资源不需要上传对象")
			return
		}
		artifact.ExternalURL = externalURL
		if artifact.FileName == "" {
			artifact.FileName = "App Store / TestFlight"
		}
	} else {
		if fileName == "" || !clientResourceFileNameMatchesFormat(fileName, format) {
			writeClientResourceError(c, http.StatusBadRequest, "file_name 扩展名与 format 不匹配")
			return
		}
		objectKey := strings.TrimLeft(strings.TrimSpace(req.ObjectKey), "/")
		if objectKey == "" || strings.TrimSpace(req.UploadToken) == "" || !storage.IsStagingObjectKey(objectKey, "client_resource", user.ID) {
			writeClientResourceError(c, http.StatusBadRequest, "非法的上传对象或凭证")
			return
		}
		if !clientResourceFileNameMatchesFormat(path.Base(objectKey), format) {
			writeClientResourceError(c, http.StatusBadRequest, "上传对象扩展名与 format 不匹配")
			return
		}
		grant, storedContentType, err := storage.ValidateStagedUpload(c.Request.Context(), req.UploadToken, objectKey, "client_resource", user.ID)
		if err != nil {
			writeClientResourceError(c, http.StatusBadRequest, "上传授权或对象校验失败")
			return
		}
		if !validClientResourceContentType(format, grant.ContentType, storedContentType) {
			writeClientResourceError(c, http.StatusBadRequest, "Content-Type 与 format 不匹配")
			return
		}
		finalize = func(lockedRelease *gormdb.ClientResourceRelease) error {
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), clientResourceFinalizeTimeout)
			defer cancel()
			stagedSize, digest, err := storage.HashObjectSHA256(finalizeCtx, objectKey, storage.MaxSizeForFileType("client_resource"))
			if err != nil {
				return fmt.Errorf("hash staged client resource: %w", err)
			}
			if stagedSize != grant.Size {
				return fmt.Errorf("staged client resource size mismatch: got %d want %d", stagedSize, grant.Size)
			}
			if lockedRelease.Resource == nil {
				return fmt.Errorf("client resource is missing")
			}
			finalKey := clientResourceObjectKey(lockedRelease.Resource, lockedRelease, artifact, digest)
			if err := storage.Promote(finalizeCtx, objectKey, finalKey); err != nil {
				return err
			}
			finalSize, finalDigest, verifyErr := storage.HashObjectSHA256(finalizeCtx, finalKey, storage.MaxSizeForFileType("client_resource"))
			if verifyErr != nil || finalSize != grant.Size || finalDigest != digest {
				cleanupClientResourceObject(c, finalKey, "校验失败后删除客户端资源对象失败")
				if verifyErr != nil {
					return fmt.Errorf("hash promoted client resource: %w", verifyErr)
				}
				return fmt.Errorf("promoted client resource integrity mismatch")
			}
			artifact.StorageKey, artifact.FileSize, artifact.SHA256 = finalKey, finalSize, finalDigest
			return nil
		}
	}
	rollback := func() {
		if artifact.StorageKey != "" {
			cleanupClientResourceObject(c, artifact.StorageKey, "回滚客户端资源对象失败")
		}
	}
	repo := gormdb.NewClientResourceRepository()
	if err := repo.CompleteArtifact(artifact, finalize, rollback); err != nil {
		switch {
		case errors.Is(err, gormdb.ErrClientResourceTargetConflict), gormdb.IsDuplicateKeyError(err):
			writeClientResourceError(c, http.StatusConflict, "相同格式、runtime、variant 的适用目标已存在")
		case errors.Is(err, gormdb.ErrClientResourceReleaseNotEditable):
			writeClientResourceError(c, http.StatusConflict, "当前版本状态不允许添加文件")
		case errors.Is(err, gormdb.ErrClientResourceDisabled):
			writeClientResourceError(c, http.StatusConflict, "资源已停用")
		case errors.Is(err, storage.ErrFinalObjectAlreadyExists):
			writeClientResourceError(c, http.StatusConflict, "不可变资源对象已存在")
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientResourceError(c, http.StatusNotFound, "发布不存在")
		default:
			logClientResourceStorageError("保存客户端资源 artifact 失败", artifact.StorageKey, err)
			writeClientResourceError(c, http.StatusInternalServerError, "保存资源文件失败")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("完成客户端资源文件: release_id=%d artifact_id=%d targets=%d", release.ID, artifact.ID, len(artifact.Targets)), "client_resource_artifact_complete", user.ID, user.Name, user.CallSign, c.ClientIP())
	updated, _ := repo.GetReleaseByID(release.ID)
	writeClientResourceSuccess(c, "资源文件上传完成", clientResourceReleaseToResponse(updated))
}

func PublishClientResourceRelease(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	release, ok := loadNestedClientResourceRelease(c)
	if !ok {
		return
	}
	contract := clientcontract.Current()
	published, err := gormdb.NewClientResourceRepository().PublishRelease(release.ID, func(locked *gormdb.ClientResourceRelease) error {
		return validateClientResourceServerContract(locked, contract)
	})
	if err != nil {
		var compatibilityFailure *clientcontract.Failure
		switch {
		case errors.Is(err, gormdb.ErrClientResourceHasNoArtifact), errors.Is(err, gormdb.ErrClientResourceTargetRequired):
			writeClientResourceError(c, http.StatusBadRequest, "发布至少需要一个具有适用目标的完整文件")
		case errors.Is(err, gormdb.ErrClientResourceReleaseNotDraft):
			writeClientResourceError(c, http.StatusConflict, "只有草稿可以发布")
		case errors.Is(err, gormdb.ErrClientResourceDisabled):
			writeClientResourceError(c, http.StatusConflict, "资源已停用")
		case errors.As(err, &compatibilityFailure), errors.Is(err, errClientResourceInvalidContractRequirement):
			writeClientResourceError(c, http.StatusConflict, clientResourceContractFailureMessage(err))
		default:
			writeClientResourceRepositoryError(c, err, "发布客户端资源失败")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("发布客户端资源版本: resource_id=%d version=%s", published.ResourceID, published.Version), "client_resource_release_publish", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientResourceSuccess(c, "资源版本发布成功", clientResourceReleaseToResponse(published))
}

func DeleteClientResourceRelease(c *gin.Context) {
	user, ok := requireClientResourceAdmin(c)
	if !ok {
		return
	}
	release, ok := loadNestedClientResourceRelease(c)
	if !ok {
		return
	}
	deleted, err := gormdb.NewClientResourceRepository().DeleteRelease(release.ID)
	if err != nil {
		writeClientResourceRepositoryError(c, err, "删除资源版本失败")
		return
	}
	result := cleanupDeletedClientResourceReleaseObjects(c, deleted)
	oplog.AddLog(
		fmt.Sprintf("删除客户端资源版本: release_id=%d status=%s artifacts=%d objects=%d cleanup_failures=%d", release.ID, release.Status, result.DeletedArtifacts, result.DeletedObjects, result.ObjectCleanupFailures),
		"client_resource_release_delete", user.ID, user.Name, user.CallSign, c.ClientIP(),
	)
	writeClientResourceSuccess(c, "资源版本已删除", result)
}

func GetClientResourceManifest(c *gin.Context) {
	request, err := parseClientResourceManifestRequest(c)
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, err.Error())
		return
	}
	artifacts, err := gormdb.NewClientResourceRepository().ListManifestArtifacts(gormdb.ClientResourceManifestLookup{
		Channel: request.Channel, Platform: request.Platform, Arch: request.Arch,
	})
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "查询客户端资源失败")
		return
	}
	manifest := buildClientResourceManifest(artifacts, request)
	payload, err := json.Marshal(manifest)
	if err != nil {
		writeClientResourceError(c, http.StatusInternalServerError, "生成客户端资源清单失败")
		return
	}
	digest := sha256.Sum256(payload)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=60, must-revalidate")
	c.Header("Vary", "Accept-Encoding")
	if clientResourceETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	writeClientResourceSuccess(c, "成功", manifest)
}

func GetClientResourceArtifactDownload(c *gin.Context) {
	id, err := clientResourcePathID(c, "artifact_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的 artifact ID")
		return
	}
	artifact, err := gormdb.NewClientResourceRepository().GetDownloadableArtifact(id)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientResourceError(c, http.StatusNotFound, "资源文件不存在")
		case errors.Is(err, gormdb.ErrClientResourceNotPublished):
			writeClientResourceError(c, http.StatusNotFound, "资源文件不可下载")
		default:
			writeClientResourceError(c, http.StatusInternalServerError, "获取资源文件失败")
		}
		return
	}
	if err := validateClientResourceServerContract(artifact.Release, clientcontract.Current()); err != nil {
		writeClientResourceError(c, http.StatusNotFound, "资源文件与当前服务端不兼容")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	if artifact.ExternalURL != "" {
		writeClientResourceSuccess(c, "成功", gin.H{"artifact_id": artifact.ID, "download_url": artifact.ExternalURL})
		return
	}
	if !storage.IsEnabled() {
		writeClientResourceError(c, http.StatusServiceUnavailable, "存储服务不可用")
		return
	}
	downloadURL, err := storage.PresignGet(c.Request.Context(), artifact.StorageKey, clientResourceDownloadURLExpiry)
	if err != nil {
		logClientResourceStorageError("生成资源下载地址失败", artifact.StorageKey, err)
		writeClientResourceError(c, http.StatusServiceUnavailable, "生成下载链接失败")
		return
	}
	if strings.HasPrefix(downloadURL, "/") {
		downloadURL = publicAPIBase(c) + downloadURL
	}
	writeClientResourceSuccess(c, "成功", gin.H{
		"artifact_id": artifact.ID, "download_url": downloadURL,
		"url_expires_at": time.Now().Add(clientResourceDownloadURLExpiry).UTC().Format(time.RFC3339),
	})
}

func normalizeClientResourceDefinition(resourceKey, name, category, description string) (string, string, string, string, error) {
	resourceKey = strings.ToLower(strings.Trim(strings.TrimSpace(resourceKey), "/"))
	name, category, description = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(category)), strings.TrimSpace(description)
	if resourceKey == "" || len(resourceKey) > clientResourceMaxKeyLength {
		return "", "", "", "", fmt.Errorf("resource_key 无效")
	}
	for _, segment := range strings.Split(resourceKey, "/") {
		if segment == "." || segment == ".." || !clientResourceKeySegmentPattern.MatchString(segment) {
			return "", "", "", "", fmt.Errorf("resource_key 只能由安全的小写路径段组成")
		}
	}
	if name == "" || utf8.RuneCountInString(name) > clientResourceMaxNameLength {
		return "", "", "", "", fmt.Errorf("资源名称无效")
	}
	if category != "" && (utf8.RuneCountInString(category) > clientResourceMaxCategoryLength || !clientResourceSlugPattern.MatchString(category)) {
		return "", "", "", "", fmt.Errorf("category 格式无效")
	}
	if len(description) > clientResourceMaxDescriptionLength {
		return "", "", "", "", fmt.Errorf("description 过长")
	}
	return resourceKey, name, category, description, nil
}

func normalizeClientResourceArtifact(req clientResourceArtifactCompleteRequest) (string, string, string, string, string, error) {
	format := strings.ToLower(strings.TrimSpace(req.Format))
	runtimeName := strings.ToLower(strings.TrimSpace(req.Runtime))
	variant := strings.ToLower(strings.TrimSpace(req.Variant))
	if runtimeName == "" {
		runtimeName = "default"
	}
	if variant == "" {
		variant = "default"
	}
	if !clientResourceSlugPattern.MatchString(format) || !clientResourceSlugPattern.MatchString(runtimeName) || !clientResourceSlugPattern.MatchString(variant) {
		return "", "", "", "", "", fmt.Errorf("format、runtime 或 variant 格式无效")
	}
	buildNumber := strings.TrimSpace(req.BuildNumber)
	if utf8.RuneCountInString(buildNumber) > clientResourceMaxBuildNumberLength {
		return "", "", "", "", "", fmt.Errorf("build_number 过长")
	}
	signature := strings.TrimSpace(req.ContentSignature)
	signatureAlgorithm := strings.ToLower(strings.TrimSpace(req.SignatureAlgorithm))
	if len(signature) > clientResourceMaxSignatureLength || utf8.RuneCountInString(signatureAlgorithm) > clientResourceMaxSignatureAlgorithm {
		return "", "", "", "", "", fmt.Errorf("内容签名元数据过长")
	}
	if (signature == "") != (signatureAlgorithm == "") {
		return "", "", "", "", "", fmt.Errorf("content_signature 与 signature_algorithm 必须同时提供")
	}
	metadata := "{}"
	if len(req.Metadata) > 0 && string(req.Metadata) != "null" {
		if len(req.Metadata) > clientResourceMaxMetadataLength || !json.Valid(req.Metadata) {
			return "", "", "", "", "", fmt.Errorf("metadata 必须是有效且不超过 64 KiB 的 JSON")
		}
		var value map[string]any
		if err := json.Unmarshal(req.Metadata, &value); err != nil {
			return "", "", "", "", "", fmt.Errorf("metadata 必须是 JSON 对象")
		}
		compact, _ := json.Marshal(value)
		metadata = string(compact)
	}
	return format, runtimeName, variant, buildNumber, metadata, nil
}

func normalizeClientResourceTargets(format string, requests []clientResourceArtifactTargetRequest) ([]gormdb.ClientResourceArtifactTarget, error) {
	if len(requests) == 0 || len(requests) > 128 {
		return nil, fmt.Errorf("targets 数量必须在 1 到 128 之间")
	}
	seen := make(map[string]struct{}, len(requests))
	targets := make([]gormdb.ClientResourceArtifactTarget, 0, len(requests))
	for _, request := range requests {
		platform := normalizeClientTargetValue(request.Platform)
		arch := normalizeClientArch(request.Arch)
		if !clientResourceTargetPattern.MatchString(platform) || !clientResourceTargetPattern.MatchString(arch) || platform == "universal" || arch == "universal" {
			return nil, fmt.Errorf("platform 和 arch 必须是明确的目标，不能使用 universal")
		}
		if err := validateClientResourceApplicationTarget(format, platform, arch); err != nil {
			return nil, err
		}
		key := platform + "\x00" + arch
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("targets 包含重复的平台/架构组合")
		}
		seen[key] = struct{}{}
		minOSVersion := strings.TrimSpace(request.MinOSVersion)
		if minOSVersion != "" {
			validated, err := validateClientResourceVersion(minOSVersion)
			if err != nil {
				return nil, fmt.Errorf("min_os_version %w", err)
			}
			minOSVersion = validated
		}
		if request.MinAndroidAPI < 0 || request.MinAndroidAPI > 1000 {
			return nil, fmt.Errorf("min_android_api 无效")
		}
		if platform != "android" && request.MinAndroidAPI != 0 {
			return nil, fmt.Errorf("min_android_api 仅适用于 Android")
		}
		targets = append(targets, gormdb.ClientResourceArtifactTarget{
			Platform: platform, Arch: arch, MinOSVersion: minOSVersion, MinAndroidAPI: request.MinAndroidAPI,
		})
	}
	return targets, nil
}

func validateClientResourceApplicationTarget(format, platform, arch string) error {
	switch format {
	case "apk":
		if platform != "android" || (arch != "armv7" && arch != "arm64" && arch != "x86_64") {
			return fmt.Errorf("apk 目标必须是明确的 Android 架构")
		}
	case "exe", "msix":
		if platform != "windows" {
			return fmt.Errorf("%s 只能用于 Windows 目标", format)
		}
	case "dmg", "pkg":
		if platform != "macos" {
			return fmt.Errorf("%s 只能用于 macOS 目标", format)
		}
	case "ipa", "app_store":
		if platform != "ios" {
			return fmt.Errorf("%s 只能用于 iOS 目标", format)
		}
	}
	return nil
}

func normalizeClientTargetValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeClientArch(value string) string {
	value = normalizeClientTargetValue(value)
	switch value {
	case "amd64", "x64":
		return "x86_64"
	case "aarch64", "arm64-v8a":
		return "arm64"
	case "armeabi-v7a", "arm32":
		return "armv7"
	default:
		return value
	}
}

func validateClientResourceVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > clientResourceMaxVersionLength || !clientversion.IsValid(value) {
		return "", fmt.Errorf("版本号格式无效，应为 SemVer 2.0.0")
	}
	return value, nil
}

func normalizeClientResourceReleaseRequirements(minServerVersion string, requiredProtocolVersion int, capabilities []string) (string, uint16, *string, error) {
	minServerVersion = strings.TrimSpace(minServerVersion)
	if minServerVersion != "" {
		validated, err := validateClientResourceVersion(minServerVersion)
		if err != nil {
			return "", 0, nil, fmt.Errorf("min_server_version %s", err)
		}
		minServerVersion = validated
	}
	if requiredProtocolVersion < 0 || requiredProtocolVersion > 65535 {
		return "", 0, nil, fmt.Errorf("required_protocol_version 必须在 0 到 65535 之间")
	}
	normalizedCapabilities, err := normalizeClientResourceCapabilities(capabilities)
	if err != nil {
		return "", 0, nil, err
	}
	if len(normalizedCapabilities) > 0 && requiredProtocolVersion == 0 {
		return "", 0, nil, fmt.Errorf("声明 required_capabilities 时必须同时声明 required_protocol_version")
	}
	if len(normalizedCapabilities) == 0 {
		return minServerVersion, uint16(requiredProtocolVersion), nil, nil
	}
	raw, err := json.Marshal(normalizedCapabilities)
	if err != nil {
		return "", 0, nil, fmt.Errorf("序列化 required_capabilities: %w", err)
	}
	encoded := string(raw)
	return minServerVersion, uint16(requiredProtocolVersion), &encoded, nil
}

func normalizeClientResourceCapabilities(capabilities []string) ([]string, error) {
	if len(capabilities) > clientResourceMaxCapabilities {
		return nil, fmt.Errorf("required_capabilities 最多包含 %d 项", clientResourceMaxCapabilities)
	}
	seen := make(map[string]struct{}, len(capabilities))
	normalized := make([]string, 0, len(capabilities))
	for _, value := range capabilities {
		capability := strings.ToLower(strings.TrimSpace(value))
		if !clientResourceCapabilityPattern.MatchString(capability) {
			return nil, fmt.Errorf("required_capabilities 包含无效能力名")
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func clientResourceRequiredCapabilities(release *gormdb.ClientResourceRelease) ([]string, error) {
	if release == nil || release.RequiredCapabilitiesJSON == nil {
		return nil, nil
	}
	var capabilities []string
	if err := json.Unmarshal([]byte(*release.RequiredCapabilitiesJSON), &capabilities); err != nil {
		return nil, fmt.Errorf("%w: required_capabilities is not valid JSON", errClientResourceInvalidContractRequirement)
	}
	normalized, err := normalizeClientResourceCapabilities(capabilities)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errClientResourceInvalidContractRequirement, err)
	}
	return normalized, nil
}

func validateClientResourceServerContract(release *gormdb.ClientResourceRelease, contract clientcontract.Contract) error {
	if release == nil {
		return fmt.Errorf("%w: release is nil", errClientResourceInvalidContractRequirement)
	}
	if release.MinServerVersion != "" && !clientversion.IsValid(release.MinServerVersion) {
		return fmt.Errorf("%w: min_server_version is invalid", errClientResourceInvalidContractRequirement)
	}
	capabilities, err := clientResourceRequiredCapabilities(release)
	if err != nil {
		return err
	}
	if len(capabilities) > 0 && release.RequiredProtocolVersion == 0 {
		return fmt.Errorf("%w: capabilities require a protocol version", errClientResourceInvalidContractRequirement)
	}
	return clientcontract.Check(contract, clientcontract.Requirement{
		MinServerVersion:        release.MinServerVersion,
		RequiredProtocolVersion: release.RequiredProtocolVersion,
		RequiredCapabilities:    capabilities,
	})
}

func clientResourceContractFailureMessage(err error) string {
	if errors.Is(err, errClientResourceInvalidContractRequirement) {
		return "发布版本的服务端协议约束无效"
	}
	var failure *clientcontract.Failure
	if !errors.As(err, &failure) {
		return "当前服务端不满足发布版本的协议约束"
	}
	switch failure.Kind {
	case clientcontract.FailureUnknownServerVersion:
		return fmt.Sprintf("当前服务端版本 %s 不是可比较的 SemVer，无法满足最低服务端版本 %s", failure.Current, failure.Required)
	case clientcontract.FailureServerVersionTooLow:
		return fmt.Sprintf("当前服务端版本 %s 低于发布要求 %s", failure.Current, failure.Required)
	case clientcontract.FailureProtocolVersionTooLow:
		return fmt.Sprintf("当前幽灵协议版本 %s 低于发布要求 %s", failure.Current, failure.Required)
	case clientcontract.FailureMissingCapability:
		return fmt.Sprintf("当前服务端缺少发布要求的能力 %s", failure.Capability)
	default:
		return "当前服务端不满足发布版本的协议约束"
	}
}

func normalizeClientResourceChannel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = clientResourceDefaultChannel
	}
	if value != "stable" && value != "beta" {
		return "", fmt.Errorf("channel 只支持 stable 或 beta")
	}
	return value, nil
}

func clientResourceFileNameMatchesFormat(fileName, format string) bool {
	if format == "app_store" {
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(strings.TrimSpace(fileName))), ".")
	return ext != "" && ext == format
}

func validClientResourceContentType(format string, values ...string) bool {
	allowed := map[string]map[string]bool{
		"apk":   {"application/vnd.android.package-archive": true, "application/zip": true, "application/octet-stream": true},
		"exe":   {"application/vnd.microsoft.portable-executable": true, "application/x-msdownload": true, "application/octet-stream": true},
		"msix":  {"application/zip": true, "application/octet-stream": true, "application/vnd.ms-appx": true},
		"dmg":   {"application/x-apple-diskimage": true, "application/octet-stream": true},
		"ipa":   {"application/zip": true, "application/octet-stream": true},
		"json":  {"application/json": true, "text/json": true, "application/octet-stream": true},
		"ttf":   {"font/ttf": true, "application/x-font-ttf": true, "application/octet-stream": true},
		"otf":   {"font/otf": true, "application/x-font-opentype": true, "application/octet-stream": true},
		"woff":  {"font/woff": true, "application/font-woff": true, "application/octet-stream": true},
		"woff2": {"font/woff2": true, "application/octet-stream": true},
	}
	formats, strict := allowed[format]
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
		if value == "" {
			continue
		}
		if strict && !formats[value] {
			return false
		}
	}
	return true
}

func clientResourceObjectKey(resource *gormdb.ClientResource, release *gormdb.ClientResourceRelease, artifact *gormdb.ClientResourceArtifact, digest string) string {
	segments := strings.Split(resource.ResourceKey, "/")
	parts := []string{"client-resources"}
	parts = append(parts, segments...)
	parts = append(parts, release.Channel, release.Version, artifact.Format, artifact.Runtime, artifact.Variant, digest, path.Base(artifact.FileName))
	return path.Join(parts...)
}

func parseClientResourceManifestRequest(c *gin.Context) (clientResourceManifestRequest, error) {
	platform := normalizeClientTargetValue(c.Query("platform"))
	arch := normalizeClientArch(c.Query("arch"))
	if !clientResourceTargetPattern.MatchString(platform) || !clientResourceTargetPattern.MatchString(arch) || platform == "universal" || arch == "universal" {
		return clientResourceManifestRequest{}, fmt.Errorf("platform 和 arch 参数必须是明确目标")
	}
	channel, err := normalizeClientResourceChannel(c.Query("channel"))
	if err != nil {
		return clientResourceManifestRequest{}, err
	}
	clientVersion := strings.TrimSpace(c.Query("client_version"))
	if clientVersion != "" {
		clientVersion, err = validateClientResourceVersion(clientVersion)
		if err != nil {
			return clientResourceManifestRequest{}, fmt.Errorf("client_version %s", err)
		}
	}
	osVersion := strings.TrimSpace(c.Query("os_version"))
	if osVersion != "" {
		osVersion, err = validateClientResourceVersion(osVersion)
		if err != nil {
			return clientResourceManifestRequest{}, fmt.Errorf("os_version %s", err)
		}
	}
	androidAPI := 0
	if raw := strings.TrimSpace(c.Query("android_api")); raw != "" {
		if platform != "android" {
			return clientResourceManifestRequest{}, fmt.Errorf("android_api 仅适用于 Android")
		}
		androidAPI, err = strconv.Atoi(raw)
		if err != nil || androidAPI < 0 || androidAPI > 1000 {
			return clientResourceManifestRequest{}, fmt.Errorf("android_api 参数无效")
		}
	}
	return clientResourceManifestRequest{Platform: platform, Arch: arch, Channel: channel, ClientVersion: clientVersion, OSVersion: osVersion, AndroidAPI: androidAPI}, nil
}

func buildClientResourceManifest(artifacts []*gormdb.ClientResourceArtifact, request clientResourceManifestRequest) clientResourceManifestResponse {
	return buildClientResourceManifestForContract(artifacts, request, clientcontract.Current())
}

func buildClientResourceManifestForContract(artifacts []*gormdb.ClientResourceArtifact, request clientResourceManifestRequest, contract clientcontract.Contract) clientResourceManifestResponse {
	compatible := make([]*gormdb.ClientResourceArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Release == nil || artifact.Release.Resource == nil || !clientResourceReleaseCompatible(artifact.Release, request, contract) || !clientResourceArtifactTargetCompatible(artifact, request) {
			continue
		}
		compatible = append(compatible, artifact)
	}
	byResource := make(map[int][]*gormdb.ClientResourceArtifact)
	for _, artifact := range compatible {
		byResource[artifact.Release.ResourceID] = append(byResource[artifact.Release.ResourceID], artifact)
	}
	items := make([]clientResourceManifestItem, 0, len(byResource))
	for _, candidates := range byResource {
		selectedChannel := "stable"
		if request.Channel == "beta" {
			for _, candidate := range candidates {
				if candidate.Release.Channel == "beta" {
					selectedChannel = "beta"
					break
				}
			}
		}
		var selectedRelease *gormdb.ClientResourceRelease
		for _, candidate := range candidates {
			release := candidate.Release
			if release.Channel != selectedChannel {
				continue
			}
			if selectedRelease == nil || clientversion.Compare(release.Version, selectedRelease.Version) > 0 || (release.Version == selectedRelease.Version && release.ID > selectedRelease.ID) {
				selectedRelease = release
			}
		}
		if selectedRelease == nil {
			continue
		}
		selectedArtifacts := make([]clientResourceArtifactResponse, 0)
		for _, candidate := range candidates {
			if candidate.Release.ID == selectedRelease.ID {
				selectedArtifacts = append(selectedArtifacts, clientResourceArtifactToResponse(candidate, false))
			}
		}
		sort.Slice(selectedArtifacts, func(i, j int) bool {
			left, right := selectedArtifacts[i], selectedArtifacts[j]
			if left.Format != right.Format {
				return left.Format < right.Format
			}
			if left.Runtime != right.Runtime {
				return left.Runtime < right.Runtime
			}
			if left.Variant != right.Variant {
				return left.Variant < right.Variant
			}
			return left.ID < right.ID
		})
		resource := selectedRelease.Resource
		requiredCapabilities, _ := clientResourceRequiredCapabilities(selectedRelease)
		items = append(items, clientResourceManifestItem{
			Resource: clientResourceSummary{ID: resource.ID, ResourceKey: resource.ResourceKey, Name: resource.Name, Category: resource.Category, Required: resource.Required},
			Release: clientResourceManifestRelease{
				ID: selectedRelease.ID, Version: selectedRelease.Version, Channel: selectedRelease.Channel,
				Title: selectedRelease.Title, Changelog: selectedRelease.Changelog, ForceUpdate: selectedRelease.ForceUpdate,
				MinClientVersion: selectedRelease.MinClientVersion, MinServerVersion: selectedRelease.MinServerVersion,
				RequiredProtocolVersion: selectedRelease.RequiredProtocolVersion, RequiredCapabilities: requiredCapabilities,
				PublishedAt: selectedRelease.PublishedAt,
			},
			Artifacts: selectedArtifacts,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Resource.ResourceKey < items[j].Resource.ResourceKey })
	return clientResourceManifestResponse{
		SchemaVersion: clientResourceManifestSchemaVersion,
		ServerVersion: contract.ServerVersion, ProtocolVersion: contract.ProtocolVersion,
		Capabilities: append([]string(nil), contract.Capabilities...), Resources: items,
	}
}

func clientResourceReleaseCompatible(release *gormdb.ClientResourceRelease, request clientResourceManifestRequest, contract clientcontract.Contract) bool {
	if err := validateClientResourceServerContract(release, contract); err != nil {
		return false
	}
	return release.MinClientVersion == "" || request.ClientVersion != "" && clientversion.Compare(request.ClientVersion, release.MinClientVersion) >= 0
}

func clientResourceArtifactTargetCompatible(artifact *gormdb.ClientResourceArtifact, request clientResourceManifestRequest) bool {
	for _, target := range artifact.Targets {
		if target.Platform != request.Platform || target.Arch != request.Arch {
			continue
		}
		if target.MinAndroidAPI > 0 && (request.AndroidAPI == 0 || request.AndroidAPI < target.MinAndroidAPI) {
			continue
		}
		if target.MinOSVersion != "" && (request.OSVersion == "" || clientversion.Compare(request.OSVersion, target.MinOSVersion) < 0) {
			continue
		}
		return true
	}
	return false
}

func clientResourceToResponse(resource *gormdb.ClientResource) clientResourceResponse {
	if resource == nil {
		return clientResourceResponse{}
	}
	response := clientResourceResponse{ID: resource.ID, ResourceKey: resource.ResourceKey, Name: resource.Name, Category: resource.Category, Description: resource.Description, Required: resource.Required, Enabled: resource.Enabled, CreatedBy: resource.CreatedBy, CreateTime: resource.CreateTime, UpdateTime: resource.UpdateTime}
	for i := range resource.Releases {
		release := &resource.Releases[i]
		switch release.Channel {
		case "stable":
			if response.CurrentStableVersion == "" || clientversion.Compare(release.Version, response.CurrentStableVersion) > 0 {
				response.CurrentStableVersion = release.Version
			}
		case "beta":
			if response.CurrentBetaVersion == "" || clientversion.Compare(release.Version, response.CurrentBetaVersion) > 0 {
				response.CurrentBetaVersion = release.Version
			}
		}
	}
	return response
}

func clientResourceReleaseToResponse(release *gormdb.ClientResourceRelease) clientResourceReleaseResponse {
	if release == nil {
		return clientResourceReleaseResponse{}
	}
	requiredCapabilities, _ := clientResourceRequiredCapabilities(release)
	response := clientResourceReleaseResponse{
		ID: release.ID, ResourceID: release.ResourceID, Version: release.Version, Channel: release.Channel,
		Title: release.Title, Changelog: release.Changelog, Status: release.Status, ForceUpdate: release.ForceUpdate,
		MinClientVersion: release.MinClientVersion, MinServerVersion: release.MinServerVersion,
		RequiredProtocolVersion: release.RequiredProtocolVersion, RequiredCapabilities: requiredCapabilities,
		PublishedAt: release.PublishedAt, CreatedBy: release.CreatedBy, CreateTime: release.CreateTime, UpdateTime: release.UpdateTime,
		Artifacts: make([]clientResourceArtifactResponse, 0, len(release.Artifacts)),
	}
	if release.Resource != nil {
		response.Resource = clientResourceSummary{ID: release.Resource.ID, ResourceKey: release.Resource.ResourceKey, Name: release.Resource.Name, Category: release.Resource.Category, Required: release.Resource.Required}
	}
	for i := range release.Artifacts {
		response.Artifacts = append(response.Artifacts, clientResourceArtifactToResponse(&release.Artifacts[i], true))
	}
	return response
}

func clientResourceArtifactToResponse(artifact *gormdb.ClientResourceArtifact, includeStorageKey bool) clientResourceArtifactResponse {
	if artifact == nil {
		return clientResourceArtifactResponse{}
	}
	response := clientResourceArtifactResponse{ID: artifact.ID, ReleaseID: artifact.ReleaseID, Format: artifact.Format, Runtime: artifact.Runtime, Variant: artifact.Variant, BuildNumber: artifact.BuildNumber, FileName: artifact.FileName, FileSize: artifact.FileSize, SHA256: artifact.SHA256, ContentSignature: artifact.ContentSignature, SignatureAlgorithm: artifact.SignatureAlgorithm, ExternalURL: artifact.ExternalURL, Targets: make([]clientResourceArtifactTargetResponse, 0, len(artifact.Targets))}
	if includeStorageKey {
		response.StorageKey = artifact.StorageKey
	}
	if artifact.Metadata != "" && artifact.Metadata != "{}" {
		response.Metadata = json.RawMessage(artifact.Metadata)
	}
	for _, target := range artifact.Targets {
		response.Targets = append(response.Targets, clientResourceArtifactTargetResponse{Platform: target.Platform, Arch: target.Arch, MinOSVersion: target.MinOSVersion, MinAndroidAPI: target.MinAndroidAPI})
	}
	sort.Slice(response.Targets, func(i, j int) bool {
		if response.Targets[i].Platform != response.Targets[j].Platform {
			return response.Targets[i].Platform < response.Targets[j].Platform
		}
		return response.Targets[i].Arch < response.Targets[j].Arch
	})
	return response
}

func loadNestedClientResourceRelease(c *gin.Context) (*gormdb.ClientResourceRelease, bool) {
	resourceID, err := clientResourcePathID(c, "resource_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的资源 ID")
		return nil, false
	}
	releaseID, err := clientResourcePathID(c, "release_id")
	if err != nil {
		writeClientResourceError(c, http.StatusBadRequest, "无效的发布 ID")
		return nil, false
	}
	release, err := gormdb.NewClientResourceRepository().GetReleaseByID(releaseID)
	if err != nil {
		writeClientResourceRepositoryError(c, err, "获取资源发布失败")
		return nil, false
	}
	if release.ResourceID != resourceID {
		writeClientResourceError(c, http.StatusNotFound, "发布不属于该资源")
		return nil, false
	}
	return release, true
}

func requireClientResourceAdmin(c *gin.Context) (*gormdb.User, bool) {
	value, ok := c.Get("user")
	user, ok := value.(*gormdb.User)
	if !ok || user == nil || !hasRoleGORM(user, "admin") {
		writeClientResourceError(c, http.StatusUnauthorized, "未授权")
		return nil, false
	}
	return user, true
}

func clientResourcePathID(c *gin.Context, name string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param(name)))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func clientResourcePagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func writeClientResourceSuccess(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": message, "data": data})
}
func writeClientResourceError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message})
}

func writeClientResourceRepositoryError(c *gin.Context, err error, fallback string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeClientResourceError(c, http.StatusNotFound, "记录不存在")
		return
	}
	writeClientResourceError(c, http.StatusInternalServerError, fallback)
}

func isClientResourceHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func clientResourceETagMatches(header, etag string) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == etag || value == "*" {
			return true
		}
	}
	return false
}

func cleanupClientResourceObject(c *gin.Context, key, message string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
	defer cancel()
	if err := storage.Delete(cleanupCtx, key); err != nil {
		logClientResourceStorageError(message, key, err)
	}
}

func cleanupDeletedClientResourceReleaseObjects(c *gin.Context, release *gormdb.ClientResourceRelease) clientResourceReleaseDeleteResponse {
	result := clientResourceReleaseDeleteResponse{}
	if release == nil {
		return result
	}
	keys := make(map[string]struct{})
	for _, artifact := range release.Artifacts {
		result.DeletedArtifacts++
		if key := strings.TrimSpace(artifact.StorageKey); key != "" {
			keys[key] = struct{}{}
		}
	}
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), clientResourceDeleteCleanupTimeout)
	defer cancel()
	for _, key := range orderedKeys {
		if err := storage.Delete(cleanupCtx, key); err != nil {
			result.ObjectCleanupFailures++
			logClientResourceStorageError("删除客户端资源版本对象失败", key, err)
			continue
		}
		result.DeletedObjects++
	}
	return result
}

func cleanupDeletedClientResourceObjects(c *gin.Context, resource *gormdb.ClientResource) clientResourceDeleteResponse {
	result := clientResourceDeleteResponse{}
	if resource == nil {
		return result
	}
	keys := make(map[string]struct{})
	for _, release := range resource.Releases {
		result.DeletedReleases++
		for _, artifact := range release.Artifacts {
			result.DeletedArtifacts++
			if key := strings.TrimSpace(artifact.StorageKey); key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), clientResourceDeleteCleanupTimeout)
	defer cancel()
	for _, key := range orderedKeys {
		if err := storage.Delete(cleanupCtx, key); err != nil {
			result.ObjectCleanupFailures++
			logClientResourceStorageError("级联删除客户端资源对象失败", key, err)
			continue
		}
		result.DeletedObjects++
	}
	return result
}

func logClientResourceStorageError(message, key string, err error) {
	log.Printf("[CLIENT_RESOURCE] %s key=%s err=%v", message, key, err)
}
