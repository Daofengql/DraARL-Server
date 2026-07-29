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
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"draarl/internal/clientversion"
	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	clientReleaseDefaultAppID       = "draarl-client"
	clientReleaseDefaultChannel     = "stable"
	clientReleaseDownloadURLExpiry  = 15 * time.Minute
	clientReleaseFinalizeTimeout    = 10 * time.Minute
	clientReleaseMaxAppIDLength     = 100
	clientReleaseMaxVersionLength   = 64
	clientReleaseMaxTitleLength     = 255
	clientReleaseMaxFileNameLength  = 255
	clientReleaseMaxChangelogLength = 1 << 20
	clientReleaseMaxBuildNumber     = 64
	clientReleaseMaxExternalURL     = 1024
	clientReleaseMaxSignature       = 65535
	clientReleaseMaxSignatureAlgo   = 64
)

var clientAppIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

type clientReleaseCreateRequest struct {
	AppID               string `json:"app_id" binding:"required"`
	Version             string `json:"version" binding:"required"`
	Channel             string `json:"channel"`
	Title               string `json:"title"`
	Changelog           string `json:"changelog"`
	ForceUpdate         bool   `json:"force_update"`
	MinSupportedVersion string `json:"min_supported_version"`
}

type clientReleaseArtifactCompleteRequest struct {
	Platform           string `json:"platform" binding:"required"`
	Arch               string `json:"arch" binding:"required"`
	AndroidABI         string `json:"android_abi"`
	PackageType        string `json:"package_type" binding:"required"`
	BuildNumber        string `json:"build_number"`
	MinOSVersion       string `json:"min_os_version"`
	MinAndroidAPI      int    `json:"min_android_api"`
	FileName           string `json:"file_name"`
	ObjectKey          string `json:"object_key"`
	UploadToken        string `json:"upload_token"`
	ExternalURL        string `json:"external_url"`
	Signature          string `json:"signature"`
	SignatureAlgorithm string `json:"signature_algorithm"`
}

type clientArtifactMetadata struct {
	BuildNumber        string
	MinOSVersion       string
	MinAndroidAPI      int
	Signature          string
	SignatureAlgorithm string
}

type clientReleaseListResponse struct {
	Items    []clientReleaseResponse `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

type clientReleaseResponse struct {
	ID                  int                      `json:"id"`
	AppID               string                   `json:"app_id"`
	Version             string                   `json:"version"`
	Channel             string                   `json:"channel"`
	Title               string                   `json:"title"`
	Changelog           string                   `json:"changelog"`
	Status              string                   `json:"status"`
	ForceUpdate         bool                     `json:"force_update"`
	MinSupportedVersion string                   `json:"min_supported_version,omitempty"`
	PublishedAt         *time.Time               `json:"published_at,omitempty"`
	CreatedBy           int                      `json:"created_by"`
	CreateTime          time.Time                `json:"create_time"`
	Artifacts           []clientArtifactResponse `json:"artifacts"`
}

type clientArtifactResponse struct {
	ID                 int        `json:"id"`
	ReleaseID          int        `json:"release_id"`
	Platform           string     `json:"platform"`
	Arch               string     `json:"arch"`
	AndroidABI         string     `json:"android_abi,omitempty"`
	PackageType        string     `json:"package_type"`
	BuildNumber        string     `json:"build_number,omitempty"`
	MinOSVersion       string     `json:"min_os_version,omitempty"`
	MinAndroidAPI      int        `json:"min_android_api,omitempty"`
	FileName           string     `json:"file_name"`
	FileSize           int64      `json:"file_size"`
	SHA256             string     `json:"sha256,omitempty"`
	Signature          string     `json:"signature,omitempty"`
	SignatureAlgorithm string     `json:"signature_algorithm,omitempty"`
	ExternalURL        string     `json:"external_url,omitempty"`
	DownloadURL        string     `json:"download_url,omitempty"`
	URLExpiresAt       *time.Time `json:"url_expires_at,omitempty"`
}

type clientLatestResponse struct {
	HasUpdate bool                   `json:"has_update"`
	Release   clientReleaseSummary   `json:"release,omitempty"`
	Artifact  clientArtifactResponse `json:"artifact,omitempty"`
}

type clientReleaseSummary struct {
	ID                  int        `json:"id"`
	AppID               string     `json:"app_id"`
	Version             string     `json:"version"`
	Channel             string     `json:"channel"`
	Title               string     `json:"title,omitempty"`
	Changelog           string     `json:"changelog,omitempty"`
	ForceUpdate         bool       `json:"force_update"`
	MinSupportedVersion string     `json:"min_supported_version,omitempty"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
}

type clientLatestRequest struct {
	AppID       string
	Platform    string
	Arch        string
	PackageType string
	Current     string
	Channel     string
	AndroidAPI  int
	OSVersion   string
}

// CreateClientRelease creates an immutable-version draft for administrators.
// POST /api/client-releases
func CreateClientRelease(c *gin.Context) {
	user, ok := adminUser(c)
	if !ok {
		writeClientReleaseError(c, http.StatusUnauthorized, "未授权")
		return
	}
	var req clientReleaseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	appID := strings.TrimSpace(req.AppID)
	if appID == "" {
		appID = clientReleaseDefaultAppID
	}
	if len(appID) > clientReleaseMaxAppIDLength || !clientAppIDPattern.MatchString(appID) {
		writeClientReleaseError(c, http.StatusBadRequest, "app_id 格式无效")
		return
	}
	version, err := validateClientVersion(req.Version)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, err.Error())
		return
	}
	channel, err := normalizeClientChannel(req.Channel)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, err.Error())
		return
	}
	minSupported := strings.TrimSpace(req.MinSupportedVersion)
	if minSupported != "" {
		if minSupported, err = validateClientVersion(minSupported); err != nil {
			writeClientReleaseError(c, http.StatusBadRequest, "min_supported_version "+err.Error())
			return
		}
		if clientversion.Compare(minSupported, version) > 0 {
			writeClientReleaseError(c, http.StatusBadRequest, "min_supported_version 不能高于发布版本")
			return
		}
	}
	changelog := strings.TrimSpace(req.Changelog)
	if len(changelog) > clientReleaseMaxChangelogLength {
		writeClientReleaseError(c, http.StatusBadRequest, "changelog 过长")
		return
	}
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > clientReleaseMaxTitleLength {
		writeClientReleaseError(c, http.StatusBadRequest, "title 过长")
		return
	}

	release := &gormdb.ClientRelease{
		AppID:               appID,
		Version:             version,
		Channel:             channel,
		Title:               title,
		Changelog:           changelog,
		Status:              gormdb.ClientReleaseStatusDraft,
		ForceUpdate:         req.ForceUpdate,
		MinSupportedVersion: minSupported,
		CreatedBy:           user.ID,
	}
	if err := gormdb.NewClientReleaseRepository().Create(release); err != nil {
		if gormdb.IsDuplicateKeyError(err) {
			writeClientReleaseError(c, http.StatusConflict, "相同 app_id、频道和版本的发布已存在")
			return
		}
		writeClientReleaseError(c, http.StatusInternalServerError, "创建发布草稿失败")
		return
	}
	oplog.AddLog(fmt.Sprintf("创建客户端发布草稿: %s %s/%s", appID, channel, version), "client_release_create", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientReleaseSuccess(c, "发布草稿创建成功", releaseToResponse(release, nil))
}

// ListClientReleases lists administrator-visible releases and their target matrix.
// GET /api/client-releases
func ListClientReleases(c *gin.Context) {
	page := parsePositiveQueryInt(c, "page", 1)
	pageSize := parsePositiveQueryInt(c, "page_size", 20)
	if pageSize > 100 {
		pageSize = 100
	}
	releases, total, err := gormdb.NewClientReleaseRepository().List(gormdb.ClientReleaseListFilter{
		AppID: c.Query("app_id"), Version: c.Query("version"), Channel: c.Query("channel"), Status: c.Query("status"),
		Platform: c.Query("platform"), Arch: c.Query("arch"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeClientReleaseError(c, http.StatusInternalServerError, "获取客户端发布列表失败")
		return
	}
	items := make([]clientReleaseResponse, 0, len(releases))
	for _, release := range releases {
		items = append(items, releaseToResponse(release, nil))
	}
	writeClientReleaseSuccess(c, "成功", clientReleaseListResponse{Items: items, Total: total, Page: page, PageSize: pageSize})
}

// GetClientRelease returns a release and its platform/architecture matrix.
// GET /api/client-releases/:id
func GetClientRelease(c *gin.Context) {
	id, err := clientReleaseID(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "无效的发布 ID")
		return
	}
	release, err := gormdb.NewClientReleaseRepository().GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
			return
		}
		writeClientReleaseError(c, http.StatusInternalServerError, "获取发布失败")
		return
	}
	writeClientReleaseSuccess(c, "成功", releaseToResponse(release, nil))
}

// CompleteClientReleaseArtifact validates an upload grant, calculates SHA-256,
// promotes the staging object, and records an immutable artifact.
// POST /api/client-releases/:id/artifacts/complete
func CompleteClientReleaseArtifact(c *gin.Context) {
	user, ok := adminUser(c)
	if !ok {
		writeClientReleaseError(c, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := clientReleaseID(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "无效的发布 ID")
		return
	}
	var req clientReleaseArtifactCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "请求参数错误")
		return
	}
	repo := gormdb.NewClientReleaseRepository()
	release, err := repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
			return
		}
		writeClientReleaseError(c, http.StatusInternalServerError, "获取发布失败")
		return
	}
	if release.Status != gormdb.ClientReleaseStatusDraft {
		writeClientReleaseError(c, http.StatusConflict, "只有草稿可以添加安装包")
		return
	}
	platform, arch, androidABI, packageType, err := normalizeClientTarget(req.Platform, req.Arch, req.AndroidABI, req.PackageType)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, err.Error())
		return
	}
	metadata, err := normalizeClientArtifactMetadata(platform, req)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, err.Error())
		return
	}
	fileName := strings.TrimSpace(req.FileName)
	if utf8.RuneCountInString(fileName) > clientReleaseMaxFileNameLength || strings.ContainsAny(fileName, `/\\`) {
		writeClientReleaseError(c, http.StatusBadRequest, "file_name 无效")
		return
	}

	artifact := &gormdb.ClientReleaseArtifact{
		ReleaseID:          id,
		Platform:           platform,
		Arch:               arch,
		AndroidABI:         androidABI,
		PackageType:        packageType,
		BuildNumber:        metadata.BuildNumber,
		MinOSVersion:       metadata.MinOSVersion,
		MinAndroidAPI:      metadata.MinAndroidAPI,
		FileName:           fileName,
		Signature:          metadata.Signature,
		SignatureAlgorithm: metadata.SignatureAlgorithm,
	}

	var completeArtifact func(release *gormdb.ClientRelease) error
	if packageType == "app_store" {
		externalURL := strings.TrimSpace(req.ExternalURL)
		if externalURL == "" || utf8.RuneCountInString(externalURL) > clientReleaseMaxExternalURL || !isHTTPSURL(externalURL) {
			writeClientReleaseError(c, http.StatusBadRequest, "app_store 必须提供有效的 external_url")
			return
		}
		if req.ObjectKey != "" || req.UploadToken != "" {
			writeClientReleaseError(c, http.StatusBadRequest, "app_store 不需要上传对象")
			return
		}
		artifact.ExternalURL = externalURL
		if artifact.FileName == "" {
			artifact.FileName = "App Store / TestFlight"
		}
	} else {
		if strings.TrimSpace(req.ExternalURL) != "" {
			writeClientReleaseError(c, http.StatusBadRequest, "只有 app_store 可以提供 external_url")
			return
		}
		if fileName == "" || !packageFileNameMatches(fileName, packageType) {
			writeClientReleaseError(c, http.StatusBadRequest, "file_name 扩展名与 package_type 不匹配")
			return
		}
		objectKey := strings.TrimLeft(strings.TrimSpace(req.ObjectKey), "/")
		if objectKey == "" || strings.TrimSpace(req.UploadToken) == "" || !storage.IsStagingObjectKey(objectKey, "client_package", user.ID) {
			writeClientReleaseError(c, http.StatusBadRequest, "非法的上传对象或凭证")
			return
		}
		if !packageFileNameMatches(path.Base(objectKey), packageType) {
			writeClientReleaseError(c, http.StatusBadRequest, "上传对象扩展名与 package_type 不匹配")
			return
		}
		grant, storedContentType, err := storage.ValidateStagedUpload(c.Request.Context(), req.UploadToken, objectKey, "client_package", user.ID)
		if err != nil {
			writeClientReleaseError(c, http.StatusBadRequest, "上传授权或对象校验失败")
			return
		}
		if !validClientPackageContentType(packageType, grant.ContentType, storedContentType) {
			writeClientReleaseError(c, http.StatusBadRequest, "安装包 Content-Type 不符合 package_type")
			return
		}
		completeArtifact = func(lockedRelease *gormdb.ClientRelease) error {
			finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), clientReleaseFinalizeTimeout)
			defer cancel()
			finalKey := clientReleaseObjectKey(lockedRelease, platform, arch, fileName)
			if err := storage.Promote(finalizeCtx, objectKey, finalKey); err != nil {
				return err
			}
			fileSize, digest, err := hashClientPackage(finalizeCtx, finalKey)
			if err != nil || fileSize != grant.Size {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
				defer cancel()
				if cleanupErr := storage.Delete(cleanupCtx, finalKey); cleanupErr != nil {
					logClientReleaseStorageError("校验失败后删除客户端安装包对象失败", finalKey, cleanupErr)
				}
				if err != nil {
					return fmt.Errorf("hash promoted client package: %w", err)
				}
				return fmt.Errorf("promoted client package size mismatch: got %d want %d", fileSize, grant.Size)
			}
			artifact.StorageKey = finalKey
			artifact.FileSize = fileSize
			artifact.SHA256 = digest
			return nil
		}
	}

	rollbackArtifact := func() {
		if artifact.StorageKey != "" {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
			defer cancel()
			if cleanupErr := storage.Delete(cleanupCtx, artifact.StorageKey); cleanupErr != nil {
				logClientReleaseStorageError("回滚客户端安装包对象失败", artifact.StorageKey, cleanupErr)
			}
		}
	}
	if err := repo.CompleteArtifact(artifact, completeArtifact, rollbackArtifact); err != nil {
		if gormdb.IsDuplicateKeyError(err) {
			writeClientReleaseError(c, http.StatusConflict, "该发布目标已存在安装包")
			return
		}
		if errors.Is(err, gormdb.ErrClientReleaseNotDraft) {
			writeClientReleaseError(c, http.StatusConflict, "只有草稿可以添加安装包")
			return
		}
		if errors.Is(err, storage.ErrFinalObjectAlreadyExists) {
			writeClientReleaseError(c, http.StatusConflict, "安装包对象已存在")
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
			return
		}
		writeClientReleaseError(c, http.StatusInternalServerError, "保存安装包元数据失败")
		return
	}
	oplog.AddLog(fmt.Sprintf("完成客户端安装包: release_id=%d platform=%s arch=%s type=%s", id, platform, arch, packageType), "client_artifact_complete", user.ID, user.Name, user.CallSign, c.ClientIP())
	updated, _ := repo.GetByID(id)
	if updated == nil {
		updated = &gormdb.ClientRelease{ID: id}
	}
	writeClientReleaseSuccess(c, "安装包上传完成", releaseToResponse(updated, nil))
}

// PublishClientRelease publishes a draft after at least one complete artifact.
// POST /api/client-releases/:id/publish
func PublishClientRelease(c *gin.Context) {
	user, ok := adminUser(c)
	if !ok {
		writeClientReleaseError(c, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := clientReleaseID(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "无效的发布 ID")
		return
	}
	release, err := gormdb.NewClientReleaseRepository().Publish(id)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
		case errors.Is(err, gormdb.ErrClientReleaseHasNoPackage):
			writeClientReleaseError(c, http.StatusBadRequest, "发布至少需要一个完整安装包")
		case errors.Is(err, gormdb.ErrClientReleaseNotDraft):
			writeClientReleaseError(c, http.StatusConflict, "只有草稿可以发布")
		default:
			writeClientReleaseError(c, http.StatusInternalServerError, "发布客户端版本失败")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("发布客户端版本: release_id=%d version=%s", release.ID, release.Version), "client_release_publish", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientReleaseSuccess(c, "客户端版本已发布", releaseToResponse(release, nil))
}

// WithdrawClientRelease withdraws a published release without deleting audit data.
// POST /api/client-releases/:id/withdraw
func WithdrawClientRelease(c *gin.Context) {
	user, ok := adminUser(c)
	if !ok {
		writeClientReleaseError(c, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := clientReleaseID(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "无效的发布 ID")
		return
	}
	release, err := gormdb.NewClientReleaseRepository().Withdraw(id)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
		case errors.Is(err, gormdb.ErrClientReleaseNotPublished):
			writeClientReleaseError(c, http.StatusConflict, "只有已发布版本可以撤回")
		default:
			writeClientReleaseError(c, http.StatusInternalServerError, "撤回客户端版本失败")
		}
		return
	}
	oplog.AddLog(fmt.Sprintf("撤回客户端版本: release_id=%d version=%s", release.ID, release.Version), "client_release_withdraw", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientReleaseSuccess(c, "客户端版本已撤回", releaseToResponse(release, nil))
}

// DeleteClientRelease deletes only drafts and removes their final objects.
// DELETE /api/client-releases/:id
func DeleteClientRelease(c *gin.Context) {
	user, ok := adminUser(c)
	if !ok {
		writeClientReleaseError(c, http.StatusUnauthorized, "未授权")
		return
	}
	id, err := clientReleaseID(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, "无效的发布 ID")
		return
	}
	release, err := gormdb.NewClientReleaseRepository().DeleteDraft(id)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeClientReleaseError(c, http.StatusNotFound, "发布不存在")
		case errors.Is(err, gormdb.ErrClientReleaseNotDraft):
			writeClientReleaseError(c, http.StatusConflict, "只有草稿可以删除")
		default:
			writeClientReleaseError(c, http.StatusInternalServerError, "删除发布草稿失败")
		}
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 30*time.Second)
	defer cancel()
	for _, artifact := range release.Artifacts {
		if artifact.StorageKey != "" {
			if err := storage.Delete(cleanupCtx, artifact.StorageKey); err != nil {
				// 数据库记录已经删除，保留日志供运维清理孤儿对象。
				logClientReleaseStorageError("删除草稿对象失败", artifact.StorageKey, err)
			}
		}
	}
	oplog.AddLog(fmt.Sprintf("删除客户端发布草稿: release_id=%d", id), "client_release_delete_draft", user.ID, user.Name, user.CallSign, c.ClientIP())
	writeClientReleaseSuccess(c, "发布草稿已删除", nil)
}

// GetLatestClientRelease is the public, platform-aware update check endpoint.
// GET /api/public/client/latest
func GetLatestClientRelease(c *gin.Context) {
	getLatestClientRelease(
		c,
		gormdb.NewClientReleaseRepository(),
		storage.IsEnabled,
		storage.PresignGet,
		time.Now,
	)
}

type clientReleaseArtifactLister interface {
	ListPublishedArtifacts(gormdb.ClientArtifactLookup) ([]*gormdb.ClientReleaseArtifact, error)
}

type clientReleasePresignGet func(context.Context, string, time.Duration) (string, error)

func getLatestClientRelease(
	c *gin.Context,
	repository clientReleaseArtifactLister,
	storageEnabled func() bool,
	presignGet clientReleasePresignGet,
	nowFunc func() time.Time,
) {
	request, err := parseClientLatestRequest(c)
	if err != nil {
		writeClientReleaseError(c, http.StatusBadRequest, err.Error())
		return
	}
	architectures := []string{request.Arch}
	if request.Arch != "universal" {
		architectures = append(architectures, "universal")
	}
	artifacts, err := repository.ListPublishedArtifacts(gormdb.ClientArtifactLookup{
		AppID: request.AppID, Channel: request.Channel, Platform: request.Platform,
		PackageType: request.PackageType, Architectures: architectures,
	})
	if err != nil {
		writeClientReleaseError(c, http.StatusInternalServerError, "查询客户端更新失败")
		return
	}
	candidate := chooseClientArtifact(artifacts, request)
	now := nowFunc()
	etag := clientReleaseETagAt(request, candidate, now)
	c.Header("ETag", etag)
	// The response can contain a bearer-style presigned URL. Keep it out of
	// shared caches while still allowing each client to use conditional GETs.
	c.Header("Cache-Control", "private, max-age=60")
	c.Header("Vary", "Accept-Encoding")
	if candidate == nil {
		if len(artifacts) > 0 {
			writeClientReleaseError(c, http.StatusNotFound, "暂无兼容当前系统版本的安装包")
			return
		}
		writeClientReleaseError(c, http.StatusNotFound, "暂无匹配的平台、架构或安装格式")
		return
	}
	if etagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	if request.Current != "" && clientversion.Compare(candidate.Release.Version, request.Current) <= 0 {
		writeClientReleaseSuccess(c, "当前已是最新版本", nil)
		return
	}

	result := clientLatestResponse{
		HasUpdate: true,
		Release: clientReleaseSummary{
			ID: candidate.Release.ID, AppID: candidate.Release.AppID, Version: candidate.Release.Version,
			Channel: candidate.Release.Channel, Title: candidate.Release.Title, Changelog: candidate.Release.Changelog,
			ForceUpdate: candidate.Release.ForceUpdate, MinSupportedVersion: candidate.Release.MinSupportedVersion,
			PublishedAt: candidate.Release.PublishedAt,
		},
		Artifact: artifactToResponse(candidate, "", nil),
	}
	if candidate.ExternalURL != "" {
		result.Artifact.DownloadURL = candidate.ExternalURL
	} else {
		if !storageEnabled() {
			writeClientReleaseError(c, http.StatusServiceUnavailable, "存储服务不可用")
			return
		}
		expiresAt := now.Add(clientReleaseDownloadURLExpiry)
		downloadURL, err := presignGet(c.Request.Context(), candidate.StorageKey, clientReleaseDownloadURLExpiry)
		if err != nil {
			writeClientReleaseError(c, http.StatusServiceUnavailable, "生成下载链接失败")
			return
		}
		if strings.HasPrefix(downloadURL, "/") {
			downloadURL = publicAPIBase(c) + downloadURL
		}
		result.Artifact.DownloadURL = downloadURL
		result.Artifact.URLExpiresAt = &expiresAt
	}
	writeClientReleaseSuccess(c, "成功", result)
}

func adminUser(c *gin.Context) (*gormdb.User, bool) {
	value, ok := c.Get("user")
	user, ok := value.(*gormdb.User)
	return user, ok && user != nil
}

func writeClientReleaseSuccess(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": message, "data": data})
}

func writeClientReleaseError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message})
}

func releaseToResponse(release *gormdb.ClientRelease, _ map[int]clientArtifactResponse) clientReleaseResponse {
	if release == nil {
		return clientReleaseResponse{}
	}
	response := clientReleaseResponse{
		ID: release.ID, AppID: release.AppID, Version: release.Version, Channel: release.Channel,
		Title: release.Title, Changelog: release.Changelog, Status: release.Status,
		ForceUpdate: release.ForceUpdate, MinSupportedVersion: release.MinSupportedVersion,
		PublishedAt: release.PublishedAt, CreatedBy: release.CreatedBy, CreateTime: release.CreateTime,
		Artifacts: make([]clientArtifactResponse, 0, len(release.Artifacts)),
	}
	for i := range release.Artifacts {
		response.Artifacts = append(response.Artifacts, artifactToResponse(&release.Artifacts[i], "", nil))
	}
	return response
}

func artifactToResponse(artifact *gormdb.ClientReleaseArtifact, downloadURL string, expiresAt *time.Time) clientArtifactResponse {
	if artifact == nil {
		return clientArtifactResponse{}
	}
	return clientArtifactResponse{
		ID: artifact.ID, ReleaseID: artifact.ReleaseID, Platform: artifact.Platform, Arch: artifact.Arch,
		AndroidABI: artifact.AndroidABI, PackageType: artifact.PackageType, BuildNumber: artifact.BuildNumber,
		MinOSVersion: artifact.MinOSVersion, MinAndroidAPI: artifact.MinAndroidAPI, FileName: artifact.FileName,
		FileSize: artifact.FileSize, SHA256: artifact.SHA256, Signature: artifact.Signature,
		SignatureAlgorithm: artifact.SignatureAlgorithm, ExternalURL: artifact.ExternalURL,
		DownloadURL: downloadURL, URLExpiresAt: expiresAt,
	}
}

func validateClientVersion(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > clientReleaseMaxVersionLength || !clientversion.IsValid(value) {
		return "", fmt.Errorf("版本号格式无效，应为 semver 格式如 1.0.0 或 1.0.0-beta.1")
	}
	return value, nil
}

func normalizeClientChannel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = clientReleaseDefaultChannel
	}
	if value != "stable" && value != "beta" {
		return "", fmt.Errorf("channel 只支持 stable 或 beta")
	}
	return value, nil
}

func normalizeClientTarget(platform, arch, androidABI, packageType string) (string, string, string, string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	arch = strings.ToLower(strings.TrimSpace(arch))
	suppliedAndroidABI := strings.ToLower(strings.TrimSpace(androidABI))
	packageType = strings.ToLower(strings.TrimSpace(packageType))
	if platform == "" || arch == "" || packageType == "" {
		return "", "", "", "", fmt.Errorf("platform、arch 和 package_type 为必填字段")
	}
	if platform == "android" {
		switch arch {
		case "armv7", "armeabi-v7a", "arm32":
			arch, androidABI = "armv7", "armeabi-v7a"
		case "armv8", "arm64", "arm64-v8a", "aarch64":
			arch, androidABI = "arm64", "arm64-v8a"
		case "universal":
			androidABI = "universal"
		default:
			return "", "", "", "", fmt.Errorf("Android arch 只支持 armv7、arm64 或 universal")
		}
		if packageType != "apk" {
			return "", "", "", "", fmt.Errorf("Android package_type 只支持 apk")
		}
		if suppliedAndroidABI != "" && suppliedAndroidABI != androidABI {
			return "", "", "", "", fmt.Errorf("android_abi 与 arch 不匹配")
		}
		return platform, arch, androidABI, packageType, nil
	}
	if suppliedAndroidABI != "" {
		return "", "", "", "", fmt.Errorf("android_abi 仅适用于 Android")
	}
	if arch == "amd64" || arch == "x64" {
		arch = "x86_64"
	}
	if arch == "aarch64" {
		arch = "arm64"
	}
	switch platform {
	case "windows":
		if arch != "x86_64" && arch != "universal" {
			return "", "", "", "", fmt.Errorf("Windows arch 只支持 x86_64 或 universal")
		}
		if packageType != "exe" && packageType != "msix" {
			return "", "", "", "", fmt.Errorf("Windows package_type 只支持 exe 或 msix")
		}
	case "macos":
		if arch != "arm64" && arch != "x86_64" && arch != "universal" {
			return "", "", "", "", fmt.Errorf("macOS arch 无效")
		}
		if packageType != "dmg" && packageType != "pkg" {
			return "", "", "", "", fmt.Errorf("macOS package_type 只支持 dmg 或 pkg")
		}
	case "ios":
		if arch != "arm64" && arch != "universal" {
			return "", "", "", "", fmt.Errorf("iOS arch 只支持 arm64 或 universal")
		}
		if packageType != "app_store" && packageType != "ipa" {
			return "", "", "", "", fmt.Errorf("iOS package_type 只支持 app_store 或 ipa")
		}
		if packageType == "app_store" && arch != "universal" {
			return "", "", "", "", fmt.Errorf("iOS app_store arch 只支持 universal")
		}
	default:
		return "", "", "", "", fmt.Errorf("不支持的平台: %s", platform)
	}
	return platform, arch, "", packageType, nil
}

func normalizeClientArtifactMetadata(platform string, req clientReleaseArtifactCompleteRequest) (clientArtifactMetadata, error) {
	metadata := clientArtifactMetadata{
		BuildNumber:        strings.TrimSpace(req.BuildNumber),
		MinOSVersion:       strings.TrimSpace(req.MinOSVersion),
		MinAndroidAPI:      req.MinAndroidAPI,
		Signature:          strings.TrimSpace(req.Signature),
		SignatureAlgorithm: strings.TrimSpace(req.SignatureAlgorithm),
	}
	if metadata.MinAndroidAPI < 0 || metadata.MinAndroidAPI > 1000 {
		return clientArtifactMetadata{}, fmt.Errorf("min_android_api 无效")
	}
	if platform != "android" && metadata.MinAndroidAPI != 0 {
		return clientArtifactMetadata{}, fmt.Errorf("min_android_api 仅适用于 Android")
	}
	if metadata.MinOSVersion != "" {
		validated, err := validateClientVersion(metadata.MinOSVersion)
		if err != nil {
			return clientArtifactMetadata{}, fmt.Errorf("min_os_version %w", err)
		}
		metadata.MinOSVersion = validated
	}
	if utf8.RuneCountInString(metadata.BuildNumber) > clientReleaseMaxBuildNumber {
		return clientArtifactMetadata{}, fmt.Errorf("build_number 过长")
	}
	if len(metadata.Signature) > clientReleaseMaxSignature {
		return clientArtifactMetadata{}, fmt.Errorf("signature 过长")
	}
	if utf8.RuneCountInString(metadata.SignatureAlgorithm) > clientReleaseMaxSignatureAlgo {
		return clientArtifactMetadata{}, fmt.Errorf("signature_algorithm 过长")
	}
	if (metadata.Signature == "") != (metadata.SignatureAlgorithm == "") {
		return clientArtifactMetadata{}, fmt.Errorf("signature 与 signature_algorithm 必须同时提供")
	}
	return metadata, nil
}

func packageFileNameMatches(fileName, packageType string) bool {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(fileName)))
	allowed := map[string]string{"apk": ".apk", "exe": ".exe", "msix": ".msix", "dmg": ".dmg", "pkg": ".pkg", "ipa": ".ipa"}
	want, ok := allowed[packageType]
	return ok && ext == want
}

func validClientPackageContentType(packageType, declared, detected string) bool {
	allowed := map[string]map[string]bool{
		"apk":  {"application/vnd.android.package-archive": true, "application/zip": true, "application/octet-stream": true},
		"exe":  {"application/vnd.microsoft.portable-executable": true, "application/x-msdownload": true, "application/octet-stream": true},
		"msix": {"application/zip": true, "application/octet-stream": true, "application/vnd.ms-appx": true},
		"dmg":  {"application/x-apple-diskimage": true, "application/octet-stream": true},
		"pkg":  {"application/octet-stream": true, "application/x-newton-compatible-pkg": true},
		"ipa":  {"application/zip": true, "application/octet-stream": true},
	}
	values := []string{declared, detected}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
		if value != "" && !allowed[packageType][value] {
			return false
		}
	}
	return true
}

func hashClientPackage(ctx context.Context, objectKey string) (int64, string, error) {
	reader, err := storage.Open(ctx, objectKey)
	if err != nil {
		return 0, "", err
	}
	defer reader.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(reader, storage.MaxSizeForFileType("client_package")+1))
	if err != nil {
		return 0, "", err
	}
	if written > storage.MaxSizeForFileType("client_package") {
		return written, "", fmt.Errorf("安装包超过大小限制")
	}
	return written, hex.EncodeToString(hasher.Sum(nil)), nil
}

func clientReleaseObjectKey(release *gormdb.ClientRelease, platform, arch, fileName string) string {
	return path.Join("client-releases", safeClientSegment(release.AppID), safeClientSegment(release.Channel), safeClientSegment(release.Version), platform, arch, safeClientSegment(path.Base(fileName)))
}

func safeClientSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' {
			b.WriteRune(ch)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "item"
	}
	return b.String()
}

func clientReleaseID(c *gin.Context) (int, error) {
	return strconv.Atoi(strings.TrimSpace(c.Param("id")))
}

func parsePositiveQueryInt(c *gin.Context, name string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(name, strconv.Itoa(fallback)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func parseClientLatestRequest(c *gin.Context) (clientLatestRequest, error) {
	appID := strings.TrimSpace(c.Query("app_id"))
	if appID == "" {
		appID = clientReleaseDefaultAppID
	}
	if len(appID) > clientReleaseMaxAppIDLength || !clientAppIDPattern.MatchString(appID) {
		return clientLatestRequest{}, fmt.Errorf("app_id 格式无效")
	}
	platform := strings.ToLower(strings.TrimSpace(c.Query("platform")))
	if platform == "" {
		return clientLatestRequest{}, fmt.Errorf("platform 参数必填")
	}
	arch := strings.ToLower(strings.TrimSpace(c.Query("arch")))
	if platform == "android" && arch == "" {
		return clientLatestRequest{}, fmt.Errorf("Android 的 arch 参数必填")
	}
	if arch == "" {
		arch = "universal"
	}
	packageType := strings.ToLower(strings.TrimSpace(c.Query("package_type")))
	if packageType == "" {
		packageType = defaultPackageTypeForPlatform(platform)
	}
	_, arch, _, packageType, err := normalizeClientTarget(platform, arch, "", packageType)
	if err != nil {
		return clientLatestRequest{}, err
	}
	channel, err := normalizeClientChannel(c.DefaultQuery("channel", clientReleaseDefaultChannel))
	if err != nil {
		return clientLatestRequest{}, err
	}
	current := strings.TrimSpace(c.Query("current_version"))
	if current != "" {
		if current, err = validateClientVersion(current); err != nil {
			return clientLatestRequest{}, fmt.Errorf("current_version %s", err)
		}
	}
	androidAPI := 0
	if raw := strings.TrimSpace(c.Query("android_api")); raw != "" {
		if platform != "android" {
			return clientLatestRequest{}, fmt.Errorf("android_api 仅适用于 Android")
		}
		androidAPI, err = strconv.Atoi(raw)
		if err != nil || androidAPI < 0 || androidAPI > 1000 {
			return clientLatestRequest{}, fmt.Errorf("android_api 参数无效")
		}
	}
	osVersion := strings.TrimSpace(c.Query("os_version"))
	if osVersion != "" {
		if osVersion, err = validateClientVersion(osVersion); err != nil {
			return clientLatestRequest{}, fmt.Errorf("os_version %s", err)
		}
	}
	return clientLatestRequest{
		AppID: appID, Platform: platform, Arch: arch, PackageType: packageType,
		Current: current, Channel: channel, AndroidAPI: androidAPI, OSVersion: osVersion,
	}, nil
}

func defaultPackageTypeForPlatform(platform string) string {
	switch platform {
	case "android":
		return "apk"
	case "windows":
		return "exe"
	case "macos":
		return "dmg"
	case "ios":
		return "app_store"
	default:
		return ""
	}
}

func chooseClientArtifact(artifacts []*gormdb.ClientReleaseArtifact, request clientLatestRequest) *gormdb.ClientReleaseArtifact {
	filtered := make([]*gormdb.ClientReleaseArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Release == nil {
			continue
		}
		if artifact.MinAndroidAPI > 0 && request.Platform == "android" && (request.AndroidAPI == 0 || artifact.MinAndroidAPI > request.AndroidAPI) {
			continue
		}
		if artifact.MinOSVersion != "" && (request.OSVersion == "" || clientversion.Compare(request.OSVersion, artifact.MinOSVersion) < 0) {
			continue
		}
		if artifact.Arch != request.Arch && artifact.Arch != "universal" {
			continue
		}
		filtered = append(filtered, artifact)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		versionCompare := clientversion.Compare(left.Release.Version, right.Release.Version)
		if versionCompare != 0 {
			return versionCompare > 0
		}
		// Version ordering comes first: a newer release that intentionally ships
		// only a universal package must still update clients. Within the same
		// version, always prefer the exact ABI over universal.
		if (left.Arch == request.Arch) != (right.Arch == request.Arch) {
			return left.Arch == request.Arch
		}
		if left.Release.CreateTime.Equal(right.Release.CreateTime) {
			return left.ID > right.ID
		}
		return left.Release.CreateTime.After(right.Release.CreateTime)
	})
	return filtered[0]
}

func clientReleaseETag(request clientLatestRequest, candidate *gormdb.ClientReleaseArtifact) string {
	return clientReleaseETagAt(request, candidate, time.Now())
}

func clientReleaseETagAt(request clientLatestRequest, candidate *gormdb.ClientReleaseArtifact, now time.Time) string {
	source := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s", request.AppID, request.Platform, request.Arch, request.PackageType, request.Channel, request.AndroidAPI, request.OSVersion, request.Current)
	if candidate == nil || candidate.Release == nil {
		source += "|none"
	} else {
		source += fmt.Sprintf("|%d|%d|%s|%s", candidate.Release.ID, candidate.ID, candidate.Release.Version, candidate.SHA256)
		if candidate.StorageKey != "" && candidate.ExternalURL == "" {
			// Rotate the validator before a previously returned presigned URL can
			// expire. Otherwise a client presenting the same ETag could receive
			// 304 forever and never obtain a fresh download URL.
			bucketSeconds := int64(clientReleaseDownloadURLExpiry / time.Second)
			source += fmt.Sprintf("|url-bucket:%d", now.Unix()/bucketSeconds)
		}
	}
	digest := sha256.Sum256([]byte(source))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func etagMatches(header, etag string) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == etag || value == "*" {
			return true
		}
	}
	return false
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func logClientReleaseStorageError(message, key string, err error) {
	// Keep object keys in the log for operational cleanup, but never log upload tokens.
	log.Printf("[CLIENT_RELEASE] %s key=%s err=%v", message, key, err)
}
