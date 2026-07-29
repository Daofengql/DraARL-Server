package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gormdb "draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
)

type clientReleaseArtifactListerStub struct {
	artifacts []*gormdb.ClientReleaseArtifact
	err       error
	query     gormdb.ClientArtifactLookup
	calls     int
}

func (stub *clientReleaseArtifactListerStub) ListPublishedArtifacts(query gormdb.ClientArtifactLookup) ([]*gormdb.ClientReleaseArtifact, error) {
	stub.calls++
	stub.query = query
	return stub.artifacts, stub.err
}

type clientLatestTestEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func performClientLatestRequest(
	t *testing.T,
	target string,
	repository clientReleaseArtifactLister,
	storageEnabled func() bool,
	presignGet clientReleasePresignGet,
	now time.Time,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	getLatestClientRelease(c, repository, storageEnabled, presignGet, func() time.Time { return now })
	return recorder
}

func decodeClientLatestEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) clientLatestTestEnvelope {
	t.Helper()
	var envelope clientLatestTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return envelope
}

func TestClientReleaseAdminHandlersRejectMissingAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		method  string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "create", method: http.MethodPost, path: "/api/client-releases", handler: CreateClientRelease},
		{name: "complete artifact", method: http.MethodPost, path: "/api/client-releases/1/artifacts/complete", handler: CompleteClientReleaseArtifact},
		{name: "publish", method: http.MethodPost, path: "/api/client-releases/1/publish", handler: PublishClientRelease},
		{name: "withdraw", method: http.MethodPost, path: "/api/client-releases/1/withdraw", handler: WithdrawClientRelease},
		{name: "delete", method: http.MethodDelete, path: "/api/client-releases/1", handler: DeleteClientRelease},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(tc.method, tc.path, nil)
			context.Params = gin.Params{{Key: "id", Value: "1"}}
			tc.handler(context)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestNormalizeClientTargetAndroidABI(t *testing.T) {
	tests := []struct {
		name     string
		arch     string
		abi      string
		wantArch string
		wantABI  string
		wantErr  bool
	}{
		{name: "armv7 alias", arch: "armeabi-v7a", wantArch: "armv7", wantABI: "armeabi-v7a"},
		{name: "arm64 alias", arch: "aarch64", wantArch: "arm64", wantABI: "arm64-v8a"},
		{name: "universal", arch: "universal", wantArch: "universal", wantABI: "universal"},
		{name: "wrong abi", arch: "armv7", abi: "arm64-v8a", wantErr: true},
		{name: "universal abi with exact arch", arch: "armv7", abi: "universal", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, arch, abi, _, err := normalizeClientTarget("android", tc.arch, tc.abi, "apk")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected target validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if arch != tc.wantArch || abi != tc.wantABI {
				t.Fatalf("target=%s/%s, want=%s/%s", arch, abi, tc.wantArch, tc.wantABI)
			}
		})
	}
}

func TestNormalizeClientTargetRejectsAndroidABIForOtherPlatforms(t *testing.T) {
	if _, _, _, _, err := normalizeClientTarget("windows", "x86_64", "arm64-v8a", "exe"); err == nil {
		t.Fatal("android_abi must be rejected for non-Android artifacts")
	}
}

func TestNormalizeClientTargetAppStoreRequiresUniversal(t *testing.T) {
	if _, _, _, _, err := normalizeClientTarget("ios", "arm64", "", "app_store"); err == nil {
		t.Fatal("app_store arm64 target should be rejected")
	}
	platform, arch, _, packageType, err := normalizeClientTarget("ios", "universal", "", "app_store")
	if err != nil || platform != "ios" || arch != "universal" || packageType != "app_store" {
		t.Fatalf("universal app_store target=(%q,%q,%q) err=%v", platform, arch, packageType, err)
	}
}

func TestNormalizeClientArtifactMetadata(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		request  clientReleaseArtifactCompleteRequest
		wantErr  bool
	}{
		{name: "android API", platform: "android", request: clientReleaseArtifactCompleteRequest{MinAndroidAPI: 26, MinOSVersion: "8.0.0"}},
		{name: "non Android API", platform: "windows", request: clientReleaseArtifactCompleteRequest{MinAndroidAPI: 26}, wantErr: true},
		{name: "long build number", platform: "android", request: clientReleaseArtifactCompleteRequest{BuildNumber: strings.Repeat("1", clientReleaseMaxBuildNumber+1)}, wantErr: true},
		{name: "long signature algorithm", platform: "android", request: clientReleaseArtifactCompleteRequest{SignatureAlgorithm: strings.Repeat("a", clientReleaseMaxSignatureAlgo+1)}, wantErr: true},
		{name: "signature without algorithm", platform: "android", request: clientReleaseArtifactCompleteRequest{Signature: "signed"}, wantErr: true},
		{name: "algorithm without signature", platform: "android", request: clientReleaseArtifactCompleteRequest{SignatureAlgorithm: "ed25519"}, wantErr: true},
		{name: "complete signature metadata", platform: "android", request: clientReleaseArtifactCompleteRequest{Signature: "signed", SignatureAlgorithm: "ed25519"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeClientArtifactMetadata(tc.platform, tc.request)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func TestAppStoreExternalURLRequiresHTTPS(t *testing.T) {
	if isHTTPSURL("http://apps.example.com/client") {
		t.Fatal("plain HTTP App Store URL must be rejected")
	}
	if !isHTTPSURL("https://apps.apple.com/app/id123") {
		t.Fatal("valid HTTPS App Store URL was rejected")
	}
}

func TestChooseClientArtifactPrefersExactABIAndCompatibleVersion(t *testing.T) {
	now := time.Now()
	old := &gormdb.ClientRelease{ID: 1, Version: "1.1.0", Channel: "stable", CreateTime: now.Add(-time.Minute)}
	newRelease := &gormdb.ClientRelease{ID: 2, Version: "1.2.0", Channel: "stable", CreateTime: now}
	artifacts := []*gormdb.ClientReleaseArtifact{
		{ID: 10, Release: old, Platform: "android", Arch: "arm64", MinAndroidAPI: 21},
		{ID: 11, Release: newRelease, Platform: "android", Arch: "universal", MinAndroidAPI: 26},
		{ID: 12, Release: newRelease, Platform: "android", Arch: "arm64", MinAndroidAPI: 21},
	}
	got := chooseClientArtifact(artifacts, clientLatestRequest{Platform: "android", Arch: "arm64", AndroidAPI: 26})
	if got == nil || got.ID != 12 {
		t.Fatalf("got artifact %#v, want exact arm64 artifact", got)
	}

	got = chooseClientArtifact(artifacts, clientLatestRequest{Platform: "android", Arch: "arm64", AndroidAPI: 22})
	if got == nil || got.ID != 12 {
		t.Fatalf("got artifact %#v, want compatible exact artifact", got)
	}
}

func TestChooseClientArtifactUsesNewerUniversalRelease(t *testing.T) {
	now := time.Now()
	exactRelease := &gormdb.ClientRelease{ID: 1, Version: "1.0.0", CreateTime: now.Add(-time.Minute)}
	universalRelease := &gormdb.ClientRelease{ID: 2, Version: "2.0.0", CreateTime: now}
	artifacts := []*gormdb.ClientReleaseArtifact{
		{ID: 1, Release: exactRelease, Platform: "android", Arch: "armv7"},
		{ID: 2, Release: universalRelease, Platform: "android", Arch: "universal"},
	}
	got := chooseClientArtifact(artifacts, clientLatestRequest{Platform: "android", Arch: "armv7"})
	if got == nil || got.ID != 2 {
		t.Fatalf("got artifact %#v, want newer universal release", got)
	}

	got = chooseClientArtifact(artifacts[1:], clientLatestRequest{Platform: "android", Arch: "armv7"})
	if got == nil || got.ID != 2 {
		t.Fatalf("got artifact %#v, want universal fallback when no exact ABI exists", got)
	}
}

func TestChooseClientArtifactRequiresOSVersionForMinOSArtifact(t *testing.T) {
	release := &gormdb.ClientRelease{ID: 1, Version: "1.0.0"}
	artifact := &gormdb.ClientReleaseArtifact{
		ID: 1, Release: release, Platform: "macos", Arch: "arm64", MinOSVersion: "13.0.0",
	}
	request := clientLatestRequest{Platform: "macos", Arch: "arm64"}
	if got := chooseClientArtifact([]*gormdb.ClientReleaseArtifact{artifact}, request); got != nil {
		t.Fatalf("artifact requiring min OS version was returned without an os_version: %#v", got)
	}
	request.OSVersion = "13.0.0"
	if got := chooseClientArtifact([]*gormdb.ClientReleaseArtifact{artifact}, request); got == nil {
		t.Fatal("compatible min OS artifact was not returned")
	}
}

func TestChooseClientArtifactRejectsArm64ForArmv7(t *testing.T) {
	release := &gormdb.ClientRelease{ID: 1, Version: "1.0.0"}
	artifacts := []*gormdb.ClientReleaseArtifact{{ID: 1, Release: release, Platform: "android", Arch: "arm64"}}
	if got := chooseClientArtifact(artifacts, clientLatestRequest{Platform: "android", Arch: "armv7"}); got != nil {
		t.Fatalf("arm64 artifact must not be returned for armv7: %#v", got)
	}
}

func TestClientReleaseETagVariesByOSVersion(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	request := clientLatestRequest{
		AppID: "draarl-client", Platform: "macos", Arch: "arm64",
		Channel: "stable", Current: "1.0.0", OSVersion: "13.0.0",
	}
	first := clientReleaseETagAt(request, nil, now)
	request.OSVersion = "14.0.0"
	second := clientReleaseETagAt(request, nil, now)
	if first == second {
		t.Fatal("ETag must vary when os_version changes compatibility selection")
	}
}

func TestParseClientLatestRequestPackageType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		target       string
		wantPlatform string
		wantArch     string
		wantPackage  string
		wantErr      bool
	}{
		{name: "Android armv7 alias", target: "/api/public/client/latest?platform=ANDROID&arch=armeabi-v7a", wantPlatform: "android", wantArch: "armv7", wantPackage: "apk"},
		{name: "Android arm64 alias", target: "/api/public/client/latest?platform=android&arch=aarch64", wantPlatform: "android", wantArch: "arm64", wantPackage: "apk"},
		{name: "Android universal", target: "/api/public/client/latest?platform=android&arch=universal", wantPlatform: "android", wantArch: "universal", wantPackage: "apk"},
		{name: "Windows defaults", target: "/api/public/client/latest?platform=windows", wantPlatform: "windows", wantArch: "universal", wantPackage: "exe"},
		{name: "Windows x64 alias and msix", target: "/api/public/client/latest?platform=windows&arch=x64&package_type=msix", wantPlatform: "windows", wantArch: "x86_64", wantPackage: "msix"},
		{name: "macOS defaults", target: "/api/public/client/latest?platform=macos", wantPlatform: "macos", wantArch: "universal", wantPackage: "dmg"},
		{name: "macOS aarch64 alias and pkg", target: "/api/public/client/latest?platform=macos&arch=aarch64&package_type=pkg", wantPlatform: "macos", wantArch: "arm64", wantPackage: "pkg"},
		{name: "iOS defaults", target: "/api/public/client/latest?platform=ios", wantPlatform: "ios", wantArch: "universal", wantPackage: "app_store"},
		{name: "iOS IPA", target: "/api/public/client/latest?platform=ios&arch=arm64&package_type=ipa", wantPlatform: "ios", wantArch: "arm64", wantPackage: "ipa"},
		{name: "invalid cross platform type", target: "/api/public/client/latest?platform=android&arch=arm64&package_type=exe", wantErr: true},
		{name: "android API on Windows", target: "/api/public/client/latest?platform=windows&arch=x86_64&android_api=26", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, tc.target, nil)
			got, err := parseClientLatestRequest(context)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("request parsed as %#v, want error", got)
				}
				return
			}
			if err != nil || got.Platform != tc.wantPlatform || got.Arch != tc.wantArch || got.PackageType != tc.wantPackage {
				t.Fatalf("target=(%q,%q,%q) err=%v, want=(%q,%q,%q)", got.Platform, got.Arch, got.PackageType, err, tc.wantPlatform, tc.wantArch, tc.wantPackage)
			}
		})
	}
}

func TestClientReleaseETagVariesByPackageType(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	request := clientLatestRequest{AppID: "app", Platform: "windows", Arch: "x86_64", PackageType: "exe", Channel: "stable"}
	first := clientReleaseETagAt(request, nil, now)
	request.PackageType = "msix"
	second := clientReleaseETagAt(request, nil, now)
	if first == second {
		t.Fatal("ETag must vary by requested package_type")
	}
}

func TestClientReleaseETagRotatesForPresignedDownload(t *testing.T) {
	release := &gormdb.ClientRelease{ID: 7, Version: "2.0.0"}
	artifact := &gormdb.ClientReleaseArtifact{
		ID: 11, Release: release, StorageKey: "client-releases/app/stable/2.0.0/android/arm64/app.apk", SHA256: "digest",
	}
	request := clientLatestRequest{AppID: "app", Platform: "android", Arch: "arm64", Channel: "stable"}
	now := time.Unix(1_800_000_000, 0)
	first := clientReleaseETagAt(request, artifact, now)
	second := clientReleaseETagAt(request, artifact, now.Add(clientReleaseDownloadURLExpiry))
	if first == second {
		t.Fatal("ETag must rotate so an expired presigned download can be refreshed")
	}

	artifact.ExternalURL = "https://example.com/app.apk"
	first = clientReleaseETagAt(request, artifact, now)
	second = clientReleaseETagAt(request, artifact, now.Add(clientReleaseDownloadURLExpiry))
	if first != second {
		t.Fatal("external URLs do not need time-based ETag rotation")
	}
}

func TestGetLatestClientReleaseReturnsNormalizedTargetAndIntegrityData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	publishedAt := now.Add(-time.Hour)
	release := &gormdb.ClientRelease{
		ID: 23, AppID: "draarl-client", Version: "2.0.0", Channel: "beta",
		Title: "Beta 2", Changelog: "HTTP response matrix", ForceUpdate: true,
		MinSupportedVersion: "1.5.0", PublishedAt: &publishedAt,
	}
	artifact := &gormdb.ClientReleaseArtifact{
		ID: 41, ReleaseID: release.ID, Release: release,
		Platform: "android", Arch: "armv7", AndroidABI: "armeabi-v7a", PackageType: "apk",
		BuildNumber: "200", MinOSVersion: "12.0.0", MinAndroidAPI: 26,
		FileName: "draarl-2.0.0-armv7.apk", FileSize: 123456,
		SHA256: strings.Repeat("a", 64), Signature: "signed", SignatureAlgorithm: "ed25519",
		StorageKey: "client-releases/draarl-client/beta/2.0.0/android/armv7/draarl.apk",
	}
	repository := &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{artifact}}
	presignCalls := 0
	recorder := performClientLatestRequest(
		t,
		"https://updates.example.test/api/public/client/latest?platform=ANDROID&arch=arm32&current_version=1.0.0&channel=BETA&android_api=26&os_version=13.0.0",
		repository,
		func() bool { return true },
		func(_ context.Context, key string, expiry time.Duration) (string, error) {
			presignCalls++
			if key != artifact.StorageKey || expiry != clientReleaseDownloadURLExpiry {
				t.Fatalf("presign key=%q expiry=%s", key, expiry)
			}
			return "/api/storage/get?token=test", nil
		},
		now,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls=%d, want 1", repository.calls)
	}
	query := repository.query
	if query.AppID != "draarl-client" || query.Channel != "beta" || query.Platform != "android" || query.PackageType != "apk" {
		t.Fatalf("normalized lookup=%#v", query)
	}
	if strings.Join(query.Architectures, ",") != "armv7,universal" {
		t.Fatalf("architectures=%v, want armv7 plus universal fallback", query.Architectures)
	}
	if presignCalls != 1 {
		t.Fatalf("presign calls=%d, want 1", presignCalls)
	}
	if recorder.Header().Get("ETag") == "" || recorder.Header().Get("Cache-Control") != "private, max-age=60" {
		t.Fatalf("cache headers=%v", recorder.Header())
	}
	envelope := decodeClientLatestEnvelope(t, recorder)
	if envelope.Code != http.StatusOK || envelope.Message != "成功" {
		t.Fatalf("envelope=%#v", envelope)
	}
	var response clientLatestResponse
	if err := json.Unmarshal(envelope.Data, &response); err != nil {
		t.Fatal(err)
	}
	if !response.HasUpdate || response.Release.Version != "2.0.0" || !response.Release.ForceUpdate || response.Release.MinSupportedVersion != "1.5.0" {
		t.Fatalf("release response=%#v", response.Release)
	}
	got := response.Artifact
	if got.Platform != "android" || got.Arch != "armv7" || got.AndroidABI != "armeabi-v7a" || got.PackageType != "apk" {
		t.Fatalf("artifact target=%#v", got)
	}
	if got.BuildNumber != "200" || got.MinAndroidAPI != 26 || got.MinOSVersion != "12.0.0" || got.FileSize != artifact.FileSize || got.SHA256 != artifact.SHA256 {
		t.Fatalf("artifact metadata=%#v", got)
	}
	if got.Signature != "signed" || got.SignatureAlgorithm != "ed25519" {
		t.Fatalf("artifact signature metadata=%#v", got)
	}
	if got.DownloadURL != "https://updates.example.test/api/storage/get?token=test" {
		t.Fatalf("download_url=%q", got.DownloadURL)
	}
	if got.URLExpiresAt == nil || !got.URLExpiresAt.Equal(now.Add(clientReleaseDownloadURLExpiry)) {
		t.Fatalf("url_expires_at=%v", got.URLExpiresAt)
	}
}

func TestGetLatestClientReleaseNoUpdateDoesNotGenerateDownloadURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	release := &gormdb.ClientRelease{ID: 1, AppID: "draarl-client", Version: "2.0.0", Channel: "stable"}
	artifact := &gormdb.ClientReleaseArtifact{
		ID: 2, ReleaseID: release.ID, Release: release, Platform: "windows", Arch: "universal",
		PackageType: "exe", StorageKey: "client-releases/app.exe", SHA256: strings.Repeat("b", 64),
	}
	for _, currentVersion := range []string{"2.0.0", "2.1.0"} {
		t.Run(currentVersion, func(t *testing.T) {
			repository := &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{artifact}}
			recorder := performClientLatestRequest(
				t,
				"/api/public/client/latest?platform=windows&current_version="+currentVersion,
				repository,
				func() bool { t.Fatal("storage availability must not be checked when there is no update"); return false },
				func(context.Context, string, time.Duration) (string, error) {
					t.Fatal("download URL must not be generated when there is no update")
					return "", nil
				},
				now,
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			envelope := decodeClientLatestEnvelope(t, recorder)
			if envelope.Message != "当前已是最新版本" || string(envelope.Data) != "null" {
				t.Fatalf("response=%s", recorder.Body.String())
			}
			if repository.query.Architectures == nil || strings.Join(repository.query.Architectures, ",") != "universal" || repository.query.PackageType != "exe" {
				t.Fatalf("default Windows lookup=%#v", repository.query)
			}
		})
	}
}

func TestGetLatestClientReleaseDoesNotReturnNotModifiedForMissingArtifact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_800_000_000, 0)
	repository := &clientReleaseArtifactListerStub{}
	requestTarget := "/api/public/client/latest?platform=android&arch=arm64"

	first := performClientLatestRequest(t, requestTarget, repository, func() bool { return true }, nil, now)
	if first.Code != http.StatusNotFound {
		t.Fatalf("initial status=%d body=%s", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing-artifact response omitted ETag")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, requestTarget, nil)
	c.Request.Header.Set("If-None-Match", etag)
	getLatestClientRelease(c, repository, func() bool { return true }, nil, func() time.Time { return now })
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("conditional missing-artifact status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetLatestClientReleaseReturnsExternalStoreURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	release := &gormdb.ClientRelease{ID: 3, AppID: "draarl-client", Version: "1.3.0", Channel: "stable"}
	artifact := &gormdb.ClientReleaseArtifact{
		ID: 4, ReleaseID: release.ID, Release: release, Platform: "ios", Arch: "universal",
		PackageType: "app_store", ExternalURL: "https://apps.apple.com/app/id123456789",
	}
	repository := &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{artifact}}
	recorder := performClientLatestRequest(
		t,
		"/api/public/client/latest?platform=ios",
		repository,
		func() bool { t.Fatal("external artifacts must not inspect storage"); return false },
		func(context.Context, string, time.Duration) (string, error) {
			t.Fatal("external artifacts must not be presigned")
			return "", nil
		},
		now,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	envelope := decodeClientLatestEnvelope(t, recorder)
	var response clientLatestResponse
	if err := json.Unmarshal(envelope.Data, &response); err != nil {
		t.Fatal(err)
	}
	if response.Artifact.ExternalURL != artifact.ExternalURL || response.Artifact.DownloadURL != artifact.ExternalURL || response.Artifact.URLExpiresAt != nil {
		t.Fatalf("external artifact response=%#v", response.Artifact)
	}
	if repository.query.PackageType != "app_store" || strings.Join(repository.query.Architectures, ",") != "universal" {
		t.Fatalf("default iOS lookup=%#v", repository.query)
	}
}

func TestGetLatestClientReleaseRejectsInvalidParametersBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		target string
	}{
		{name: "missing platform", target: "/api/public/client/latest"},
		{name: "missing Android arch", target: "/api/public/client/latest?platform=android"},
		{name: "unsupported platform", target: "/api/public/client/latest?platform=linux&arch=x86_64"},
		{name: "Android cross ABI", target: "/api/public/client/latest?platform=android&arch=x86_64"},
		{name: "cross-platform package", target: "/api/public/client/latest?platform=android&arch=arm64&package_type=exe"},
		{name: "invalid channel", target: "/api/public/client/latest?platform=windows&channel=nightly"},
		{name: "invalid current version", target: "/api/public/client/latest?platform=windows&current_version=v1.0.0"},
		{name: "invalid OS version", target: "/api/public/client/latest?platform=macos&os_version=14"},
		{name: "negative Android API", target: "/api/public/client/latest?platform=android&arch=arm64&android_api=-1"},
		{name: "Android API on Windows", target: "/api/public/client/latest?platform=windows&android_api=26"},
		{name: "invalid app ID", target: "/api/public/client/latest?app_id=bad%20id&platform=windows"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &clientReleaseArtifactListerStub{}
			recorder := performClientLatestRequest(
				t, test.target, repository, func() bool { return true },
				func(context.Context, string, time.Duration) (string, error) { return "", nil }, time.Now(),
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if repository.calls != 0 {
				t.Fatalf("invalid request reached repository: %#v", repository.query)
			}
			envelope := decodeClientLatestEnvelope(t, recorder)
			if envelope.Code != http.StatusBadRequest || envelope.Message == "" {
				t.Fatalf("error envelope=%#v", envelope)
			}
		})
	}
}

func TestGetLatestClientReleaseDistinguishesLookupCompatibilityAndStorageErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	release := &gormdb.ClientRelease{ID: 1, AppID: "draarl-client", Version: "1.0.0", Channel: "stable"}
	compatibleTarget := &gormdb.ClientReleaseArtifact{
		ID: 2, ReleaseID: 1, Release: release, Platform: "android", Arch: "arm64", PackageType: "apk",
		StorageKey: "client-releases/app.apk", MinAndroidAPI: 30,
	}
	tests := []struct {
		name           string
		repository     *clientReleaseArtifactListerStub
		storageEnabled bool
		presignError   error
		wantStatus     int
		wantMessage    string
	}{
		{name: "no target", repository: &clientReleaseArtifactListerStub{}, storageEnabled: true, wantStatus: http.StatusNotFound, wantMessage: "暂无匹配的平台、架构或安装格式"},
		{name: "incompatible system", repository: &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{compatibleTarget}}, storageEnabled: true, wantStatus: http.StatusNotFound, wantMessage: "暂无兼容当前系统版本的安装包"},
		{name: "repository failure", repository: &clientReleaseArtifactListerStub{err: errors.New("database unavailable")}, storageEnabled: true, wantStatus: http.StatusInternalServerError, wantMessage: "查询客户端更新失败"},
		{name: "storage disabled", repository: &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{{ID: 3, ReleaseID: 1, Release: release, Platform: "android", Arch: "arm64", PackageType: "apk", StorageKey: "client-releases/app.apk"}}}, wantStatus: http.StatusServiceUnavailable, wantMessage: "存储服务不可用"},
		{name: "presign failure", repository: &clientReleaseArtifactListerStub{artifacts: []*gormdb.ClientReleaseArtifact{{ID: 4, ReleaseID: 1, Release: release, Platform: "android", Arch: "arm64", PackageType: "apk", StorageKey: "client-releases/app.apk"}}}, storageEnabled: true, presignError: errors.New("signer unavailable"), wantStatus: http.StatusServiceUnavailable, wantMessage: "生成下载链接失败"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performClientLatestRequest(
				t,
				"/api/public/client/latest?platform=android&arch=arm64&android_api=26",
				test.repository,
				func() bool { return test.storageEnabled },
				func(context.Context, string, time.Duration) (string, error) { return "", test.presignError },
				now,
			)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			envelope := decodeClientLatestEnvelope(t, recorder)
			if envelope.Code != test.wantStatus || envelope.Message != test.wantMessage {
				t.Fatalf("error envelope=%#v", envelope)
			}
		})
	}
}
