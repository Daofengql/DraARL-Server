package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/buildinfo"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/middleware"
	jwtutil "draarl/pkg/jwt"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
	drivermysql "github.com/go-sql-driver/mysql"
)

const clientResourceE2EEnabledEnv = "DRAARL_CLIENT_RESOURCE_E2E"

type clientResourceE2EEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type clientResourceE2EPresignData struct {
	UploadURL   string            `json:"upload_url"`
	Headers     map[string]string `json:"headers"`
	ObjectKey   string            `json:"object_key"`
	UploadToken string            `json:"upload_token"`
}

type clientResourceE2EStagingItem struct {
	ObjectKey string `json:"object_key"`
	FileName  string `json:"file_name"`
	Size      int64  `json:"size"`
}

type clientResourceE2EStagingListData struct {
	Items []clientResourceE2EStagingItem `json:"items"`
	Total int                            `json:"total"`
}

type clientResourceE2EStagingRetryData struct {
	ObjectKey   string `json:"object_key"`
	UploadToken string `json:"upload_token"`
}

type clientResourceE2EDownloadData struct {
	ArtifactID  int    `json:"artifact_id"`
	DownloadURL string `json:"download_url"`
	ExpiresAt   string `json:"url_expires_at"`
}

type clientResourceE2EFirmware struct {
	ID          int    `json:"id"`
	DevModel    int    `json:"dev_model"`
	Version     string `json:"version"`
	MinioPath   string `json:"minio_path"`
	FileHash    string `json:"file_hash"`
	DownloadURL string `json:"download_url"`
}

type clientResourceE2EHTTPResult struct {
	Status int
	Header http.Header
	Body   []byte
}

func TestClientResourceHTTPE2E(t *testing.T) {
	if !clientResourceE2EEnvEnabled(os.Getenv(clientResourceE2EEnabledEnv)) {
		t.Skip("set " + clientResourceE2EEnabledEnv + "=true and DRAARL_TEST_MYSQL_DSN to run the HTTP E2E")
	}
	dsn := strings.TrimSpace(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Fatal("DRAARL_TEST_MYSQL_DSN is required")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q", parsed.DBName)
	}
	parsed.ParseTime = true
	previousBuildVersion := buildinfo.Version
	buildinfo.Version = "v1.1.5"
	t.Cleanup(func() { buildinfo.Version = previousBuildVersion })
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 10, MaxIdleConns: 2, MaxLifetime: 60, LogLevel: "silent"}); err != nil {
		t.Fatalf("initialize MySQL: %v", err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.ClientResource{}, &gormdb.ClientResourceRelease{}, &gormdb.ClientResourceArtifact{}, &gormdb.ClientResourceArtifactTarget{}, &gormdb.FirmwareRelease{}); err != nil {
		t.Fatalf("migrate E2E tables: %v", err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	admin := &gormdb.User{Name: "resource-admin-" + suffix, Email: "resource-admin-" + suffix + "@example.invalid", CallSign: "RA" + suffix[len(suffix)-8:], Roles: "admin", Status: 1, ApprovalStatus: 1}
	ordinary := &gormdb.User{Name: "resource-user-" + suffix, Email: "resource-user-" + suffix + "@example.invalid", CallSign: "RU" + suffix[len(suffix)-8:], Roles: "user", Status: 1, ApprovalStatus: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(ordinary).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(&gormdb.User{}, []int{admin.ID, ordinary.ID}).Error })

	jwtSecret := "client-resource-e2e-jwt-secret-2026"
	if err := jwtutil.SetSecret(jwtSecret); err != nil {
		t.Fatal(err)
	}
	adminToken, _ := jwtutil.GenerateToken(admin.Name, []string{"admin"})
	ordinaryToken, _ := jwtutil.GenerateToken(ordinary.Name, []string{"user"})
	oldConfig := config.Config
	t.Cleanup(func() { config.Config = oldConfig })

	variants := []struct {
		name    string
		profile func(*testing.T) config.StorageProfile
	}{
		{name: "local", profile: func(t *testing.T) config.StorageProfile {
			return config.StorageProfile{Driver: storage.DriverLocal, Local: config.LocalStorageConfig{RootPath: t.TempDir(), BaseURL: "/files"}}
		}},
	}
	if profile, ok := clientResourceE2ES3Profile(t); ok {
		variants = append(variants, struct {
			name    string
			profile func(*testing.T) config.StorageProfile
		}{name: "s3", profile: func(*testing.T) config.StorageProfile { return profile }})
	}

	for _, variant := range variants {
		variant := variant
		t.Run(variant.name, func(t *testing.T) {
			cfg := &config.Configuration{}
			cfg.JWT.Secret = jwtSecret
			cfg.Storage.UploadLimits.ClientResourceBytes = 8 * 1024 * 1024
			cfg.Storage.ActiveProfile = "client-resource-e2e"
			cfg.Storage.Profiles = map[string]config.StorageProfile{cfg.Storage.ActiveProfile: variant.profile(t)}
			config.Config = cfg
			if err := storage.Init(cfg); err != nil {
				t.Fatalf("initialize %s storage: %v", variant.name, err)
			}

			resourceKey := "model/e2e-" + variant.name + "-" + suffix
			stagingKeys := make([]string, 0, 4)
			var resourceID, releaseID, artifactID, appendedArtifactID, firmwareID int
			var firmwareKey string
			t.Cleanup(func() {
				for _, key := range stagingKeys {
					_ = storage.Delete(context.Background(), key)
				}
				var artifacts []gormdb.ClientResourceArtifact
				_ = db.Where("release_id = ?", releaseID).Find(&artifacts).Error
				artifactIDs := make([]int, 0, len(artifacts))
				for _, artifact := range artifacts {
					artifactIDs = append(artifactIDs, artifact.ID)
					if artifact.StorageKey != "" {
						_ = storage.Delete(context.Background(), artifact.StorageKey)
					}
				}
				if len(artifactIDs) > 0 {
					_ = db.Where("artifact_id IN ?", artifactIDs).Delete(&gormdb.ClientResourceArtifactTarget{}).Error
				}
				if releaseID != 0 {
					_ = db.Where("release_id = ?", releaseID).Delete(&gormdb.ClientResourceArtifact{}).Error
					_ = db.Delete(&gormdb.ClientResourceRelease{}, releaseID).Error
				}
				if resourceID != 0 {
					_ = db.Delete(&gormdb.ClientResource{}, resourceID).Error
				}
				if firmwareID != 0 {
					_ = db.Delete(&gormdb.FirmwareRelease{}, firmwareID).Error
				}
				if firmwareKey != "" {
					_ = storage.Delete(context.Background(), firmwareKey)
				}
			})

			gin.SetMode(gin.TestMode)
			server := httptest.NewServer(clientResourceE2ERouter())
			t.Cleanup(server.Close)
			client := server.Client()

			createBody := map[string]any{"resource_key": resourceKey, "name": "E2E denoise model", "category": "model", "required": true, "enabled": true}
			clientResourceE2ERequireStatus(t, clientResourceE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-resources", "", createBody, nil), http.StatusUnauthorized)
			clientResourceE2ERequireStatus(t, clientResourceE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-resources", ordinaryToken, createBody, nil), http.StatusForbidden)
			createdResult := clientResourceE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-resources", adminToken, createBody, nil)
			clientResourceE2ERequireStatus(t, createdResult, http.StatusOK)
			resource := clientResourceE2EDecode[clientResourceResponse](t, createdResult).Data
			resourceID = resource.ID

			releaseResult := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases", server.URL, resourceID), adminToken, map[string]any{
				"version": "1.0.0", "channel": "stable", "title": "E2E model", "min_client_version": "1.0.0",
				"min_server_version": "1.1.0", "required_protocol_version": 1,
				"required_capabilities": []string{"source_group_v1", "multi_receive_v1"},
			}, nil)
			clientResourceE2ERequireStatus(t, releaseResult, http.StatusOK)
			release := clientResourceE2EDecode[clientResourceReleaseResponse](t, releaseResult).Data
			releaseID = release.ID
			if release.MinServerVersion != "1.1.0" || release.RequiredProtocolVersion != 1 || len(release.RequiredCapabilities) != 2 || release.RequiredCapabilities[0] != "multi_receive_v1" {
				t.Fatalf("release contract=%#v", release)
			}

			payload := []byte("client-resource-multi-target-e2e-" + variant.name)
			presign := clientResourceE2EPresignAndUpload(t, client, server.URL, adminToken, "client_resource", "denoise.onnx", "application/octet-stream", payload)
			stagingKeys = append(stagingKeys, presign.ObjectKey)
			stagingList := clientResourceE2EJSON(t, client, http.MethodGet, server.URL+"/api/client-resources/staging", adminToken, nil, nil)
			clientResourceE2ERequireStatus(t, stagingList, http.StatusOK)
			stagingItems := clientResourceE2EDecode[clientResourceE2EStagingListData](t, stagingList).Data
			if stagingItems.Total < 1 || len(stagingItems.Items) < 1 {
				t.Fatalf("staging list=%#v", stagingItems)
			}
			retryResult := clientResourceE2EJSON(t, client, http.MethodPost, server.URL+"/api/client-resources/staging/retry", adminToken, map[string]any{"object_key": presign.ObjectKey}, nil)
			clientResourceE2ERequireStatus(t, retryResult, http.StatusOK)
			retry := clientResourceE2EDecode[clientResourceE2EStagingRetryData](t, retryResult).Data
			if retry.ObjectKey != presign.ObjectKey || retry.UploadToken == "" {
				t.Fatalf("staging retry=%#v", retry)
			}
			completeBody := map[string]any{
				"format": "onnx", "runtime": "cpu", "variant": "default", "file_name": "denoise.onnx",
				"object_key": retry.ObjectKey, "upload_token": retry.UploadToken,
				"metadata": map[string]any{"sample_rate": 48000},
				"targets":  []map[string]any{{"platform": "windows", "arch": "x86_64"}, {"platform": "macos", "arch": "arm64", "min_os_version": "13.0.0"}},
			}
			completeResult := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases/%d/artifacts/complete", server.URL, resourceID, releaseID), adminToken, completeBody, nil)
			clientResourceE2ERequireStatus(t, completeResult, http.StatusOK)
			release = clientResourceE2EDecode[clientResourceReleaseResponse](t, completeResult).Data
			if len(release.Artifacts) != 1 || len(release.Artifacts[0].Targets) != 2 || release.Artifacts[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256(payload)) {
				t.Fatalf("completed release=%#v", release)
			}
			if release.Artifacts[0].StorageKey == "" || !strings.HasPrefix(release.Artifacts[0].StorageKey, "client-resources/") {
				t.Fatalf("admin response omitted final storage key: %#v", release.Artifacts[0])
			}
			artifactStorageKey := release.Artifacts[0].StorageKey
			artifactID = release.Artifacts[0].ID
			draftDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, draftDownload, http.StatusNotFound)

			duplicatePayload := []byte("duplicate-target")
			duplicatePresign := clientResourceE2EPresignAndUpload(t, client, server.URL, adminToken, "client_resource", "duplicate.onnx", "application/octet-stream", duplicatePayload)
			stagingKeys = append(stagingKeys, duplicatePresign.ObjectKey)
			duplicate := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases/%d/artifacts/complete", server.URL, resourceID, releaseID), adminToken, map[string]any{
				"format": "onnx", "runtime": "cpu", "variant": "default", "file_name": "duplicate.onnx", "object_key": duplicatePresign.ObjectKey, "upload_token": duplicatePresign.UploadToken,
				"targets": []map[string]any{{"platform": "macos", "arch": "arm64"}},
			}, nil)
			clientResourceE2ERequireStatus(t, duplicate, http.StatusConflict)

			if err := db.Model(&gormdb.ClientResourceRelease{}).Where("id = ?", releaseID).Update("min_server_version", "9.0.0").Error; err != nil {
				t.Fatal(err)
			}
			incompatiblePublish := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases/%d/publish", server.URL, resourceID, releaseID), adminToken, nil, nil)
			clientResourceE2ERequireStatus(t, incompatiblePublish, http.StatusConflict)
			if err := db.Model(&gormdb.ClientResourceRelease{}).Where("id = ?", releaseID).Update("min_server_version", "1.1.0").Error; err != nil {
				t.Fatal(err)
			}
			publish := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases/%d/publish", server.URL, resourceID, releaseID), adminToken, nil, nil)
			clientResourceE2ERequireStatus(t, publish, http.StatusOK)

			manifestURL := server.URL + "/api/public/client-resources/manifest?platform=windows&arch=x86_64&channel=beta&client_version=1.0.0"
			if err := db.Model(&gormdb.ClientResourceRelease{}).Where("id = ?", releaseID).Update("min_server_version", "9.0.0").Error; err != nil {
				t.Fatal(err)
			}
			incompatibleManifestResult := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, incompatibleManifestResult, http.StatusOK)
			incompatibleManifest := clientResourceE2EDecode[clientResourceManifestResponse](t, incompatibleManifestResult).Data
			if incompatibleManifest.ServerVersion != "1.1.5" || incompatibleManifest.ProtocolVersion != 1 || len(incompatibleManifest.Capabilities) != 2 || len(incompatibleManifest.Resources) != 0 {
				t.Fatalf("incompatible manifest=%#v", incompatibleManifest)
			}
			incompatibleDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, incompatibleDownload, http.StatusNotFound)
			if err := db.Model(&gormdb.ClientResourceRelease{}).Where("id = ?", releaseID).Update("min_server_version", "1.1.0").Error; err != nil {
				t.Fatal(err)
			}
			manifestResult := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, manifestResult, http.StatusOK)
			manifest := clientResourceE2EDecode[clientResourceManifestResponse](t, manifestResult).Data
			if manifest.SchemaVersion != 1 || manifest.ServerVersion != "1.1.5" || manifest.ProtocolVersion != 1 || len(manifest.Capabilities) != 2 || len(manifest.Resources) != 1 || manifest.Resources[0].Release.Channel != "stable" || manifest.Resources[0].Release.MinServerVersion != "1.1.0" || len(manifest.Resources[0].Release.RequiredCapabilities) != 2 || len(manifest.Resources[0].Artifacts) != 1 {
				t.Fatalf("manifest=%#v", manifest)
			}
			if bytes.Contains(manifestResult.Body, []byte("download_url")) || bytes.Contains(manifestResult.Body, []byte("storage_key")) {
				t.Fatalf("manifest leaked transport data: %s", manifestResult.Body)
			}
			etag := manifestResult.Header.Get("ETag")
			if etag == "" {
				t.Fatal("manifest omitted ETag")
			}
			notModified := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, http.Header{"If-None-Match": []string{etag}})
			clientResourceE2ERequireStatus(t, notModified, http.StatusNotModified)
			notFoundDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID+999999), "", nil, nil)
			clientResourceE2ERequireStatus(t, notFoundDownload, http.StatusNotFound)
			disableResource := clientResourceE2EJSON(t, client, http.MethodPatch, fmt.Sprintf("%s/api/client-resources/%d", server.URL, resourceID), adminToken, map[string]any{"enabled": false}, nil)
			clientResourceE2ERequireStatus(t, disableResource, http.StatusOK)
			disabledManifest := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, disabledManifest, http.StatusOK)
			if disabledManifest.Header.Get("ETag") == "" || disabledManifest.Header.Get("ETag") == etag {
				t.Fatalf("disable did not change manifest etag: before=%q after=%q", etag, disabledManifest.Header.Get("ETag"))
			}
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, disabledManifest).Data; len(got.Resources) != 0 {
				t.Fatalf("disabled resource remained in manifest: %#v", got)
			}
			disabledDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, disabledDownload, http.StatusNotFound)
			enableResource := clientResourceE2EJSON(t, client, http.MethodPatch, fmt.Sprintf("%s/api/client-resources/%d", server.URL, resourceID), adminToken, map[string]any{"enabled": true}, nil)
			clientResourceE2ERequireStatus(t, enableResource, http.StatusOK)
			linux := clientResourceE2EJSON(t, client, http.MethodGet, server.URL+"/api/public/client-resources/manifest?platform=linux&arch=x86_64&client_version=1.0.0", "", nil, nil)
			clientResourceE2ERequireStatus(t, linux, http.StatusOK)
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, linux).Data; len(got.Resources) != 0 {
				t.Fatalf("unselected platform received resources: %#v", got)
			}

			appendedPayload := []byte("client-resource-published-append-e2e-" + variant.name)
			appendPresign := clientResourceE2EPresignAndUpload(t, client, server.URL, adminToken, "client_resource", "denoise-linux.onnx", "application/octet-stream", appendedPayload)
			stagingKeys = append(stagingKeys, appendPresign.ObjectKey)
			appendResult := clientResourceE2EJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/client-resources/%d/releases/%d/artifacts/complete", server.URL, resourceID, releaseID), adminToken, map[string]any{
				"format": "onnx", "runtime": "gpu", "variant": "default", "file_name": "denoise-linux.onnx",
				"object_key": appendPresign.ObjectKey, "upload_token": appendPresign.UploadToken,
				"metadata": map[string]any{"backend": "cuda"},
				"targets":  []map[string]any{{"platform": "linux", "arch": "x86_64"}},
			}, nil)
			clientResourceE2ERequireStatus(t, appendResult, http.StatusOK)
			release = clientResourceE2EDecode[clientResourceReleaseResponse](t, appendResult).Data
			var appendedArtifactStorageKey string
			for _, candidate := range release.Artifacts {
				if candidate.FileName == "denoise-linux.onnx" {
					appendedArtifactID = candidate.ID
					appendedArtifactStorageKey = candidate.StorageKey
					if candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256(appendedPayload)) || len(candidate.Targets) != 1 {
						t.Fatalf("appended artifact=%#v", candidate)
					}
				}
			}
			if len(release.Artifacts) != 2 || appendedArtifactID == 0 || appendedArtifactStorageKey == "" {
				t.Fatalf("published append release=%#v", release)
			}
			linuxManifestURL := server.URL + "/api/public/client-resources/manifest?platform=linux&arch=x86_64&channel=stable&client_version=1.0.0"
			linuxManifest := clientResourceE2EJSON(t, client, http.MethodGet, linuxManifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, linuxManifest, http.StatusOK)
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, linuxManifest).Data; len(got.Resources) != 1 || got.Resources[0].Release.ID != releaseID || len(got.Resources[0].Artifacts) != 1 || got.Resources[0].Artifacts[0].ID != appendedArtifactID {
				t.Fatalf("linux appended manifest=%#v", got)
			}
			windowsAfterAppend := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, windowsAfterAppend, http.StatusOK)
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, windowsAfterAppend).Data; len(got.Resources) != 1 || got.Resources[0].Artifacts[0].ID != artifactID {
				t.Fatalf("original target disappeared after append: %#v", got)
			}

			downloadResult := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, downloadResult, http.StatusOK)
			downloadData := clientResourceE2EDecode[clientResourceE2EDownloadData](t, downloadResult).Data
			if downloadData.DownloadURL == "" || downloadData.ExpiresAt == "" {
				t.Fatalf("download data=%#v", downloadData)
			}
			download := clientResourceE2ERaw(t, client, http.MethodGet, downloadData.DownloadURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, download, http.StatusOK)
			if !bytes.Equal(download.Body, payload) {
				t.Fatalf("download body=%q", download.Body)
			}
			previousExpiry := clientResourceDownloadURLExpiry
			clientResourceDownloadURLExpiry = 2 * time.Second
			t.Cleanup(func() { clientResourceDownloadURLExpiry = previousExpiry })
			shortDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, shortDownload, http.StatusOK)
			shortData := clientResourceE2EDecode[clientResourceE2EDownloadData](t, shortDownload).Data
			time.Sleep(4 * time.Second)
			expiredDownload := clientResourceE2ERaw(t, client, http.MethodGet, shortData.DownloadURL, "", nil, nil)
			if expiredDownload.Status < http.StatusBadRequest {
				t.Fatalf("expired download URL remained usable: status=%d", expiredDownload.Status)
			}
			refreshedDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, refreshedDownload, http.StatusOK)
			refreshedData := clientResourceE2EDecode[clientResourceE2EDownloadData](t, refreshedDownload).Data
			resumed := clientResourceE2ERaw(t, client, http.MethodGet, refreshedData.DownloadURL, "", nil, http.Header{"Range": []string{"bytes=7-14"}})
			clientResourceE2ERequireStatus(t, resumed, http.StatusPartialContent)
			if !bytes.Equal(resumed.Body, payload[7:15]) {
				t.Fatalf("resumed range body=%q want=%q", resumed.Body, payload[7:15])
			}
			rangeResult := clientResourceE2ERaw(t, client, http.MethodGet, refreshedData.DownloadURL, "", nil, http.Header{"Range": []string{"bytes=7-14"}})
			clientResourceE2ERequireStatus(t, rangeResult, http.StatusPartialContent)
			if !bytes.Equal(rangeResult.Body, payload[7:15]) {
				t.Fatalf("range body=%q want=%q", rangeResult.Body, payload[7:15])
			}
			if variant.name == "local" {
				var stored gormdb.ClientResourceArtifact
				if err := db.First(&stored, artifactID).Error; err != nil {
					t.Fatal(err)
				}
				permanent := clientResourceE2ERaw(t, client, http.MethodGet, server.URL+"/files/"+stored.StorageKey, "", nil, nil)
				clientResourceE2ERequireStatus(t, permanent, http.StatusNotFound)
			}

			firmwarePayload := []byte("firmware-boundary-" + variant.name)
			firmwarePresign := clientResourceE2EPresignAndUpload(t, client, server.URL, adminToken, "firmware", "device.bin", "application/octet-stream", firmwarePayload)
			stagingKeys = append(stagingKeys, firmwarePresign.ObjectKey)
			firmwareComplete := clientResourceE2EJSON(t, client, http.MethodPost, server.URL+"/api/firmware/complete", adminToken, map[string]any{"dev_model": 1, "version": "9.9.1", "file_name": "device.bin", "object_key": firmwarePresign.ObjectKey, "upload_token": firmwarePresign.UploadToken}, nil)
			clientResourceE2ERequireStatus(t, firmwareComplete, http.StatusOK)
			firmware := clientResourceE2EDecode[clientResourceE2EFirmware](t, firmwareComplete).Data
			firmwareID, firmwareKey = firmware.ID, firmware.MinioPath
			if !strings.HasPrefix(firmwareKey, "firmware/1/9.9.1/") || !strings.HasSuffix(firmwareKey, "/device.bin") {
				t.Fatalf("unexpected immutable firmware key %q", firmwareKey)
			}
			firmwareLatest := clientResourceE2EJSON(t, client, http.MethodGet, server.URL+"/api/public/firmware/latest?dev_model=1&current_version=9.9.0", "", nil, nil)
			clientResourceE2ERequireStatus(t, firmwareLatest, http.StatusOK)
			latestFirmware := clientResourceE2EDecode[clientResourceE2EFirmware](t, firmwareLatest).Data
			if latestFirmware.ID != firmwareID || latestFirmware.DownloadURL == "" {
				t.Fatalf("firmware latest=%#v", latestFirmware)
			}

			deleteRelease := clientResourceE2EJSON(t, client, http.MethodDelete, fmt.Sprintf("%s/api/client-resources/%d/releases/%d", server.URL, resourceID, releaseID), adminToken, nil, nil)
			clientResourceE2ERequireStatus(t, deleteRelease, http.StatusOK)
			deletedRelease := clientResourceE2EDecode[clientResourceReleaseDeleteResponse](t, deleteRelease).Data
			if deletedRelease.DeletedArtifacts != 2 || deletedRelease.DeletedObjects != 2 || deletedRelease.ObjectCleanupFailures != 0 {
				t.Fatalf("delete release cleanup=%#v body=%s", deletedRelease, deleteRelease.Body)
			}
			deletedWindowsManifest := clientResourceE2EJSON(t, client, http.MethodGet, manifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, deletedWindowsManifest, http.StatusOK)
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, deletedWindowsManifest).Data; len(got.Resources) != 0 {
				t.Fatalf("deleted release remained in windows manifest: %#v", got)
			}
			deletedLinuxManifest := clientResourceE2EJSON(t, client, http.MethodGet, linuxManifestURL, "", nil, nil)
			clientResourceE2ERequireStatus(t, deletedLinuxManifest, http.StatusOK)
			if got := clientResourceE2EDecode[clientResourceManifestResponse](t, deletedLinuxManifest).Data; len(got.Resources) != 0 {
				t.Fatalf("deleted release remained in linux manifest: %#v", got)
			}
			deletedOriginalDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, artifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, deletedOriginalDownload, http.StatusNotFound)
			deletedAppendedDownload := clientResourceE2EJSON(t, client, http.MethodGet, fmt.Sprintf("%s/api/public/client-resources/artifacts/%d/download", server.URL, appendedArtifactID), "", nil, nil)
			clientResourceE2ERequireStatus(t, deletedAppendedDownload, http.StatusNotFound)
			if _, _, err := storage.Stat(context.Background(), artifactStorageKey); err == nil {
				t.Fatalf("delete release retained client resource object %q", artifactStorageKey)
			}
			if _, _, err := storage.Stat(context.Background(), appendedArtifactStorageKey); err == nil {
				t.Fatalf("delete release retained appended client resource object %q", appendedArtifactStorageKey)
			}
			for name, model := range map[string]any{
				"releases": &gormdb.ClientResourceRelease{}, "artifacts": &gormdb.ClientResourceArtifact{},
				"targets": &gormdb.ClientResourceArtifactTarget{},
			} {
				var count int64
				query := db.Model(model)
				switch name {
				case "releases":
					query = query.Where("resource_id = ?", resourceID)
				case "artifacts":
					query = query.Where("release_id = ?", releaseID)
				case "targets":
					query = query.Where("artifact_id IN ?", []int{artifactID, appendedArtifactID})
				}
				if err := query.Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("release delete %s count=%d err=%v", name, count, err)
				}
			}

			deleteResource := clientResourceE2EJSON(t, client, http.MethodDelete, fmt.Sprintf("%s/api/client-resources/%d", server.URL, resourceID), adminToken, nil, nil)
			clientResourceE2ERequireStatus(t, deleteResource, http.StatusOK)
			for name, model := range map[string]any{
				"resources": &gormdb.ClientResource{},
			} {
				var count int64
				query := db.Model(model).Where("id = ?", resourceID)
				if err := query.Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("cascade delete %s count=%d err=%v", name, count, err)
				}
			}
			if _, _, err := storage.Stat(context.Background(), firmwareKey); err != nil {
				t.Fatalf("client resource cascade deleted firmware object: %v", err)
			}
		})
	}
}

func clientResourceE2ERouter() *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.GET("/files/*key", ServeLocalFile)
	api := engine.Group("/api")
	api.PUT("/storage/put", StorageDirectPut)
	api.GET("/storage/get", StorageDirectGet)
	api.GET("/public/client-resources/manifest", GetClientResourceManifest)
	api.GET("/public/client-resources/artifacts/:artifact_id/download", GetClientResourceArtifactDownload)
	api.GET("/public/firmware/latest", GetLatestFirmware)
	protected := api.Group("")
	protected.Use(middleware.AuthMiddleware(), middleware.LoadUserInfo())
	protected.POST("/storage/presign-put", PresignPut)
	admin := protected.Group("")
	admin.Use(middleware.RequireAdmin())
	admin.GET("/client-resources", ListClientResources)
	admin.POST("/client-resources", CreateClientResource)
	admin.GET("/client-resources/staging", ListClientResourceStaging)
	admin.POST("/client-resources/staging/retry", RetryClientResourceStaging)
	admin.GET("/client-resources/:resource_id", GetClientResource)
	admin.PATCH("/client-resources/:resource_id", UpdateClientResource)
	admin.DELETE("/client-resources/:resource_id", DeleteClientResource)
	admin.GET("/client-resources/:resource_id/releases", ListClientResourceReleases)
	admin.POST("/client-resources/:resource_id/releases", CreateClientResourceRelease)
	admin.GET("/client-resources/:resource_id/releases/:release_id", GetClientResourceRelease)
	admin.POST("/client-resources/:resource_id/releases/:release_id/artifacts/complete", CompleteClientResourceArtifact)
	admin.POST("/client-resources/:resource_id/releases/:release_id/publish", PublishClientResourceRelease)
	admin.DELETE("/client-resources/:resource_id/releases/:release_id", DeleteClientResourceRelease)
	admin.POST("/firmware/complete", CompleteFirmwareUpload)
	return engine
}

func clientResourceE2EPresignAndUpload(t *testing.T, client *http.Client, baseURL, token, fileType, fileName, contentType string, payload []byte) clientResourceE2EPresignData {
	t.Helper()
	result := clientResourceE2EJSON(t, client, http.MethodPost, baseURL+"/api/storage/presign-put", token, map[string]any{"file_type": fileType, "file_name": fileName, "size": len(payload), "content_type": contentType}, nil)
	clientResourceE2ERequireStatus(t, result, http.StatusOK)
	presign := clientResourceE2EDecode[clientResourceE2EPresignData](t, result).Data
	headers := make(http.Header)
	for name, value := range presign.Headers {
		headers.Set(name, value)
	}
	put := clientResourceE2ERaw(t, client, http.MethodPut, presign.UploadURL, "", payload, headers)
	if put.Status < 200 || put.Status >= 300 {
		t.Fatalf("presigned PUT status=%d body=%s", put.Status, put.Body)
	}
	return presign
}

func clientResourceE2ES3Profile(t *testing.T) (config.StorageProfile, bool) {
	t.Helper()
	endpoint := strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_ENDPOINT"))
	if endpoint == "" {
		return config.StorageProfile{}, false
	}
	for _, name := range []string{"DRAARL_S3_TEST_ACCESS_KEY", "DRAARL_S3_TEST_SECRET_KEY", "DRAARL_S3_TEST_BUCKET"} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			t.Fatalf("%s is required", name)
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
		Provider: provider, Endpoint: endpoint, PresignEndpoint: strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_PRESIGN_ENDPOINT")), Region: strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_REGION")), BucketLookup: strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET_LOOKUP")), AccessKey: "${DRAARL_S3_TEST_ACCESS_KEY}", SecretKey: "${DRAARL_S3_TEST_SECRET_KEY}", SessionToken: sessionToken, UseSSL: clientResourceE2EEnvBool(t, "DRAARL_S3_TEST_USE_SSL", false), PresignUseSSL: clientResourceE2EEnvBool(t, "DRAARL_S3_TEST_PRESIGN_USE_SSL", false), Bucket: strings.TrimSpace(os.Getenv("DRAARL_S3_TEST_BUCKET")), AutoCreateBucket: clientResourceE2EEnvBool(t, "DRAARL_S3_TEST_AUTO_CREATE_BUCKET", false),
	}}, true
}

func clientResourceE2EEnvBool(t *testing.T, name string, fallback bool) bool {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		t.Fatalf("invalid %s=%q", name, value)
	}
	return parsed
}
func clientResourceE2EEnvEnabled(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func clientResourceE2EJSON(t *testing.T, client *http.Client, method, target, token string, body any, headers http.Header) clientResourceE2EHTTPResult {
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
		headers.Set("Content-Type", "application/json")
	}
	return clientResourceE2ERaw(t, client, method, target, token, payload, headers)
}

func clientResourceE2ERaw(t *testing.T, client *http.Client, method, target, token string, body []byte, headers http.Header) clientResourceE2EHTTPResult {
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
	return clientResourceE2EHTTPResult{Status: response.StatusCode, Header: response.Header.Clone(), Body: responseBody}
}

func clientResourceE2ERequireStatus(t *testing.T, result clientResourceE2EHTTPResult, want int) {
	t.Helper()
	if result.Status != want {
		t.Fatalf("HTTP status=%d want=%d body=%s", result.Status, want, strings.TrimSpace(string(result.Body)))
	}
}
func clientResourceE2EDecode[T any](t *testing.T, result clientResourceE2EHTTPResult) clientResourceE2EEnvelope[T] {
	t.Helper()
	var envelope clientResourceE2EEnvelope[T]
	if err := json.Unmarshal(result.Body, &envelope); err != nil {
		t.Fatalf("decode %q: %v", result.Body, err)
	}
	return envelope
}
