package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
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
}

func TestShouldPresignUpload(t *testing.T) {
	for _, ft := range []string{"assets", "firmware", "operator_cert"} {
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

func TestKnownDrivers(t *testing.T) {
	drivers := KnownDrivers()
	found := map[string]bool{}
	for _, d := range drivers {
		found[d] = true
	}
	if !found[DriverLocal] || !found[DriverMinIO] {
		t.Fatalf("expected local+minio registered, got %v", drivers)
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

	key := "uploads/assets/test.bin"
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
