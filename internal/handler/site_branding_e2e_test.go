package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/server"
	"draarl/pkg/cache"
	appjwt "draarl/pkg/jwt"
	"draarl/pkg/storage"
)

// Run this fixture alone against a disposable MySQL database and private S3
// bucket: DRAARL_SITE_BRANDING_E2E=true DRAARL_TEST_CONFIG=... go test
// ./internal/handler -run '^TestSiteBrandingMySQLS3$' -count=1.
func TestSiteBrandingMySQLS3(t *testing.T) {
	if os.Getenv("DRAARL_SITE_BRANDING_E2E") != "true" {
		t.Skip("set DRAARL_SITE_BRANDING_E2E=true and DRAARL_TEST_CONFIG")
	}
	cfg, err := config.Load(os.Getenv("DRAARL_TEST_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Database.DBName, "draarl_test_") || cfg.Storage.Driver != "s3" || !strings.HasPrefix(cfg.Storage.S3.Bucket, "draarl-test-") {
		t.Fatal("a dedicated draarl_test_ database and draarl-test- S3 bucket are required")
	}
	if err := gormdb.Init(&gormdb.Config{DSN: cfg.GetDSN(), LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	db := gormdb.Get()
	if err := db.AutoMigrate(&gormdb.User{}, &gormdb.SiteConfig{}, &gormdb.OperatorLog{}); err != nil {
		t.Fatal(err)
	}
	if err := appjwt.SetSecret(cfg.JWT.Secret); err != nil {
		t.Fatal(err)
	}
	if err := cache.InitManager(); err != nil {
		t.Fatal(err)
	}
	admin := &gormdb.User{Name: "site-branding-admin", Roles: "admin", Status: 1, ApprovalStatus: 1, SessionVersion: 1}
	if err := db.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Delete(admin).Error })
	token, err := appjwt.GenerateTokenForUser(admin.ID, admin.Name, []string{"admin"}, admin.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	app := server.New(cfg)
	httpServer := httptest.NewServer(app.GetEngine())
	t.Cleanup(httpServer.Close)

	request := func(method, path string, body io.Reader, contentType string, auth bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, httpServer.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	read := func(response *http.Response, status int) []byte {
		t.Helper()
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != status {
			t.Fatalf("HTTP %d (want %d): %s; %v", response.StatusCode, status, data, err)
		}
		return data
	}
	imageData := func(c color.RGBA) []byte {
		var data bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	upload := func(path string, data []byte, route string) string {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		header := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="brand.png"`}, "Content-Type": {"image/png"}}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Data struct {
				URL string `json:"file_url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(read(request("POST", path, &body, writer.FormDataContentType(), true), 200), &result); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(result.Data.URL, route+"?v=") {
			t.Fatalf("unexpected brand URL %q", result.Data.URL)
		}
		return result.Data.URL
	}

	lightURL := upload("/api/upload/logo?theme=light", imageData(color.RGBA{R: 255, A: 255}), "/logo.png")
	darkURL := upload("/api/upload/logo?theme=dark", imageData(color.RGBA{B: 255, A: 255}), "/logo-dark.png")
	faviconData := imageData(color.RGBA{G: 255, A: 255})
	faviconURL := upload("/api/upload/favicon", faviconData, "/favicon.ico")
	repo := gormdb.GetSiteConfigRepo()
	stored, err := repo.GetSystemInfoConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{stored.LogoURL, stored.LogoDarkURL, stored.FaviconURL} {
		key := key
		t.Cleanup(func() { _ = storage.Delete(context.Background(), key) })
		if strings.HasPrefix(key, "/") || strings.HasPrefix(key, "http") {
			t.Fatalf("persisted a presentation URL: %q", key)
		}
	}
	light := read(request("GET", lightURL, nil, "", false), 200)
	dark := read(request("GET", darkURL, nil, "", false), 200)
	if bytes.Equal(light, dark) {
		t.Fatal("theme uploads overwrote each other")
	}
	if !bytes.Equal(read(request("GET", faviconURL, nil, "", false), 200), faviconData) {
		t.Fatal("favicon proxy changed the object")
	}
	read(request("HEAD", "/favicon.ico", nil, "", false), 200)
	read(request("GET", "/api/config/public", nil, "", false), 200)

	// A dead presign endpoint proves brand routes use the backend S3 client.
	otherURL, err := storage.ReadURL(context.Background(), "uploads/assets/other.bin", time.Hour)
	if err != nil || !strings.Contains(otherURL, "X-Amz-Signature=") {
		t.Fatalf("other resources lost signing: %q %v", otherURL, err)
	}
	if strings.Contains(string(read(request("GET", "/api/config/public", nil, "", false), 200)), "X-Amz-") {
		t.Fatal("public branding leaked a signed storage URL")
	}

	// Replacing a logo refreshes the browser URL and the served content.
	replacedURL := upload("/api/upload/logo?theme=light", imageData(color.RGBA{R: 128, A: 255}), "/logo.png")
	if replacedURL == lightURL || bytes.Equal(read(request("GET", replacedURL, nil, "", false), 200), light) {
		t.Fatal("logo replacement remained stale")
	}
	replaced, err := repo.GetSystemInfoConfig()
	if err != nil {
		t.Fatal(err)
	}
	replacedKey := replaced.LogoURL
	t.Cleanup(func() { _ = storage.Delete(context.Background(), replacedKey) })

	// Old absolute URLs with expired signatures still resolve via S3 credentials.
	replaced.LogoURL = "http://expired-signature.invalid/bucket/" + replaced.LogoURL + "?X-Amz-Signature=expired"
	if err := repo.SetSystemInfoConfig(*replaced); err != nil {
		t.Fatal(err)
	}
	if err := cache.GetConfigCache().InvalidateSystemInfoConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	read(request("GET", "/logo.png", nil, "", false), 200)

	read(request("DELETE", "/api/config/logo?theme=dark", nil, "", true), 200)
	if !bytes.Equal(read(request("GET", "/logo-dark.png", nil, "", false), 200), read(request("GET", "/logo.png", nil, "", false), 200)) {
		t.Fatal("missing dark logo did not fall back to light")
	}
	read(request("DELETE", "/api/config/favicon", nil, "", true), 200)
	read(request("GET", "/favicon.ico", nil, "", false), 404)
	read(request("POST", "/api/upload/logo?theme=invalid", nil, "", true), 400)
}
