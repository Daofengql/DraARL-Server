package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"

	"github.com/minio/minio-go/v7"
)

func TestResolveDriver(t *testing.T) {
	t.Run("explicit local", func(t *testing.T) {
		cfg := &config.Configuration{}
		cfg.Storage.Driver = "local"
		cfg.Storage.MinIO.Endpoint = "localhost:9000"
		if got := ResolveDriver(cfg); got != DriverLocal {
			t.Fatalf("got %s want local", got)
		}
	})
	t.Run("explicit minio", func(t *testing.T) {
		cfg := &config.Configuration{}
		cfg.Storage.Driver = "minio"
		if got := ResolveDriver(cfg); got != DriverMinIO {
			t.Fatalf("got %s want minio", got)
		}
	})
	t.Run("auto from endpoint", func(t *testing.T) {
		cfg := &config.Configuration{}
		cfg.Storage.MinIO.Endpoint = "localhost:9000"
		if got := ResolveDriver(cfg); got != DriverMinIO {
			t.Fatalf("got %s want minio", got)
		}
	})
	t.Run("auto fallback local", func(t *testing.T) {
		cfg := &config.Configuration{}
		if got := ResolveDriver(cfg); got != DriverLocal {
			t.Fatalf("got %s want local", got)
		}
	})
	t.Run("active profile wins", func(t *testing.T) {
		cfg := &config.Configuration{}
		cfg.Storage.Driver = "local"
		cfg.Storage.ActiveProfile = "r2-prod"
		cfg.Storage.Profiles = map[string]config.StorageProfile{
			"r2-prod": {Driver: DriverS3, S3: config.S3Config{Provider: "r2"}},
		}
		if got := ResolveDriver(cfg); got != DriverS3 {
			t.Fatalf("got %s want s3", got)
		}
	})
}

func TestNewDriverFromLocalProfile(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	cfg.Storage.Profiles = map[string]config.StorageProfile{
		"archive": {Driver: DriverLocal, Local: config.LocalStorageConfig{RootPath: t.TempDir(), BaseURL: "/archive"}},
	}
	store, err := NewDriver(cfg, "archive")
	if err != nil {
		t.Fatal(err)
	}
	if store.Driver() != DriverLocal || store.PublicURL("uploads/avatar/items/a.bin") != "/archive/uploads/avatar/items/a.bin" {
		t.Fatalf("unexpected profile storage: driver=%s url=%s", store.Driver(), store.PublicURL("uploads/avatar/items/a.bin"))
	}
}

func TestNormalizeS3Endpoint(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"https://example.r2.cloudflarestorage.com/", "example.r2.cloudflarestorage.com"},
		{"http://minio:9000", "minio:9000"},
		{"oss.example.com", "oss.example.com"},
	} {
		if got := normalizeS3Endpoint(tc.input); got != tc.want {
			t.Fatalf("endpoint %q => %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveS3EndpointSchemeOverridesBoolean(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		configured bool
		wantHost   string
		wantSSL    bool
	}{
		{name: "HTTPS enables TLS", value: "https://storage.example.com/", wantHost: "storage.example.com", wantSSL: true},
		{name: "HTTP disables TLS", value: "http://minio:9000", configured: true, wantHost: "minio:9000", wantSSL: false},
		{name: "host uses configured TLS", value: "storage.example.com", configured: true, wantHost: "storage.example.com", wantSSL: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, useSSL := resolveS3Endpoint(test.value, test.configured)
			if host != test.wantHost || useSSL != test.wantSSL {
				t.Fatalf("resolved=(%q,%t), want=(%q,%t)", host, useSSL, test.wantHost, test.wantSSL)
			}
		})
	}
}

func TestResolveCredentialReference(t *testing.T) {
	t.Setenv("DRAARL_TEST_S3_SECRET", "resolved-secret")
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "literal", value: "literal-secret", want: "literal-secret"},
		{name: "environment", value: "${DRAARL_TEST_S3_SECRET}", want: "resolved-secret"},
		{name: "missing", value: "${DRAARL_TEST_S3_MISSING}", wantErr: true},
		{name: "invalid name", value: "${1INVALID}", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveCredentialReference(test.value, "SecretKey")
			if test.wantErr {
				if err == nil {
					t.Fatalf("resolved %q, want error", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("resolved=%q err=%v, want %q", got, err, test.want)
			}
		})
	}
}

func TestResolveS3BucketLookupDefaults(t *testing.T) {
	tests := []struct {
		name     string
		config   config.S3Config
		driver   string
		expected minio.BucketLookupType
	}{
		{name: "explicit wins", config: config.S3Config{Provider: "cos", BucketLookup: "path"}, driver: "cos", expected: minio.BucketLookupPath},
		{name: "r2 path", config: config.S3Config{Provider: "r2"}, driver: DriverS3, expected: minio.BucketLookupPath},
		{name: "cos dns", config: config.S3Config{Provider: "cos"}, driver: DriverS3, expected: minio.BucketLookupDNS},
		{name: "oss alias dns", config: config.S3Config{}, driver: "oss", expected: minio.BucketLookupDNS},
		{name: "generic auto", config: config.S3Config{}, driver: DriverS3, expected: minio.BucketLookupAuto},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveS3BucketLookup(test.config, test.driver)
			if err != nil || got != test.expected {
				t.Fatalf("lookup=%v err=%v want=%v", got, err, test.expected)
			}
		})
	}
	if _, err := resolveS3BucketLookup(config.S3Config{BucketLookup: "dnz"}, DriverS3); err == nil {
		t.Fatal("invalid bucket lookup must not silently fall back to auto")
	}
}

func TestPrepareS3ConfigProviderRequirements(t *testing.T) {
	base := config.S3Config{
		Endpoint: "https://storage.example.com", AccessKey: "test-access", SecretKey: "test-secret",
	}
	t.Run("R2 defaults", func(t *testing.T) {
		sc := base
		sc.Provider = "r2"
		prepared, lookup, err := prepareS3Config(sc, DriverS3)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Endpoint != "storage.example.com" || !prepared.UseSSL || prepared.PresignEndpoint != prepared.Endpoint || !prepared.PresignUseSSL {
			t.Fatalf("prepared R2 endpoints=%#v", prepared)
		}
		if prepared.Region != "auto" || lookup != minio.BucketLookupPath {
			t.Fatalf("R2 region=%q lookup=%v", prepared.Region, lookup)
		}
	})
	for _, provider := range []string{"cos", "oss"} {
		t.Run(provider+" requires region", func(t *testing.T) {
			sc := base
			sc.Provider = provider
			if _, _, err := prepareS3Config(sc, DriverS3); err == nil {
				t.Fatalf("provider %s accepted an empty region", provider)
			}
		})
	}
	t.Run("credentials are required", func(t *testing.T) {
		sc := base
		sc.AccessKey = ""
		if _, _, err := prepareS3Config(sc, DriverS3); err == nil {
			t.Fatal("empty access key was accepted")
		}
		sc = base
		sc.SecretKey = ""
		if _, _, err := prepareS3Config(sc, DriverS3); err == nil {
			t.Fatal("empty secret key was accepted")
		}
	})
}

func TestS3ProviderPresignedURLShapes(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		endpoint     string
		region       string
		bucket       string
		sessionToken string
		wantHost     string
		wantPath     string
	}{
		{
			name: "Cloudflare R2", provider: "r2", endpoint: "https://012345.r2.cloudflarestorage.com",
			bucket: "draarl-contract", wantHost: "012345.r2.cloudflarestorage.com",
			wantPath: "/draarl-contract/client-releases/app.apk",
		},
		{
			name: "Tencent COS", provider: "cos", endpoint: "https://cos.ap-guangzhou.myqcloud.com",
			region: "ap-guangzhou", bucket: "draarl-1250000000", sessionToken: "temporary-token",
			wantHost: "draarl-1250000000.cos.ap-guangzhou.myqcloud.com", wantPath: "/client-releases/app.apk",
		},
		{
			name: "Alibaba OSS", provider: "oss", endpoint: "https://oss-cn-hangzhou.aliyuncs.com",
			region: "cn-hangzhou", bucket: "draarl-contract",
			wantHost: "draarl-contract.oss-cn-hangzhou.aliyuncs.com", wantPath: "/client-releases/app.apk",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, lookup, err := prepareS3Config(config.S3Config{
				Provider: test.provider, Endpoint: test.endpoint, Region: test.region,
				AccessKey: "test-access", SecretKey: "test-secret", SessionToken: test.sessionToken,
			}, test.provider)
			if err != nil {
				t.Fatal(err)
			}
			client, err := newS3APIClient(prepared, prepared.PresignEndpoint, prepared.PresignUseSSL, lookup, nil)
			if err != nil {
				t.Fatal(err)
			}
			store := &minioStorage{publicClient: client, bucket: test.bucket}
			put, err := store.PresignPut(context.Background(), "client-releases/app.apk", 5*time.Minute, "application/octet-stream", 123)
			if err != nil {
				t.Fatal(err)
			}
			if put.Method != "PUT" || put.Headers["Content-Type"] != "application/octet-stream" {
				t.Fatalf("presigned PUT metadata=%#v", put)
			}
			assertS3PresignedURLShape(t, put.UploadURL, test.wantHost, test.wantPath, prepared.Region, test.sessionToken)
			get, err := store.PresignGet(context.Background(), "client-releases/app.apk", 5*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			assertS3PresignedURLShape(t, get, test.wantHost, test.wantPath, prepared.Region, test.sessionToken)
		})
	}
}

func TestS3DownloadURLPrefixOnlyRewritesGet(t *testing.T) {
	prepared, lookup, err := prepareS3Config(config.S3Config{
		Provider: "minio", Endpoint: "https://storage.example.com",
		Region: "us-east-1", AccessKey: "test-access", SecretKey: "test-secret",
	}, DriverMinIO)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newS3APIClient(prepared, prepared.PresignEndpoint, prepared.PresignUseSSL, lookup, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &minioStorage{
		publicClient:      client,
		bucket:            "draarl",
		downloadURLPrefix: "https://downloads.example.com/files",
	}

	put, err := store.PresignPut(context.Background(), "client/app.apk", 5*time.Minute, "application/octet-stream", 123)
	if err != nil {
		t.Fatal(err)
	}
	putURL, err := url.Parse(put.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if putURL.Host != "storage.example.com" || putURL.Path != "/draarl/client/app.apk" {
		t.Fatalf("PUT URL was rewritten: %s", put.UploadURL)
	}

	get, err := store.PresignGet(context.Background(), "client/app.apk", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	getURL, err := url.Parse(get)
	if err != nil {
		t.Fatal(err)
	}
	if getURL.Host != "downloads.example.com" || getURL.Path != "/files/client/app.apk" || getURL.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("GET URL prefix/signature mismatch: %s", get)
	}
}

func assertS3PresignedURLShape(t *testing.T, rawURL, wantHost, wantPath, region, sessionToken string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != wantHost || parsed.Path != wantPath {
		t.Fatalf("presigned URL target=%s://%s%s, want=https://%s%s", parsed.Scheme, parsed.Host, parsed.Path, wantHost, wantPath)
	}
	query := parsed.Query()
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" || query.Get("X-Amz-Expires") != "300" || query.Get("X-Amz-Signature") == "" {
		t.Fatalf("presigned URL omitted V4 fields: %s", rawURL)
	}
	if !strings.Contains(query.Get("X-Amz-Credential"), "/"+region+"/s3/aws4_request") {
		t.Fatalf("credential scope=%q, want region %q", query.Get("X-Amz-Credential"), region)
	}
	if !headerContainsToken(strings.ReplaceAll(query.Get("X-Amz-SignedHeaders"), ";", ","), "host") {
		t.Fatalf("signed headers=%q, want host", query.Get("X-Amz-SignedHeaders"))
	}
	if got := query.Get("X-Amz-Security-Token"); got != sessionToken {
		t.Fatalf("session token=%q, want %q", got, sessionToken)
	}
}

func TestRewritePresignedDownloadURL(t *testing.T) {
	rawURL := "https://origin.example/draarl/client%20releases/app.apk?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=signed-value"
	tests := []struct {
		name      string
		prefix    string
		objectKey string
		want      string
	}{
		{
			name:      "absolute prefix with bucket path",
			prefix:    "https://downloads.example.com/draarl",
			objectKey: "client releases/app.apk",
			want:      "https://downloads.example.com/draarl/client%20releases/app.apk?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=signed-value",
		},
		{
			name:      "same-site proxy path",
			prefix:    "/object-downloads",
			objectKey: "client releases/app.apk",
			want:      "/object-downloads/client%20releases/app.apk?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=signed-value",
		},
		{
			name:      "same-site root",
			prefix:    "/",
			objectKey: "client releases/app.apk",
			want:      "/client%20releases/app.apk?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=signed-value",
		},
		{
			name:      "reserved object characters",
			prefix:    "https://downloads.example.com/draarl",
			objectKey: "桌面 #1?/client package.apk",
			want:      "https://downloads.example.com/draarl/%E6%A1%8C%E9%9D%A2%20%231%3F/client%20package.apk?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=signed-value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := rewritePresignedDownloadURL(rawURL, test.prefix, test.objectKey)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("url=%q want=%q", got, test.want)
			}
		})
	}
}

func TestRewritePresignedDownloadURLWithoutPrefixIsUnchanged(t *testing.T) {
	rawURL := "https://origin.example/draarl/app.apk?X-Amz-Signature=original"
	got, err := rewritePresignedDownloadURL(rawURL, "", "app.apk")
	if err != nil || got != rawURL {
		t.Fatalf("url=%q err=%v want=%q", got, err, rawURL)
	}
}

func TestValidateDownloadURLPrefix(t *testing.T) {
	for _, valid := range []string{"", "/objects", "https://downloads.example.com/draarl", "http://localhost:9000/files"} {
		if err := validateDownloadURLPrefix(valid); err != nil {
			t.Fatalf("valid prefix %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"objects", "ftp://example.com/files", "https://user@example.com/files", "https://example.com/files?token=x", "https://example.com/files#part"} {
		if err := validateDownloadURLPrefix(invalid); err == nil {
			t.Fatalf("invalid prefix %q accepted", invalid)
		}
	}
}

func TestS3NeverAdvertisesUnsignedPublicURL(t *testing.T) {
	store := &minioStorage{downloadURLPrefix: "https://downloads.example.com/draarl"}
	if store.Capabilities().PublicURL {
		t.Fatal("S3 storage must never advertise unsigned public reads")
	}
	if got := store.PublicURL("client/app.apk"); got != "" {
		t.Fatalf("S3 storage exposed unsigned URL %q", got)
	}
}

type readURLTestStorage struct {
	Storage
	capabilities Capabilities
	publicURL    string
	presignedURL string
	publicCalls  int
	presignCalls int
}

func (s *readURLTestStorage) Capabilities() Capabilities {
	return s.capabilities
}

func (s *readURLTestStorage) PublicURL(string) string {
	s.publicCalls++
	return s.publicURL
}

func (s *readURLTestStorage) PresignGet(context.Context, string, time.Duration) (string, error) {
	s.presignCalls++
	return s.presignedURL, nil
}

func TestReadURLUsesPublicCapabilityOrSignedFallback(t *testing.T) {
	previousStore := Get()
	t.Cleanup(func() {
		mu.Lock()
		current = previousStore
		mu.Unlock()
	})

	publicStore := &readURLTestStorage{
		capabilities: Capabilities{PublicURL: true, PresignGet: true},
		publicURL:    "https://cdn.example.com/assets/a.bin",
		presignedURL: "https://signed.example.com/assets/a.bin",
	}
	mu.Lock()
	current = publicStore
	mu.Unlock()
	got, err := ReadURL(context.Background(), "assets/a.bin", time.Minute)
	if err != nil || got != publicStore.publicURL || publicStore.publicCalls != 1 || publicStore.presignCalls != 0 {
		t.Fatalf("public read URL=%q err=%v calls=%d/%d", got, err, publicStore.publicCalls, publicStore.presignCalls)
	}

	privateStore := &readURLTestStorage{
		capabilities: Capabilities{PresignGet: true},
		publicURL:    "https://unsigned.example.com/assets/a.bin",
		presignedURL: "https://signed.example.com/assets/a.bin",
	}
	mu.Lock()
	current = privateStore
	mu.Unlock()
	got, err = ReadURL(context.Background(), "assets/a.bin", time.Minute)
	if err != nil || got != privateStore.presignedURL || privateStore.publicCalls != 0 || privateStore.presignCalls != 1 {
		t.Fatalf("private read URL=%q err=%v calls=%d/%d", got, err, privateStore.publicCalls, privateStore.presignCalls)
	}
	if got := PublicURL("assets/a.bin"); got != "" {
		t.Fatalf("private storage exposed unsigned public URL %q", got)
	}
}

func TestResolveMinIOPublicTarget(t *testing.T) {
	cfg := config.MinIOConfig{Endpoint: "minio:9000"}
	if endpoint, secure := resolveMinIOPublicTarget(cfg); endpoint != "minio:9000" || secure {
		t.Fatalf("fallback target=%q secure=%t", endpoint, secure)
	}
	cfg.PublicEndpoint = "storage.example.com"
	cfg.PublicUseSSL = true
	if endpoint, secure := resolveMinIOPublicTarget(cfg); endpoint != "storage.example.com" || !secure {
		t.Fatalf("public target=%q secure=%t", endpoint, secure)
	}
}

func TestShouldPresignUpload(t *testing.T) {
	for _, ft := range []string{"assets", "client_package", "firmware", "operator_cert"} {
		if !ShouldPresignUpload(ft) {
			t.Fatalf("should presign %s", ft)
		}
		if !IsAllowedPresignFileType(ft) {
			t.Fatalf("allowed type %s", ft)
		}
	}
	if ShouldPresignUpload("avatar") || ShouldPresignUpload("logo") || ShouldPresignUpload("favicon") {
		t.Fatal("image-processed types must not presign")
	}
}

func TestDetectContentTypeAndAllowedList(t *testing.T) {
	contentType, err := detectContentType(bytes.NewReader([]byte("%PDF-1.7\n")))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"application/pdf": true}
	if !IsAllowedContentType(contentType, allowed) {
		t.Fatalf("expected %s to be allowed", contentType)
	}
	if IsAllowedContentType("text/plain; charset=utf-8", allowed) {
		t.Fatal("text/plain should not be allowed")
	}
}

func TestKnownDrivers(t *testing.T) {
	drivers := KnownDrivers()
	found := map[string]bool{}
	for _, d := range drivers {
		found[d] = true
	}
	for _, driver := range []string{DriverLocal, DriverMinIO, DriverS3, "r2", "cos", "oss"} {
		if !found[driver] {
			t.Fatalf("expected %s to be registered, got %v", driver, drivers)
		}
	}
}

func TestClientPackageUploadLimitCanBeConfigured(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Storage.UploadLimits.ClientPackageBytes = 64 * 1024 * 1024
	previous := config.Config
	config.Config = cfg
	t.Cleanup(func() { config.Config = previous })

	if got := MaxSizeForFileType("client_package"); got != 64*1024*1024 {
		t.Fatalf("client package limit = %d, want %d", got, int64(64*1024*1024))
	}
}

func TestNewObjectKey(t *testing.T) {
	key := NewObjectKey("assets", ".zip")
	if !strings.HasPrefix(key, "uploads/assets/") {
		t.Fatalf("unexpected key: %s", key)
	}
	if !strings.HasSuffix(key, ".zip") {
		t.Fatalf("missing ext: %s", key)
	}
}

func TestLocalStoragePathTraversal(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"

	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ls := store.(*localStorage)

	badKeys := []string{
		"../etc/passwd",
		"a/../../etc/passwd",
		`..\windows\system32`,
		"uploads/../../../etc/passwd",
		"uploads/foo/../../..",
		string([]byte{'a', 0, 'b'}),
	}
	for _, k := range badKeys {
		if _, err := ls.resolvePath(k); err == nil {
			t.Fatalf("expected traversal rejection for %q", k)
		}
	}
	path, err := ls.resolvePath("uploads/assets/ok.bin")
	if err != nil {
		t.Fatal(err)
	}
	absRoot, _ := filepath.Abs(root)
	if path != filepath.Join(absRoot, filepath.FromSlash("uploads/assets/ok.bin")) && !strings.HasPrefix(path, absRoot+string(filepath.Separator)) {
		t.Fatalf("path outside root: %s root=%s", path, absRoot)
	}
}

func TestLocalStorageRejectsSymlinkedParentForMissingTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "uploads")); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.(*localStorage).resolvePath("uploads/assets/new.bin"); err == nil {
		t.Fatal("missing target beneath an out-of-root symlink must be rejected")
	}
}

func TestLocalPutAndPublicURL(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.Storage.Local.BaseURL = "/files"
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"

	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}

	key := "uploads/avatar/test.bin"
	data := []byte("hello-storage")
	if err := store.Put(context.Background(), key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, filepath.FromSlash(key))
	if _, err := os.Stat(full); err != nil {
		t.Fatal(err)
	}
	url := store.PublicURL(key)
	if url != "/files/"+key {
		t.Fatalf("public url: %s", url)
	}
	encodedKey := "uploads/avatar/user photo #1?.jpg"
	if got := store.PublicURL(encodedKey); got != "/files/uploads/avatar/user%20photo%20%231%3F.jpg" {
		t.Fatalf("encoded public url: %s", got)
	}
	if protectedURL := store.PublicURL("uploads/assets/private.bin"); protectedURL != "" {
		t.Fatalf("protected local object exposed permanent URL: %s", protectedURL)
	}
	for _, invalid := range []string{"uploads/avatar/../firmware/private.bin", "uploads/avatar//private.jpg", "uploads/avatar/./private.jpg"} {
		if IsLocalPublicObjectKey(invalid) {
			t.Fatalf("invalid public object key accepted: %q", invalid)
		}
	}
}

func TestLocalPutToken(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"

	prev := config.Config
	config.Config = cfg
	t.Cleanup(func() { config.Config = prev })

	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ls := store.(*localStorage)
	key := "uploads/assets/tok.bin"
	exp := time.Now().Add(5 * time.Minute)
	token, err := ls.signPutToken(key, exp, "application/octet-stream", 13)
	if err != nil {
		t.Fatal(err)
	}
	if size, err := VerifyLocalPutToken(token, key, "application/octet-stream"); err != nil || size != 13 {
		t.Fatalf("valid token rejected: %v", err)
	}
	if _, err := VerifyLocalPutToken(token, "other-key", "application/octet-stream"); err == nil {
		t.Fatal("wrong key should fail")
	}
	if _, err := VerifyLocalPutToken("", key, "application/octet-stream"); err == nil {
		t.Fatal("empty token should fail")
	}
	// expired
	expToken, err := ls.signPutToken(key, time.Now().Add(-time.Minute), "application/octet-stream", 13)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyLocalPutToken(expToken, key, "application/octet-stream"); err == nil {
		t.Fatal("expired token should fail")
	}
}

func TestLocalPresignGetUsesExpiringToken(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"

	prev := config.Config
	config.Config = cfg
	t.Cleanup(func() { config.Config = prev })

	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := "client-releases/draarl-client/stable/1.0.0/android/arm64/client.apk"
	if err := store.Put(context.Background(), key, bytes.NewReader([]byte("apk")), 3, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	presigned, err := store.PresignGet(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(presigned)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/storage/get" {
		t.Fatalf("presign path=%q", parsed.Path)
	}
	if err := VerifyLocalGetToken(parsed.Query().Get("token"), parsed.Query().Get("key")); err != nil {
		t.Fatalf("valid signed download rejected: %v", err)
	}
	if err := VerifyLocalGetToken(parsed.Query().Get("token"), "client-releases/other.apk"); err == nil {
		t.Fatal("download token must be bound to its object key")
	}
}

func TestLocalPromoteDoesNotOverwriteFinalObject(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	storeValue, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store := storeValue.(*localStorage)
	ctx := context.Background()
	staged := "staging/client-package/1/first.apk"
	final := "client-releases/app/stable/1.0.0/android/arm64/app.apk"
	first := []byte("first package")
	if err := store.Put(ctx, staged, bytes.NewReader(first), int64(len(first)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, staged, final); err != nil {
		t.Fatal(err)
	}
	secondStaged := "staging/client-package/1/second.apk"
	second := []byte("replacement package")
	if err := store.Put(ctx, secondStaged, bytes.NewReader(second), int64(len(second)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, secondStaged, final); !errors.Is(err, ErrFinalObjectAlreadyExists) {
		t.Fatalf("second promote error=%v, want immutable-final conflict", err)
	}
	reader, err := store.Open(ctx, final)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, first) {
		t.Fatalf("final object was overwritten: got %q want %q", got, first)
	}
}

func TestUploadGrantIsBoundToUserTypeAndSize(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	previous := config.Config
	config.Config = cfg
	t.Cleanup(func() { config.Config = previous })

	key := NewStagingObjectKey("assets", 42, ".bin")
	token, err := CreateUploadGrant(key, "assets", 42, 123, "application/octet-stream", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := VerifyUploadGrant(token, key, "assets", 42)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Size != 123 {
		t.Fatalf("grant size = %d, want 123", grant.Size)
	}
	if _, err := VerifyUploadGrant(token, key, "assets", 43); err == nil {
		t.Fatal("grant must not be valid for another user")
	}
	if _, err := VerifyUploadGrant(token, key, "firmware", 42); err == nil {
		t.Fatal("grant must not be valid for another file type")
	}
	expired, err := signToken(cfg.JWT.Secret, UploadGrant{
		ObjectKey: key, FileType: "assets", UserID: 42, Size: 123,
		ContentType: "application/octet-stream", ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyUploadGrant(expired, key, "assets", 42); err == nil {
		t.Fatal("expired upload grant should fail")
	}
}

func TestValidateStagedUploadRejectsWrongPrefixAndSize(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	storeValue, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previousConfig := config.Config
	previousStore := Get()
	config.Config = cfg
	mu.Lock()
	current = storeValue
	mu.Unlock()
	t.Cleanup(func() {
		config.Config = previousConfig
		mu.Lock()
		current = previousStore
		mu.Unlock()
	})

	wrongKey := "uploads/assets/not-staging.bin"
	wrongToken, err := CreateUploadGrant(wrongKey, "assets", 42, 3, "application/octet-stream", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateStagedUpload(context.Background(), wrongToken, wrongKey, "assets", 42); err == nil {
		t.Fatal("non-staging object key should fail validation")
	}

	stagedKey := NewStagingObjectKey("assets", 42, ".bin")
	if err := Put(context.Background(), stagedKey, bytes.NewReader([]byte("short")), 5, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	sizeToken, err := CreateUploadGrant(stagedKey, "assets", 42, 6, "application/octet-stream", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateStagedUpload(context.Background(), sizeToken, stagedKey, "assets", 42); err == nil {
		t.Fatal("uploaded size mismatch should fail validation")
	}
	if _, _, err := Stat(context.Background(), stagedKey); err == nil {
		t.Fatal("size-mismatched staging object should be removed")
	}
}

func TestLocalStagingValidationAndPromotion(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"

	storeValue, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previousConfig := config.Config
	previousStore := Get()
	config.Config = cfg
	mu.Lock()
	current = storeValue
	mu.Unlock()
	t.Cleanup(func() {
		config.Config = previousConfig
		mu.Lock()
		current = previousStore
		mu.Unlock()
	})

	data := []byte("staged payload")
	stagedKey := NewStagingObjectKey("assets", 42, ".bin")
	if err := Put(context.Background(), stagedKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	token, err := CreateUploadGrant(stagedKey, "assets", 42, int64(len(data)), "application/octet-stream", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	grant, _, err := ValidateStagedUpload(context.Background(), token, stagedKey, "assets", 42)
	if err != nil {
		t.Fatal(err)
	}
	finalKey, err := PromoteStagedUpload(context.Background(), grant)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(finalKey, "staging/") || !strings.HasPrefix(finalKey, "uploads/assets/") {
		t.Fatalf("unexpected final key: %s", finalKey)
	}
	if _, _, err := Stat(context.Background(), stagedKey); err == nil {
		t.Fatal("staging object should be removed after promotion")
	}
	if size, _, err := Stat(context.Background(), finalKey); err != nil || size != int64(len(data)) {
		t.Fatalf("final object stat = (%d, %v)", size, err)
	}
}

func TestLocalPutRejectsSizeMismatchWithoutPublishingPartialFile(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	storeValue, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := "staging/assets/42/test.bin"
	if err := storeValue.Put(context.Background(), key, bytes.NewReader([]byte("short")), 10, "application/octet-stream"); err == nil {
		t.Fatal("size mismatch should fail")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); !os.IsNotExist(err) {
		t.Fatalf("partial file should not be published, stat error: %v", err)
	}
}

func TestResolveAssetURL(t *testing.T) {
	if got := ResolveAssetURL("https://cdn.example/logo.png"); got != "https://cdn.example/logo.png" {
		t.Fatalf("absolute url rewritten: %s", got)
	}
	if got := ResolveAssetURL(""); got != "" {
		t.Fatalf("empty should stay empty: %s", got)
	}
}

func TestLocalWalk(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"uploads/assets/a.bin", "uploads/avatar/b.jpg", "thumb/uploads/avatar/b.jpg", "staging/assets/1/c.bin"}
	for _, k := range keys {
		if err := store.Put(context.Background(), k, bytes.NewReader([]byte(k)), int64(len(k)), ""); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]int64{}
	if err := store.Walk(context.Background(), "", func(o ObjectInfo) error {
		seen[o.Key] = o.Size
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := seen[k]; !ok {
			t.Fatalf("walk missed key: %s", k)
		}
	}
	// 前缀过滤
	prefixSeen := 0
	if err := store.Walk(context.Background(), "uploads/avatar/", func(o ObjectInfo) error {
		prefixSeen++
		if !strings.HasPrefix(o.Key, "uploads/avatar/") {
			t.Fatalf("prefix walk leaked key: %s", o.Key)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if prefixSeen != 1 {
		t.Fatalf("prefix walk count = %d want 1", prefixSeen)
	}
}

// newLocalAt 构造一个指向指定 root 的 local 驱动（用于模拟跨引擎迁移）。
func newLocalAt(t *testing.T, root string) Storage {
	t.Helper()
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = root
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	store, err := newLocalStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestMigrateBetweenDrivers(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	src := newLocalAt(t, srcRoot)
	dst := newLocalAt(t, dstRoot)

	objects := map[string]string{
		"uploads/assets/pkg.bin":         "asset-data",
		"uploads/avatar/2026/07/a.jpg":   "avatar",
		"thumb/uploads/avatar/a.jpg":     "thumb",
		"uploads/firmware/2026/07/f.bin": "firmware",
		"staging/assets/1/tmp.bin":       "should-be-skipped",
		"frontend/v1/index.js":           "cdn-skip",
	}
	for k, v := range objects {
		if err := src.Put(context.Background(), k, bytes.NewReader([]byte(v)), int64(len(v)), ""); err != nil {
			t.Fatal(err)
		}
	}

	res, err := migrateWith(context.Background(), src, dst, MigrateOptions{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// staging/ 和 frontend/ 被跳过，其余 4 个复制
	if res.Copied != 4 {
		t.Fatalf("copied = %d want 4", res.Copied)
	}
	for k, v := range objects {
		size, _, statErr := dst.Stat(context.Background(), k)
		if shouldSkipMigrateKey(k) {
			if statErr == nil {
				t.Fatalf("skipped key should not exist in dst: %s", k)
			}
			continue
		}
		if statErr != nil || size != int64(len(v)) {
			t.Fatalf("dst object %s stat = (%d, %v)", k, size, statErr)
		}
	}

	// 断点续传：再跑一次，全部命中跳过，0 复制
	res2, err := migrateWith(context.Background(), src, dst, MigrateOptions{})
	if err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if res2.Copied != 0 || res2.Skipped != 4 {
		t.Fatalf("resume: copied=%d skipped=%d want copied=0 skipped=4", res2.Copied, res2.Skipped)
	}
}

func TestMigrateRejectsSameEngine(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Storage.Local.RootPath = t.TempDir()
	cfg.JWT.Secret = "test-secret-key-for-local-storage-32b"
	for _, name := range []string{"local", "minio"} {
		if _, err := Migrate(context.Background(), cfg, name, name, MigrateOptions{}); err == nil {
			t.Fatalf("same-engine migration %s->%s should be rejected", name, name)
		}
	}
}

func TestMigrateDeleteSource(t *testing.T) {
	src := newLocalAt(t, t.TempDir())
	dst := newLocalAt(t, t.TempDir())
	key := "uploads/assets/x.bin"
	data := "payload"
	if err := src.Put(context.Background(), key, bytes.NewReader([]byte(data)), int64(len(data)), ""); err != nil {
		t.Fatal(err)
	}
	res, err := migrateWith(context.Background(), src, dst, MigrateOptions{DeleteSource: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted = %d want 1", res.Deleted)
	}
	if _, _, err := src.Stat(context.Background(), key); err == nil {
		t.Fatal("source object should be deleted")
	}
	if _, _, err := dst.Stat(context.Background(), key); err != nil {
		t.Fatalf("dst object missing: %v", err)
	}
}
