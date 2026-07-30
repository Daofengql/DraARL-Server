package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/middleware"
	jwtutil "draarl/pkg/jwt"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

const clientReleaseE2EEnabledEnv = "DRAARL_CLIENT_RELEASE_E2E"

type clientReleaseE2EEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type clientReleaseE2EPresignData struct {
	Mode        string            `json:"mode"`
	UploadURL   string            `json:"upload_url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	ObjectKey   string            `json:"object_key"`
	UploadToken string            `json:"upload_token"`
}

type clientReleaseE2EFirmwareLatestData struct {
	ID          int    `json:"id"`
	DevModel    int    `json:"dev_model"`
	Version     string `json:"version"`
	FileSize    int64  `json:"file_size"`
	FileHash    string `json:"file_hash"`
	HashAlgo    string `json:"hash_algo"`
	HasUpdate   bool   `json:"has_update"`
	DownloadURL string `json:"download_url"`
}

type clientReleaseE2EDownloadData struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	MimeType    string `json:"mime_type"`
	DownloadURL string `json:"download_url"`
}

type clientReleaseE2EOperatorCertData struct {
	ActiveCert  *clientReleaseE2EOperatorCert `json:"active_cert"`
	PendingCert *clientReleaseE2EOperatorCert `json:"pending_cert"`
}

type clientReleaseE2EOperatorCert struct {
	ID       int    `json:"id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	FileType string `json:"file_type"`
	FileURL  string `json:"file_url"`
	Status   int    `json:"status"`
}

type clientReleaseE2EOperatorCertSubmitData struct {
	ID       int    `json:"id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}

type clientReleaseE2ECommRecordListData struct {
	List  []CommRecordResponse `json:"list"`
	Total int64                `json:"total"`
}

type clientReleaseE2EHTTPResult struct {
	Status int
	Header http.Header
	Body   []byte
}

// TestClientReleaseHTTPE2E is opt-in because it requires a dedicated MySQL
// database. It covers client releases, firmware, assets, operator certificates,
// avatars, and persisted communication-record downloads. When DRAARL_S3_TEST_*
// is configured, the same workflows run against that provider after local storage.
func TestClientReleaseHTTPE2E(t *testing.T) {
	if !envEnabled(os.Getenv(clientReleaseE2EEnabledEnv)) {
		t.Skip("set " + clientReleaseE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the HTTP E2E")
	}

	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	parsedDSN, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsedDSN.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q; name must start with draarl_test_", parsedDSN.DBName)
	}
	parsedDSN.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{
		DSN: parsedDSN.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2,
		MaxLifetime: 60, LogLevel: "silent",
	}); err != nil {
		t.Fatalf("initialize MySQL: %v", err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(
		&gormdb.User{},
		&gormdb.ClientRelease{},
		&gormdb.ClientReleaseArtifact{},
		&gormdb.FirmwareRelease{},
		&gormdb.Asset{},
		&gormdb.OperatorCert{},
		&gormdb.Device{},
		&gormdb.Group{},
		&gormdb.CommRecord{},
	); err != nil {
		t.Fatalf("migrate E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	admin := &gormdb.User{
		Name: "client-e2e-admin-" + suffix, Email: "client-e2e-admin-" + suffix + "@example.invalid",
		CallSign: "E2EA" + suffix[len(suffix)-8:], Roles: "admin", Status: 1, ApprovalStatus: 1,
	}
	ordinary := &gormdb.User{
		Name: "client-e2e-user-" + suffix, Email: "client-e2e-user-" + suffix + "@example.invalid",
		CallSign: "E2EU" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1,
	}
	if err := db.Create(admin).Error; err != nil {
		t.Fatalf("create E2E admin: %v", err)
	}
	if err := db.Create(ordinary).Error; err != nil {
		t.Fatalf("create E2E ordinary user: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&gormdb.User{}, []int{admin.ID, ordinary.ID}).Error
	})

	jwtSecret := "client-release-e2e-jwt-secret-2026"
	if err := jwtutil.SetSecret(jwtSecret); err != nil {
		t.Fatal(err)
	}
	adminToken, err := jwtutil.GenerateToken(admin.Name, []string{"admin"})
	if err != nil {
		t.Fatal(err)
	}
	ordinaryToken, err := jwtutil.GenerateToken(ordinary.Name, []string{"user"})
	if err != nil {
		t.Fatal(err)
	}

	oldConfig := config.Config
	t.Cleanup(func() { config.Config = oldConfig })
	variants := []struct {
		name    string
		profile func(t *testing.T) config.StorageProfile
	}{
		{
			name: "local",
			profile: func(t *testing.T) config.StorageProfile {
				return config.StorageProfile{Driver: storage.DriverLocal, Local: config.LocalStorageConfig{
					RootPath: t.TempDir(), BaseURL: "/files",
				}}
			},
		},
	}
	if s3Profile, configured := clientReleaseE2ES3Profile(t); configured {
		variants = append(variants, struct {
			name    string
			profile func(t *testing.T) config.StorageProfile
		}{name: "s3", profile: func(*testing.T) config.StorageProfile { return s3Profile }})
	}

	for _, variant := range variants {
		variant := variant
		t.Run(variant.name, func(t *testing.T) {
			cfg := &config.Configuration{}
			cfg.JWT.Secret = jwtSecret
			cfg.Storage.UploadLimits.ClientPackageBytes = 8 * 1024 * 1024
			cfg.Storage.ActiveProfile = "client-release-e2e"
			cfg.Storage.Profiles = map[string]config.StorageProfile{
				cfg.Storage.ActiveProfile: variant.profile(t),
			}
			config.Config = cfg
			if err := storage.Init(cfg); err != nil {
				t.Fatalf("initialize %s storage: %v", variant.name, err)
			}

			appID := "client-e2e-" + variant.name + "-" + suffix
			stagingKeys := make([]string, 0, 8)
			businessKeys := make([]string, 0, 8)
			firmwareIDs := make([]int, 0, 1)
			assetIDs := make([]uint, 0, 2)
			operatorCertIDs := make([]int, 0, 1)
			commRecordIDs := make([]uint, 0, 1)
			t.Cleanup(func() {
				for _, key := range stagingKeys {
					_ = storage.Delete(context.Background(), key)
				}
				for _, key := range businessKeys {
					_ = storage.Delete(context.Background(), key)
				}
				var artifacts []gormdb.ClientReleaseArtifact
				_ = db.Joins("JOIN client_releases cr ON cr.id = client_release_artifacts.release_id").
					Where("cr.app_id = ?", appID).Find(&artifacts).Error
				for _, artifact := range artifacts {
					if artifact.StorageKey != "" {
						_ = storage.Delete(context.Background(), artifact.StorageKey)
					}
				}
				releaseIDs := db.Model(&gormdb.ClientRelease{}).Select("id").Where("app_id = ?", appID)
				_ = db.Where("release_id IN (?)", releaseIDs).Delete(&gormdb.ClientReleaseArtifact{}).Error
				_ = db.Where("app_id = ?", appID).Delete(&gormdb.ClientRelease{}).Error
				if len(firmwareIDs) > 0 {
					_ = db.Delete(&gormdb.FirmwareRelease{}, firmwareIDs).Error
				}
				for i := len(assetIDs) - 1; i >= 0; i-- {
					_ = db.Delete(&gormdb.Asset{}, assetIDs[i]).Error
				}
				if len(operatorCertIDs) > 0 {
					_ = db.Delete(&gormdb.OperatorCert{}, operatorCertIDs).Error
				}
				if len(commRecordIDs) > 0 {
					_ = db.Delete(&gormdb.CommRecord{}, commRecordIDs).Error
				}
			})

			gin.SetMode(gin.TestMode)
			server := httptest.NewServer(clientReleaseE2ERouter())
			t.Cleanup(server.Close)
			client := server.Client()

			createBody := map[string]any{
				"app_id": appID, "version": "1.0.0", "channel": "stable",
				"title": "E2E release", "changelog": "real HTTP lifecycle", "min_supported_version": "0.9.0",
			}
			withoutAuth := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-releases", "", createBody, nil)
			clientReleaseE2ERequireStatus(t, withoutAuth, http.StatusUnauthorized)
			withoutAdmin := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-releases", ordinaryToken, createBody, nil)
			clientReleaseE2ERequireStatus(t, withoutAdmin, http.StatusForbidden)
			invalidMinimum := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-releases", adminToken, map[string]any{
				"app_id": appID, "version": "1.0.0", "channel": "stable", "min_supported_version": "2.0.0",
			}, nil)
			clientReleaseE2ERequireStatus(t, invalidMinimum, http.StatusBadRequest)

			createRelease := func(version string) clientReleaseResponse {
				body := map[string]any{
					"app_id": appID, "version": version, "channel": "stable",
					"title": "E2E " + version, "changelog": "release " + version,
				}
				result := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-releases", adminToken, body, nil)
				clientReleaseE2ERequireStatus(t, result, http.StatusOK)
				return clientReleaseE2EDecode[clientReleaseResponse](t, result).Data
			}
			presignAndUpload := func(authToken, fileType, fileName, contentType string, payload []byte) clientReleaseE2EPresignData {
				result := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/storage/presign-put", authToken, map[string]any{
					"file_type": fileType, "file_name": fileName, "size": len(payload),
					"content_type": contentType,
				}, nil)
				clientReleaseE2ERequireStatus(t, result, http.StatusOK)
				presign := clientReleaseE2EDecode[clientReleaseE2EPresignData](t, result).Data
				stagingKeys = append(stagingKeys, presign.ObjectKey)
				putHeaders := make(http.Header, len(presign.Headers))
				for name, value := range presign.Headers {
					putHeaders.Set(name, value)
				}
				put := clientReleaseE2ERaw(t, client, http.MethodPut, presign.UploadURL, "", payload, putHeaders)
				if put.Status < 200 || put.Status >= 300 {
					t.Fatalf("presigned PUT status=%d body=%s", put.Status, strings.TrimSpace(string(put.Body)))
				}
				return presign
			}
			completeArtifact := func(releaseID int, arch, fileName string, payload []byte) clientReleaseResponse {
				presign := presignAndUpload(adminToken, "client_package", fileName, "application/vnd.android.package-archive", payload)
				result := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/artifacts/complete", server.URL, releaseID), adminToken, map[string]any{
					"platform": "android", "arch": arch, "package_type": "apk",
					"min_android_api": 21, "min_os_version": "8.0.0", "file_name": fileName,
					"object_key": presign.ObjectKey, "upload_token": presign.UploadToken,
					"signature": "e2e-signature", "signature_algorithm": "test",
				}, nil)
				clientReleaseE2ERequireStatus(t, result, http.StatusOK)
				return clientReleaseE2EDecode[clientReleaseResponse](t, result).Data
			}
			publish := func(releaseID int) clientReleaseResponse {
				result := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/publish", server.URL, releaseID), adminToken, nil, nil)
				clientReleaseE2ERequireStatus(t, result, http.StatusOK)
				return clientReleaseE2EDecode[clientReleaseResponse](t, result).Data
			}
			latest := func(query url.Values, expectedStatus int, headers http.Header) (clientReleaseE2EHTTPResult, *clientLatestResponse) {
				query.Set("app_id", appID)
				result := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/public/client/latest?"+query.Encode(), "", nil, headers)
				clientReleaseE2ERequireStatus(t, result, expectedStatus)
				if expectedStatus != http.StatusOK {
					return result, nil
				}
				return result, clientReleaseE2EDecode[*clientLatestResponse](t, result).Data
			}

			first := createRelease("1.0.0")
			publishEmpty := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/publish", server.URL, first.ID), adminToken, nil, nil)
			clientReleaseE2ERequireStatus(t, publishEmpty, http.StatusBadRequest)

			arm64Payload := []byte("android-arm64-e2e-" + variant.name)
			armv7Payload := []byte("android-armv7-e2e-" + variant.name)
			completeArtifact(first.ID, "arm64", "draarl-1.0.0-arm64.apk", arm64Payload)
			first = completeArtifact(first.ID, "armv7", "draarl-1.0.0-armv7.apk", armv7Payload)
			armv7Artifact := clientReleaseE2EFindArtifact(t, first.Artifacts, "android", "armv7", "apk")
			if armv7Artifact.AndroidABI != "armeabi-v7a" || armv7Artifact.SHA256 != fmt.Sprintf("%x", sha256.Sum256(armv7Payload)) {
				t.Fatalf("armv7 metadata=%#v", armv7Artifact)
			}

			storeResult := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/artifacts/complete", server.URL, first.ID), adminToken, map[string]any{
				"platform": "ios", "arch": "universal", "package_type": "app_store",
				"external_url": "https://apps.apple.com/app/id123456789",
			}, nil)
			clientReleaseE2ERequireStatus(t, storeResult, http.StatusOK)

			duplicatePayload := []byte("duplicate-armv7-" + variant.name)
			duplicatePresign := presignAndUpload(adminToken, "client_package", "duplicate-armv7.apk", "application/vnd.android.package-archive", duplicatePayload)
			duplicate := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/artifacts/complete", server.URL, first.ID), adminToken, map[string]any{
				"platform": "android", "arch": "armv7", "package_type": "apk", "file_name": "duplicate-armv7.apk",
				"object_key": duplicatePresign.ObjectKey, "upload_token": duplicatePresign.UploadToken,
			}, nil)
			clientReleaseE2ERequireStatus(t, duplicate, http.StatusConflict)

			first = publish(first.ID)
			if first.Status != gormdb.ClientReleaseStatusPublished || first.PublishedAt == nil {
				t.Fatalf("first release was not published: %#v", first)
			}
			listResult := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/client-releases?platform=android&arch=armv7", adminToken, nil, nil)
			clientReleaseE2ERequireStatus(t, listResult, http.StatusOK)
			if listed := clientReleaseE2EDecode[clientReleaseListResponse](t, listResult).Data; listed.Total != 1 || len(listed.Items) != 1 {
				t.Fatalf("filtered release list=%#v", listed)
			}

			compatibleQuery := url.Values{
				"platform": {"android"}, "arch": {"armv7"}, "package_type": {"apk"},
				"current_version": {"0.9.0"}, "android_api": {"26"}, "os_version": {"8.0.0"},
			}
			latestResult, selected := latest(compatibleQuery, http.StatusOK, nil)
			if selected == nil || selected.Release.Version != "1.0.0" || selected.Artifact.Arch != "armv7" || selected.Artifact.AndroidABI != "armeabi-v7a" {
				t.Fatalf("selected exact armv7 artifact=%#v", selected)
			}
			if selected.Artifact.SHA256 != fmt.Sprintf("%x", sha256.Sum256(armv7Payload)) || selected.Artifact.URLExpiresAt == nil {
				t.Fatalf("latest artifact integrity metadata=%#v", selected.Artifact)
			}
			download := clientReleaseE2ERaw(t, client, http.MethodGet, selected.Artifact.DownloadURL, "", nil, nil)
			clientReleaseE2ERequireStatus(t, download, http.StatusOK)
			if !bytes.Equal(download.Body, armv7Payload) {
				t.Fatalf("downloaded payload=%q want=%q", download.Body, armv7Payload)
			}

			etag := latestResult.Header.Get("ETag")
			if etag == "" {
				t.Fatal("latest response omitted ETag")
			}
			notModified, _ := latest(compatibleQuery, http.StatusNotModified, http.Header{"If-None-Match": []string{etag}})
			if len(notModified.Body) != 0 {
				t.Fatalf("304 response body=%q", notModified.Body)
			}
			latest(url.Values{"platform": {"android"}, "arch": {"armv7"}, "android_api": {"26"}}, http.StatusNotFound, nil)
			latest(url.Values{"platform": {"android"}, "arch": {"armv7"}, "android_api": {"19"}, "os_version": {"8.0.0"}}, http.StatusNotFound, nil)

			currentQuery := url.Values{
				"platform": {"android"}, "arch": {"arm32"}, "current_version": {"1.0.0"},
				"android_api": {"26"}, "os_version": {"8.0.0"},
			}
			_, noUpdate := latest(currentQuery, http.StatusOK, nil)
			if noUpdate != nil {
				t.Fatalf("current client received update: %#v", noUpdate)
			}

			if variant.name == "local" {
				finalKey := clientReleaseObjectKey(&gormdb.ClientRelease{AppID: appID, Channel: "stable", Version: "1.0.0"}, "android", "armv7", "draarl-1.0.0-armv7.apk")
				permanent := clientReleaseE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+finalKey, "", nil, nil)
				clientReleaseE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			firmwarePayload := []byte("firmware-http-e2e-" + variant.name)
			firmwarePresign := presignAndUpload(adminToken, "firmware", "draarl-e2e-firmware.bin", "application/octet-stream", firmwarePayload)
			firmwareComplete := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/firmware/complete", adminToken, map[string]any{
				"dev_model": 1, "version": "3.2.1", "changelog": "firmware HTTP E2E",
				"file_name": "draarl-e2e-firmware.bin", "object_key": firmwarePresign.ObjectKey,
				"upload_token": firmwarePresign.UploadToken,
			}, nil)
			clientReleaseE2ERequireStatus(t, firmwareComplete, http.StatusOK)
			firmware := clientReleaseE2EDecode[gormdb.FirmwareRelease](t, firmwareComplete).Data
			firmwareIDs = append(firmwareIDs, firmware.ID)
			businessKeys = append(businessKeys, firmware.MinioPath)
			wantFirmwareHash := fmt.Sprintf("%x", sha256.Sum256(firmwarePayload))
			if firmware.FileSize != int64(len(firmwarePayload)) || firmware.FileHash != wantFirmwareHash || !strings.HasPrefix(firmware.MinioPath, "uploads/firmware/") {
				t.Fatalf("firmware complete metadata=%#v", firmware)
			}

			firmwareLatestResult := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/public/firmware/latest?dev_model=1&current_version=3.2.0", "", nil, nil)
			clientReleaseE2ERequireStatus(t, firmwareLatestResult, http.StatusOK)
			firmwareLatest := clientReleaseE2EDecode[*clientReleaseE2EFirmwareLatestData](t, firmwareLatestResult).Data
			if firmwareLatest == nil || firmwareLatest.ID != firmware.ID || firmwareLatest.Version != "3.2.1" || firmwareLatest.FileHash != wantFirmwareHash || firmwareLatest.HashAlgo != "sha256" || !firmwareLatest.HasUpdate {
				t.Fatalf("latest firmware=%#v", firmwareLatest)
			}
			firmwareDownload := clientReleaseE2ERaw(t, client, http.MethodGet, firmwareLatest.DownloadURL, "", nil, nil)
			clientReleaseE2ERequireStatus(t, firmwareDownload, http.StatusOK)
			if !bytes.Equal(firmwareDownload.Body, firmwarePayload) {
				t.Fatalf("firmware payload=%q want=%q", firmwareDownload.Body, firmwarePayload)
			}
			firmwareCurrent := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/public/firmware/latest?dev_model=1&current_version=3.2.1", "", nil, nil)
			clientReleaseE2ERequireStatus(t, firmwareCurrent, http.StatusOK)
			if current := clientReleaseE2EDecode[*clientReleaseE2EFirmwareLatestData](t, firmwareCurrent).Data; current != nil {
				t.Fatalf("current firmware received update: %#v", current)
			}
			if variant.name == "local" {
				permanent := clientReleaseE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+firmware.MinioPath, "", nil, nil)
				clientReleaseE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			folderResult := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/assets/folder", adminToken, map[string]any{
				"name":   "E2E resources " + variant.name + " " + suffix,
				"remark": "asset HTTP E2E",
			}, nil)
			clientReleaseE2ERequireStatus(t, folderResult, http.StatusOK)
			folder := clientReleaseE2EDecode[AssetResponse](t, folderResult).Data
			assetIDs = append(assetIDs, folder.ID)
			if folder.ID == 0 || folder.Type != "folder" {
				t.Fatalf("created asset folder=%#v", folder)
			}

			assetPayload := []byte("asset-http-e2e-" + variant.name)
			assetPresign := presignAndUpload(adminToken, "assets", "e2e-resource.bin", "application/octet-stream", assetPayload)
			assetComplete := clientReleaseE2EJSON(t, client, http.MethodPost, server.URL+"/api/assets/complete", adminToken, map[string]any{
				"parent_id": folder.ID, "name": "e2e-resource.bin", "remark": "asset HTTP E2E",
				"object_key": assetPresign.ObjectKey, "upload_token": assetPresign.UploadToken,
			}, nil)
			clientReleaseE2ERequireStatus(t, assetComplete, http.StatusOK)
			asset := clientReleaseE2EDecode[AssetResponse](t, assetComplete).Data
			assetIDs = append(assetIDs, asset.ID)
			businessKeys = append(businessKeys, asset.Path)
			if asset.ParentID == nil || *asset.ParentID != folder.ID || asset.Size != int64(len(assetPayload)) || !strings.HasPrefix(asset.Path, "uploads/assets/") {
				t.Fatalf("completed asset=%#v", asset)
			}

			folderFiles := clientReleaseE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/assets/folder/%d", server.URL, folder.ID), "", nil, nil)
			clientReleaseE2ERequireStatus(t, folderFiles, http.StatusOK)
			assetDownloadResult := clientReleaseE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/assets/%d/download", server.URL, asset.ID), "", nil, nil)
			clientReleaseE2ERequireStatus(t, assetDownloadResult, http.StatusOK)
			assetDownloadData := clientReleaseE2EDecode[clientReleaseE2EDownloadData](t, assetDownloadResult).Data
			if assetDownloadData.Name != asset.Name || assetDownloadData.Size != int64(len(assetPayload)) || assetDownloadData.DownloadURL == "" {
				t.Fatalf("asset download metadata=%#v", assetDownloadData)
			}
			assetDownload := clientReleaseE2ERaw(t, client, http.MethodGet, assetDownloadData.DownloadURL, "", nil, nil)
			clientReleaseE2ERequireStatus(t, assetDownload, http.StatusOK)
			if !bytes.Equal(assetDownload.Body, assetPayload) {
				t.Fatalf("asset payload=%q want=%q", assetDownload.Body, assetPayload)
			}
			if variant.name == "local" {
				permanent := clientReleaseE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+asset.Path, "", nil, nil)
				clientReleaseE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			certPayload := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n")
			certPresign := presignAndUpload(ordinaryToken, "operator_cert", "e2e-license.pdf", "application/pdf", certPayload)
			certSubmit := clientReleaseE2EMultipart(t, client, http.MethodPost, server.URL+"/api/upload/operator-certificate", ordinaryToken, map[string]string{
				"callsign": ordinary.CallSign, "file_name": "e2e-license.pdf",
				"object_key": certPresign.ObjectKey, "upload_token": certPresign.UploadToken,
			}, "", "", nil)
			clientReleaseE2ERequireStatus(t, certSubmit, http.StatusOK)
			certSubmitData := clientReleaseE2EDecode[clientReleaseE2EOperatorCertSubmitData](t, certSubmit).Data
			operatorCertIDs = append(operatorCertIDs, certSubmitData.ID)
			var cert gormdb.OperatorCert
			if err := db.First(&cert, certSubmitData.ID).Error; err != nil {
				t.Fatalf("load submitted operator certificate: %v", err)
			}
			businessKeys = append(businessKeys, cert.MinioPath)
			if cert.FileName != "e2e-license.pdf" || cert.FileSize != int64(len(certPayload)) || cert.FileType != "application/pdf" || !strings.HasPrefix(cert.MinioPath, "uploads/operator_cert/") {
				t.Fatalf("submitted operator certificate=%#v", cert)
			}
			certStatus := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/operator-certificate", ordinaryToken, nil, nil)
			clientReleaseE2ERequireStatus(t, certStatus, http.StatusOK)
			certStatusData := clientReleaseE2EDecode[clientReleaseE2EOperatorCertData](t, certStatus).Data
			if certStatusData.PendingCert == nil || certStatusData.PendingCert.ID != cert.ID || certStatusData.PendingCert.FileURL == "" || certStatusData.PendingCert.Status != 0 {
				t.Fatalf("operator certificate status=%#v", certStatusData)
			}
			certDownload := clientReleaseE2ERaw(t, client, http.MethodGet, clientReleaseE2EAbsoluteURL(server.URL, certStatusData.PendingCert.FileURL), "", nil, nil)
			clientReleaseE2ERequireStatus(t, certDownload, http.StatusOK)
			if !bytes.Equal(certDownload.Body, certPayload) {
				t.Fatalf("operator certificate payload=%q want=%q", certDownload.Body, certPayload)
			}
			if variant.name == "local" {
				permanent := clientReleaseE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+cert.MinioPath, "", nil, nil)
				clientReleaseE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			avatarPayload := clientReleaseE2EJPEG(t)
			avatarUpload := clientReleaseE2EMultipart(t, client, http.MethodPost, server.URL+"/api/upload/file", ordinaryToken, map[string]string{
				"file_type": "avatar",
			}, "file", "e2e-avatar.jpg", avatarPayload)
			clientReleaseE2ERequireStatus(t, avatarUpload, http.StatusOK)
			avatarData := clientReleaseE2EDecode[UploadResponse](t, avatarUpload).Data
			if !strings.HasPrefix(avatarData.MinioPath, "uploads/avatar/") || avatarData.FileURL == "" || avatarData.ThumbnailURL == "" {
				t.Fatalf("avatar upload=%#v", avatarData)
			}
			businessKeys = append(businessKeys, avatarData.MinioPath, "thumb/"+avatarData.MinioPath)
			avatarDownload := clientReleaseE2ERaw(t, client, http.MethodGet, clientReleaseE2EAbsoluteURL(server.URL, avatarData.FileURL), "", nil, nil)
			clientReleaseE2ERequireStatus(t, avatarDownload, http.StatusOK)
			if _, err := jpeg.Decode(bytes.NewReader(avatarDownload.Body)); err != nil {
				t.Fatalf("decode uploaded avatar: %v", err)
			}
			thumbDownload := clientReleaseE2ERaw(t, client, http.MethodGet, clientReleaseE2EAbsoluteURL(server.URL, avatarData.ThumbnailURL), "", nil, nil)
			clientReleaseE2ERequireStatus(t, thumbDownload, http.StatusOK)
			if _, err := jpeg.Decode(bytes.NewReader(thumbDownload.Body)); err != nil {
				t.Fatalf("decode avatar thumbnail: %v", err)
			}
			if variant.name == "local" && (!strings.HasPrefix(avatarData.FileURL, "/files/") || !strings.HasPrefix(avatarData.ThumbnailURL, "/files/")) {
				t.Fatalf("local avatar URLs should use permanent public routes: %#v", avatarData)
			}

			commPayload := []byte("recording-http-e2e-" + variant.name)
			commKey := "comm-records/2026/07/29/e2e-" + variant.name + "-" + suffix + ".raw"
			if err := storage.Put(context.Background(), commKey, bytes.NewReader(commPayload), int64(len(commPayload)), "application/octet-stream"); err != nil {
				t.Fatalf("put communication recording: %v", err)
			}
			businessKeys = append(businessKeys, commKey)
			ordinaryID := uint(ordinary.ID)
			now := time.Now()
			commRecord := &gormdb.CommRecord{
				DeviceID: 0, DeviceSSID: 105, UserID: &ordinaryID,
				StartTime: now.Add(-time.Second), EndTime: now, DurationMs: 1000,
				AudioPath: commKey, AudioSize: int64(len(commPayload)), Status: 2,
			}
			if err := db.Create(commRecord).Error; err != nil {
				t.Fatalf("create communication record: %v", err)
			}
			commRecordIDs = append(commRecordIDs, commRecord.ID)
			commListResult := clientReleaseE2EJSON(t, client, http.MethodGet, server.URL+"/api/comm-records?page_size=10", ordinaryToken, nil, nil)
			clientReleaseE2ERequireStatus(t, commListResult, http.StatusOK)
			commList := clientReleaseE2EDecode[clientReleaseE2ECommRecordListData](t, commListResult).Data
			if commList.Total != 1 || len(commList.List) != 1 || commList.List[0].ID != commRecord.ID {
				t.Fatalf("communication record list=%#v", commList)
			}
			commResult := clientReleaseE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/comm-records/%d", server.URL, commRecord.ID), ordinaryToken, nil, nil)
			clientReleaseE2ERequireStatus(t, commResult, http.StatusOK)
			commData := clientReleaseE2EDecode[CommRecordResponse](t, commResult).Data
			if commData.ID != commRecord.ID || commData.AudioURL == "" || commData.AudioSize != int64(len(commPayload)) {
				t.Fatalf("communication record=%#v", commData)
			}
			commDownload := clientReleaseE2ERaw(t, client, http.MethodGet, clientReleaseE2EAbsoluteURL(server.URL, commData.AudioURL), "", nil, nil)
			clientReleaseE2ERequireStatus(t, commDownload, http.StatusOK)
			if !bytes.Equal(commDownload.Body, commPayload) {
				t.Fatalf("communication recording payload=%q want=%q", commDownload.Body, commPayload)
			}
			if variant.name == "local" {
				permanent := clientReleaseE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+commKey, "", nil, nil)
				clientReleaseE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			second := createRelease("2.0.0")
			universalPayload := []byte("android-universal-e2e-" + variant.name)
			completeArtifact(second.ID, "universal", "draarl-2.0.0-universal.apk", universalPayload)
			publish(second.ID)
			_, universal := latest(compatibleQuery, http.StatusOK, nil)
			if universal == nil || universal.Release.Version != "2.0.0" || universal.Artifact.Arch != "universal" {
				t.Fatalf("new universal release was not selected: %#v", universal)
			}

			withdraw := clientReleaseE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-releases/%d/withdraw", server.URL, second.ID), adminToken, nil, nil)
			clientReleaseE2ERequireStatus(t, withdraw, http.StatusOK)
			_, fallback := latest(compatibleQuery, http.StatusOK, nil)
			if fallback == nil || fallback.Release.Version != "1.0.0" || fallback.Artifact.Arch != "armv7" {
				t.Fatalf("withdrawal did not restore exact prior artifact: %#v", fallback)
			}

			draft := createRelease("9.0.0")
			deleted := clientReleaseE2EJSON(t, client, http.MethodDelete, fmt.Sprintf("%s/api/client-releases/%d", server.URL, draft.ID), adminToken, nil, nil)
			clientReleaseE2ERequireStatus(t, deleted, http.StatusOK)
			missing := clientReleaseE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/client-releases/%d", server.URL, draft.ID), adminToken, nil, nil)
			clientReleaseE2ERequireStatus(t, missing, http.StatusNotFound)
		})
	}
}

func clientReleaseE2ERouter() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.GET("/files/*key", ServeLocalFile)
	api := engine.Group("/api")
	api.PUT("/storage/put", StorageDirectPut)
	api.GET("/storage/get", StorageDirectGet)
	api.GET("/public/client/latest", GetLatestClientRelease)
	api.GET("/public/firmware/latest", GetLatestFirmware)
	protected := api.Group("")
	protected.Use(middleware.AuthMiddleware(), middleware.LoadUserInfo())
	protected.POST("/storage/presign-put", PresignPut)
	protected.POST("/upload/file", UploadFile)
	protected.POST("/upload/operator-certificate", UploadOperatorCertificate)
	protected.GET("/operator-certificate", GetOperatorCertificate)
	protected.GET("/comm-records", GetCommRecords)
	protected.GET("/comm-records/:id", GetCommRecord)
	admin := protected.Group("")
	admin.Use(middleware.RequireAdmin())
	admin.GET("/client-releases", ListClientReleases)
	admin.POST("/client-releases", CreateClientRelease)
	admin.GET("/client-releases/:id", GetClientRelease)
	admin.POST("/client-releases/:id/artifacts/complete", CompleteClientReleaseArtifact)
	admin.POST("/client-releases/:id/publish", PublishClientRelease)
	admin.POST("/client-releases/:id/withdraw", WithdrawClientRelease)
	admin.DELETE("/client-releases/:id", DeleteClientRelease)
	assetHandler := &AssetHandler{repo: gormdb.NewAssetRepository()}
	admin.POST("/assets/folder", assetHandler.CreateFolder)
	admin.POST("/assets/complete", assetHandler.CompleteUpload)
	admin.POST("/firmware/complete", CompleteFirmwareUpload)
	api.GET("/assets/folder/:id", assetHandler.GetFolderFiles)
	api.GET("/assets/:id/download", assetHandler.GetDownloadURL)
	return engine
}

func clientReleaseE2ES3Profile(t *testing.T) (config.StorageProfile, bool) {
	t.Helper()
	endpoint := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_ENDPOINT"))
	if endpoint == "" {
		return config.StorageProfile{}, false
	}
	required := []string{"DRAARL_S3_TEST_ACCESS_KEY", "DRAARL_S3_TEST_SECRET_KEY", "DRAARL_S3_TEST_BUCKET"}
	for _, name := range required {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			t.Fatalf("%s is required when DRAARL_S3_TEST_ENDPOINT is set", name)
		}
	}
	provider := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_PROVIDER"))
	if provider == "" {
		provider = storage.DriverS3
	}
	sessionToken := ""
	if strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_SESSION_TOKEN")) != "" {
		sessionToken = "${DRAARL_S3_TEST_SESSION_TOKEN}"
	}
	return config.StorageProfile{Driver: storage.DriverS3, S3: config.S3Config{
		Provider: provider, Endpoint: endpoint,
		PresignEndpoint:  strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_PRESIGN_ENDPOINT")),
		Region:           strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_REGION")),
		BucketLookup:     strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET_LOOKUP")),
		AccessKey:        "${DRAARL_S3_TEST_ACCESS_KEY}",
		SecretKey:        "${DRAARL_S3_TEST_SECRET_KEY}",
		SessionToken:     sessionToken,
		UseSSL:           clientReleaseE2EEnvBool(t, "DRAARL_S3_TEST_USE_SSL", false),
		PresignUseSSL:    clientReleaseE2EEnvBool(t, "DRAARL_S3_TEST_PRESIGN_USE_SSL", false),
		Bucket:           strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET")),
		AutoCreateBucket: clientReleaseE2EEnvBool(t, "DRAARL_S3_TEST_AUTO_CREATE_BUCKET", false),
	}}, true
}

func clientReleaseE2EEnvBool(t *testing.T, name string, fallback bool) bool {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", name, value, err)
	}
	return parsed
}

func envEnabled(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func clientReleaseE2EJSON(t *testing.T, client *http.Client, method, target, token string, body any, headers http.Header) clientReleaseE2EHTTPResult {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	if headers == nil {
		headers = make(http.Header)
	}
	if body != nil {
		headers = headers.Clone()
		headers.Set("Content-Type", "application/json")
	}
	return clientReleaseE2ERaw(t, client, method, target, token, payload, headers)
}

func clientReleaseE2EMultipart(t *testing.T, client *http.Client, method, target, token string, fields map[string]string, fileField, fileName string, fileData []byte) clientReleaseE2EHTTPResult {
	t.Helper()
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(fileData); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("Content-Type", writer.FormDataContentType())
	return clientReleaseE2ERaw(t, client, method, target, token, payload.Bytes(), headers)
}

func clientReleaseE2EJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x*30), G: uint8(80 + y*40), B: 160, A: 255})
		}
	}
	var payload bytes.Buffer
	if err := jpeg.Encode(&payload, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}

func clientReleaseE2ERaw(t *testing.T, client *http.Client, method, target, token string, body []byte, headers http.Header) clientReleaseE2EHTTPResult {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return clientReleaseE2EHTTPResult{Status: response.StatusCode, Header: response.Header.Clone(), Body: responseBody}
}

func clientReleaseE2ERequireStatus(t *testing.T, result clientReleaseE2EHTTPResult, want int) {
	t.Helper()
	if result.Status != want {
		t.Fatalf("HTTP status=%d want=%d body=%s", result.Status, want, strings.TrimSpace(string(result.Body)))
	}
}

func clientReleaseE2EDecode[T any](t *testing.T, result clientReleaseE2EHTTPResult) clientReleaseE2EEnvelope[T] {
	t.Helper()
	var envelope clientReleaseE2EEnvelope[T]
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		t.Fatalf("decode response %q: %v", result.Body, err)
	}
	return envelope
}

func clientReleaseE2EFindArtifact(t *testing.T, artifacts []clientArtifactResponse, platform, arch, packageType string) clientArtifactResponse {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Platform == platform && artifact.Arch == arch && artifact.PackageType == packageType {
			return artifact
		}
	}
	t.Fatalf("artifact %s/%s/%s not found in %#v", platform, arch, packageType, artifacts)
	return clientArtifactResponse{}
}

func clientReleaseE2EAbsoluteURL(baseURL, value string) string {
	if strings.HasPrefix(value, "/") {
		return strings.TrimRight(baseURL, "/") + value
	}
	return value
}
