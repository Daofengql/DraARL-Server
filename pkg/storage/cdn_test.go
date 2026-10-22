package storage

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
)

func TestCDNTypeAOfficialVectorAndEscapedPath(t *testing.T) {
	// https://edgeone.ai/document/62475 publishes this TypeA test vector.
	for _, tc := range []struct {
		name, base, key, want string
	}{
		{
			name: "official vector", base: "https://www.example.com", key: "foo.jpg",
			want: "https://www.example.com/foo.jpg?token=1721028437-Kv4cPTAAP5YTi-0-0fbdca749d7ab784750685347e42075c",
		},
		{
			name: "escaped prefix and object", base: "https://cdn.example.com/prefix%2Fdir/", key: "/uploads/a b?#%25.bin",
			want: "https://cdn.example.com/prefix%2Fdir/uploads/a%20b%3F%23%2525.bin?token=1721028437-Kv4cPTAAP5YTi-0-2b16ada751c5a779e8b23d9806a32121",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signer, err := newCDNSigner(config.StorageConfig{
				UseCDN: true, CDNBaseURL: tc.base, CDNSignParam: "token",
				CDNKey: "DvYmqE81E1F9R791H6lmht", CDNExpirySeconds: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(1721028437, 999999999)
			got, expiresAt, err := signer.signWithNonce(tc.key, now, "Kv4cPTAAP5YTi")
			if err != nil || got != tc.want || !expiresAt.Equal(time.Unix(1721028438, 0)) {
				t.Fatalf("signed URL=%q expiresAt=%v err=%v; want %q", got, expiresAt, err, tc.want)
			}
		})
	}
}

func TestCDNReadURLsBypassDriverSigning(t *testing.T) {
	previousStore, previousCDN := readStorage()
	t.Cleanup(func() {
		mu.Lock()
		current, currentCDN = previousStore, previousCDN
		mu.Unlock()
	})
	signer, err := newCDNSigner(config.StorageConfig{
		UseCDN: true, CDNBaseURL: "https://cdn.example.com/root/", CDNKey: "test-secret",
		CDNSignParam: "auth_key", CDNExpirySeconds: 7200,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &readURLTestStorage{
		capabilities: Capabilities{PublicURL: true, PresignGet: true},
		publicURL:    "https://origin.example.com/public", presignedURL: "https://origin.example.com/signed",
	}
	mu.Lock()
	current, currentCDN = store, signer
	mu.Unlock()
	ctx := context.Background()
	for _, read := range []func() (string, error){
		func() (string, error) { return ReadURL(ctx, "uploads/avatar/a.png", time.Minute) },
		func() (string, error) { return PresignGet(ctx, "assets/a.bin", 15*time.Minute) },
		func() (string, error) { return ResolveAssetURL("uploads/logo/a.png"), nil },
		func() (string, error) { return GetFileURL("recordings/a.opus"), nil },
		func() (string, error) { return GetAvatarURL("a.png"), nil },
		func() (string, error) { return GetAvatarThumbURL("a.png"), nil },
	} {
		got, err := read()
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got)
		if err != nil || u.Host != "cdn.example.com" || !strings.HasPrefix(u.Path, "/root/") || u.Query().Get("auth_key") == "" || len(u.Query()) != 1 {
			t.Fatalf("read did not use CDN TypeA signing: %q err=%v", got, err)
		}
	}
	signedURL, expiresAt, err := PresignGetWithExpiry(ctx, "client-resources/app.apk", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(signedURL)
	parts := strings.Split(u.Query().Get("auth_key"), "-")
	if len(parts) != 4 || len(parts[1]) != 32 || parts[2] != "0" || len(parts[3]) != 32 {
		t.Fatalf("invalid TypeA signature: %q", u.RawQuery)
	}
	issuedAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || !expiresAt.Equal(time.Unix(issuedAt, 0).Add(2*time.Hour)) || time.Since(time.Unix(issuedAt, 0)) > 5*time.Second {
		t.Fatalf("CDN expiry did not use configured duration from the issuance timestamp: %v err=%v", expiresAt, err)
	}
	again, err := PresignGet(ctx, "client-resources/app.apk", time.Minute)
	if err != nil || signedURL == again {
		t.Fatalf("CDN signing did not generate a fresh nonce: %v", err)
	}
	if store.publicCalls != 0 || store.presignCalls != 0 {
		t.Fatalf("CDN reads called origin URL generators: public=%d signed=%d", store.publicCalls, store.presignCalls)
	}
	for _, key := range []string{"", "https://external.example.com/logo.png"} {
		if got, err := ReadURL(ctx, key, time.Hour); err != nil || got != key {
			t.Fatalf("legacy external/empty read changed: %q err=%v", got, err)
		}
	}

	mu.Lock()
	currentCDN = nil
	mu.Unlock()
	if got, err := PresignGet(ctx, "assets/a.bin", time.Minute); err != nil || got != store.presignedURL {
		t.Fatalf("disabled CDN did not restore origin signing: %q err=%v", got, err)
	}
}

func TestCDNInitToggleKeepsUploadsAndDriverHelpers(t *testing.T) {
	previousStore, previousCDN := readStorage()
	t.Cleanup(func() {
		mu.Lock()
		current, currentCDN = previousStore, previousCDN
		mu.Unlock()
	})
	cfg := &config.Configuration{}
	cfg.JWT.Secret = "cdn-local-test-secret-01234567890123"
	cfg.Storage = config.StorageConfig{
		UseCDN: true, CDNBaseURL: "https://cdn.example.com", CDNKey: "test-secret",
		ActiveProfile: "test-profile",
		Profiles: map[string]config.StorageProfile{
			"test-profile": {Driver: DriverLocal, Local: config.LocalStorageConfig{RootPath: t.TempDir()}},
		},
	}
	if err := Init(cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	put, err := PresignPut(ctx, "staging/assets/a.bin", time.Minute, "application/octet-stream", 10)
	if err != nil || !strings.HasPrefix(put.UploadURL, "/api/storage/put?") || put.Mode != ModeLocal {
		t.Fatalf("CDN affected direct upload: %#v err=%v", put, err)
	}
	if _, err := AbsoluteLocalPath("uploads/avatar/a.png"); err != nil {
		t.Fatalf("CDN affected local storage helper: %v", err)
	}
	got, expiresAt, err := PresignGetWithExpiry(ctx, "assets/a.bin", time.Minute)
	if err != nil || !strings.HasPrefix(got, "https://cdn.example.com/assets/a.bin?sign=") || time.Until(expiresAt) < 59*time.Minute {
		t.Fatalf("Init did not enable default CDN signing: %q expiresAt=%v err=%v", got, expiresAt, err)
	}
	cfg.Storage.UseCDN = false
	cfg.Storage.CDNBaseURL = "ignored-invalid-url"
	if err := Init(cfg); err != nil {
		t.Fatal(err)
	}
	got, err = PresignGet(ctx, "assets/a.bin", time.Minute)
	if err != nil || !strings.HasPrefix(got, "/api/storage/get?") {
		t.Fatalf("Init did not disable CDN signing: %q err=%v", got, err)
	}
}

func TestCDNKeyEnvironmentReference(t *testing.T) {
	t.Setenv("DRAARL_TEST_CDN_KEY", "test-edgeone-key")
	cfg := config.StorageConfig{UseCDN: true, CDNBaseURL: "https://cdn.example.com", CDNKey: "${DRAARL_TEST_CDN_KEY}"}
	signer, err := newCDNSigner(cfg)
	if err != nil || signer.key != "test-edgeone-key" || cfg.CDNKey != "${DRAARL_TEST_CDN_KEY}" {
		t.Fatalf("CDN key environment reference was not resolved independently of configuration: %v", err)
	}
	t.Setenv("DRAARL_TEST_CDN_KEY", "")
	if _, err := newCDNSigner(cfg); err == nil || !strings.Contains(err.Error(), "Storage.CDNKey") {
		t.Fatalf("empty CDN secret was accepted: %v", err)
	}
	cfg.UseCDN = false
	if signer, err := newCDNSigner(cfg); signer != nil || err != nil {
		t.Fatalf("disabled CDN tried to load its secret: %v", err)
	}
}
