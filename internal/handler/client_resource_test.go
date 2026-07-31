package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

func TestNormalizeClientResourceDefinition(t *testing.T) {
	key, name, category, _, err := normalizeClientResourceDefinition(" Model/Denoise ", " 降噪模型 ", "MODEL", "test")
	if err != nil || key != "model/denoise" || name != "降噪模型" || category != "model" {
		t.Fatalf("normalized=(%q,%q,%q) err=%v", key, name, category, err)
	}
	for _, invalid := range []string{"../model", "model//denoise", "model/..", "模型/denoise", "/"} {
		if _, _, _, _, err := normalizeClientResourceDefinition(invalid, "name", "", ""); err == nil {
			t.Fatalf("resource_key %q was accepted", invalid)
		}
	}
}

func TestCleanupDeletedClientResourceObjects(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "client-resource-delete-test-secret"
	cfg.Storage.ActiveProfile = "delete-local"
	cfg.Storage.Profiles = map[string]config.StorageProfile{
		"delete-local": {Driver: storage.DriverLocal, Local: config.LocalStorageConfig{RootPath: t.TempDir()}},
	}
	if err := storage.Init(cfg); err != nil {
		t.Fatal(err)
	}
	key := "client-resources/app/test/stable/1.0.0/apk/android/default/hash/app.apk"
	payload := "apk"
	if err := storage.Put(context.Background(), key, strings.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/client-resources/1", nil)
	resource := &gormdb.ClientResource{Releases: []gormdb.ClientResourceRelease{
		{Artifacts: []gormdb.ClientResourceArtifact{{StorageKey: key}, {StorageKey: key}, {ExternalURL: "https://example.invalid/app"}}},
	}}
	result := cleanupDeletedClientResourceObjects(c, resource)
	if result.DeletedReleases != 1 || result.DeletedArtifacts != 3 || result.DeletedObjects != 1 || result.ObjectCleanupFailures != 0 {
		t.Fatalf("cleanup result=%#v", result)
	}
	if _, _, err := storage.Stat(context.Background(), key); err == nil {
		t.Fatal("deleted client resource object still exists")
	}
}

func TestNormalizeClientResourceTargetsSupportsExplicitMultiSelect(t *testing.T) {
	targets, err := normalizeClientResourceTargets("onnx", []clientResourceArtifactTargetRequest{
		{Platform: "windows", Arch: "amd64"},
		{Platform: "macos", Arch: "aarch64", MinOSVersion: "13.0.0"},
		{Platform: "android", Arch: "arm64-v8a", MinAndroidAPI: 26},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 || targets[0].Arch != "x86_64" || targets[1].Arch != "arm64" || targets[2].Arch != "arm64" {
		t.Fatalf("targets=%#v", targets)
	}
	if _, err := normalizeClientResourceTargets("onnx", []clientResourceArtifactTargetRequest{
		{Platform: "windows", Arch: "x86_64"}, {Platform: "windows", Arch: "amd64"},
	}); err == nil {
		t.Fatal("duplicate normalized target was accepted")
	}
	if _, err := normalizeClientResourceTargets("onnx", []clientResourceArtifactTargetRequest{{Platform: "universal", Arch: "universal"}}); err == nil {
		t.Fatal("universal wildcard target was accepted")
	}
	if _, err := normalizeClientResourceTargets("apk", []clientResourceArtifactTargetRequest{{Platform: "windows", Arch: "x86_64"}}); err == nil {
		t.Fatal("APK on Windows was accepted")
	}
}

func TestBuildClientResourceManifestSelectsChannelVersionAndCompatibility(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	resource := &gormdb.ClientResource{ID: 1, ResourceKey: "model/denoise", Name: "降噪模型", Category: "model", Required: true, Enabled: true}
	makeArtifact := func(id, releaseID int, version, channel, minClient string, minAPI int) *gormdb.ClientResourceArtifact {
		release := &gormdb.ClientResourceRelease{ID: releaseID, ResourceID: resource.ID, Resource: resource, Version: version, Channel: channel, Status: gormdb.ClientResourceReleaseStatusPublished, MinClientVersion: minClient, PublishedAt: &now}
		return &gormdb.ClientResourceArtifact{
			ID: id, ReleaseID: releaseID, Release: release, Format: "onnx", Runtime: "cpu", Variant: "default",
			FileName: version + ".onnx", FileSize: 10, SHA256: strings.Repeat(string(rune('a'+id)), 64),
			Targets: []gormdb.ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64", MinAndroidAPI: minAPI}},
		}
	}
	artifacts := []*gormdb.ClientResourceArtifact{
		makeArtifact(1, 11, "1.0.0", "stable", "", 0),
		makeArtifact(2, 12, "2.0.0", "stable", "2.0.0", 0),
		makeArtifact(3, 13, "1.5.0", "beta", "", 0),
	}

	stableOld := buildClientResourceManifest(artifacts, clientResourceManifestRequest{Platform: "windows", Arch: "x86_64", Channel: "stable", ClientVersion: "1.0.0"})
	if len(stableOld.Resources) != 1 || stableOld.Resources[0].Release.Version != "1.0.0" {
		t.Fatalf("stable old manifest=%#v", stableOld)
	}
	stableNew := buildClientResourceManifest(artifacts, clientResourceManifestRequest{Platform: "windows", Arch: "x86_64", Channel: "stable", ClientVersion: "2.0.0"})
	if stableNew.Resources[0].Release.Version != "2.0.0" {
		t.Fatalf("stable new manifest=%#v", stableNew)
	}
	beta := buildClientResourceManifest(artifacts, clientResourceManifestRequest{Platform: "windows", Arch: "x86_64", Channel: "beta", ClientVersion: "2.0.0"})
	if beta.Resources[0].Release.Version != "1.5.0" || beta.Resources[0].Release.Channel != "beta" {
		t.Fatalf("beta manifest=%#v", beta)
	}
	withoutBeta := buildClientResourceManifest(artifacts[:2], clientResourceManifestRequest{Platform: "windows", Arch: "x86_64", Channel: "beta", ClientVersion: "2.0.0"})
	if withoutBeta.Resources[0].Release.Version != "2.0.0" || withoutBeta.Resources[0].Release.Channel != "stable" {
		t.Fatalf("beta fallback manifest=%#v", withoutBeta)
	}

	payload, err := json.Marshal(stableNew)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "download_url") || strings.Contains(string(payload), "storage_key") {
		t.Fatalf("manifest leaked transport details: %s", payload)
	}
}

func TestClientResourceObjectKeyIsContentAddressedAndTargetIndependent(t *testing.T) {
	resource := &gormdb.ClientResource{ResourceKey: "font/cjk"}
	release := &gormdb.ClientResourceRelease{Version: "1.0.0", Channel: "stable"}
	artifact := &gormdb.ClientResourceArtifact{Format: "ttf", Runtime: "default", Variant: "regular", FileName: "Noto Sans CJK.ttf"}
	digest := strings.Repeat("a", 64)
	got := clientResourceObjectKey(resource, release, artifact, digest)
	want := "client-resources/font/cjk/stable/1.0.0/ttf/default/regular/" + digest + "/Noto Sans CJK.ttf"
	if got != want {
		t.Fatalf("object key=%q want=%q", got, want)
	}
	if strings.Contains(got, "windows") || strings.Contains(got, "x86_64") {
		t.Fatalf("target leaked into object key: %s", got)
	}
}

func TestClientResourceArtifactTargetCompatibility(t *testing.T) {
	artifact := &gormdb.ClientResourceArtifact{Targets: []gormdb.ClientResourceArtifactTarget{
		{Platform: "android", Arch: "arm64", MinAndroidAPI: 26, MinOSVersion: "10.0.0"},
		{Platform: "windows", Arch: "x86_64"},
	}}
	if !clientResourceArtifactTargetCompatible(artifact, clientResourceManifestRequest{Platform: "android", Arch: "arm64", AndroidAPI: 26, OSVersion: "10.0.0"}) {
		t.Fatal("compatible Android target was rejected")
	}
	if clientResourceArtifactTargetCompatible(artifact, clientResourceManifestRequest{Platform: "android", Arch: "arm64", AndroidAPI: 25, OSVersion: "10.0.0"}) {
		t.Fatal("incompatible Android API was accepted")
	}
	if !clientResourceArtifactTargetCompatible(artifact, clientResourceManifestRequest{Platform: "windows", Arch: "x86_64"}) {
		t.Fatal("second selected platform target was rejected")
	}
}

func TestBuildClientResourceManifestKeepsMultipleRuntimeVariantCandidates(t *testing.T) {
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	resource := &gormdb.ClientResource{ID: 7, ResourceKey: "model/denoise", Name: "denoise", Category: "model", Enabled: true}
	release := &gormdb.ClientResourceRelease{ID: 20, ResourceID: resource.ID, Resource: resource, Version: "1.0.0", Channel: "stable", Status: gormdb.ClientResourceReleaseStatusPublished, PublishedAt: &now}
	artifacts := []*gormdb.ClientResourceArtifact{
		{ID: 2, ReleaseID: release.ID, Release: release, Format: "onnx", Runtime: "onnxruntime", Variant: "gpu", FileName: "gpu.onnx", SHA256: strings.Repeat("b", 64), Targets: []gormdb.ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64"}}},
		{ID: 1, ReleaseID: release.ID, Release: release, Format: "onnx", Runtime: "onnxruntime", Variant: "cpu", FileName: "cpu.onnx", SHA256: strings.Repeat("a", 64), Targets: []gormdb.ClientResourceArtifactTarget{{Platform: "windows", Arch: "x86_64"}}},
	}
	manifest := buildClientResourceManifest(artifacts, clientResourceManifestRequest{Platform: "windows", Arch: "x86_64", Channel: "stable"})
	if len(manifest.Resources) != 1 || len(manifest.Resources[0].Artifacts) != 2 {
		t.Fatalf("manifest=%#v", manifest)
	}
	if manifest.Resources[0].Artifacts[0].Variant != "cpu" || manifest.Resources[0].Artifacts[1].Variant != "gpu" {
		t.Fatalf("artifacts not sorted deterministically: %#v", manifest.Resources[0].Artifacts)
	}
}

func TestClientResourceApplicationTargetMatrix(t *testing.T) {
	tests := []struct {
		format, platform, arch string
		valid                  bool
	}{
		{format: "apk", platform: "android", arch: "armv7", valid: true},
		{format: "apk", platform: "android", arch: "arm64", valid: true},
		{format: "apk", platform: "android", arch: "x86_64", valid: true},
		{format: "apk", platform: "android", arch: "mips", valid: false},
		{format: "exe", platform: "windows", arch: "x86_64", valid: true},
		{format: "exe", platform: "linux", arch: "x86_64", valid: false},
		{format: "msix", platform: "windows", arch: "arm64", valid: true},
		{format: "dmg", platform: "macos", arch: "arm64", valid: true},
		{format: "pkg", platform: "windows", arch: "x86_64", valid: false},
		{format: "ipa", platform: "ios", arch: "arm64", valid: true},
		{format: "app_store", platform: "ios", arch: "arm64", valid: true},
	}
	for _, test := range tests {
		t.Run(test.format+"/"+test.platform+"/"+test.arch, func(t *testing.T) {
			err := validateClientResourceApplicationTarget(test.format, test.platform, test.arch)
			if (err == nil) != test.valid {
				t.Fatalf("validation error=%v, valid=%t", err, test.valid)
			}
		})
	}
}

func FuzzNormalizeClientResourceInputs(f *testing.F) {
	f.Add("model/denoise", "降噪模型", "model", "{}")
	f.Add("../escape", "", "", "not-json")
	f.Fuzz(func(t *testing.T, resourceKey, name, category, metadata string) {
		_, _, _, _, _ = normalizeClientResourceDefinition(resourceKey, name, category, metadata)
		_, _, _, _, _, _ = normalizeClientResourceArtifact(clientResourceArtifactCompleteRequest{
			Format: "onnx", Runtime: resourceKey, Variant: category,
			Metadata: json.RawMessage(metadata), Targets: []clientResourceArtifactTargetRequest{{Platform: "linux", Arch: "x86_64"}},
		})
	})
}

func FuzzBuildClientResourceManifestSelection(f *testing.F) {
	f.Add("windows", "x86_64", "stable", "1.0.0", "10.0.0")
	f.Add("android", "arm64-v8a", "beta", "", "")
	f.Fuzz(func(t *testing.T, platform, arch, channel, clientVersion, osVersion string) {
		platform = normalizeClientTargetValue(platform)
		arch = normalizeClientArch(arch)
		if platform == "" || arch == "" || platform == "universal" || arch == "universal" {
			return
		}
		request := clientResourceManifestRequest{Platform: platform, Arch: arch, Channel: channel, ClientVersion: clientVersion, OSVersion: osVersion}
		resource := &gormdb.ClientResource{ID: 1, ResourceKey: "model/fuzz", Name: "fuzz", Enabled: true}
		now := time.Now()
		release := &gormdb.ClientResourceRelease{ID: 1, ResourceID: 1, Resource: resource, Version: "1.0.0", Channel: "stable", Status: gormdb.ClientResourceReleaseStatusPublished, PublishedAt: &now}
		artifact := &gormdb.ClientResourceArtifact{ID: 1, ReleaseID: 1, Release: release, Format: "bin", Runtime: "default", Variant: "default", Targets: []gormdb.ClientResourceArtifactTarget{{Platform: platform, Arch: arch}}}
		_ = buildClientResourceManifest([]*gormdb.ClientResourceArtifact{artifact}, request)
	})
}
