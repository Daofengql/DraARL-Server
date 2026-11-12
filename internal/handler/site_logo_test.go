package handler

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"draarl/internal/gormdb"
	"github.com/gin-gonic/gin"
)

func TestBrandURLsUseSameOriginWithoutSigning(t *testing.T) {
	stored := &gormdb.SystemInfoConfig{LogoURL: "uploads/logo/a.png", LogoDarkURL: "uploads/logo/b.png", FaviconURL: "uploads/favicon/a.ico"}
	resolved := resolveSystemInfoURLs(stored)
	for _, tc := range []struct{ got, route string }{
		{resolved.LogoURL, "/logo.png?v="}, {resolved.LogoDarkURL, "/logo-dark.png?v="}, {resolved.FaviconURL, "/favicon.ico?v="},
	} {
		if !strings.HasPrefix(tc.got, tc.route) || strings.Contains(tc.got, "sign") {
			t.Fatalf("unexpected brand URL %q", tc.got)
		}
	}
	if stored.LogoURL != "uploads/logo/a.png" {
		t.Fatal("resolving URLs mutated persisted configuration")
	}
	if SiteLogoURL("", false) != "" || SiteFaviconURL("") != "" {
		t.Fatal("empty branding must stay empty")
	}
	if SiteLogoURL("uploads/logo/replaced.png", false) == resolved.LogoURL {
		t.Fatal("replacement did not refresh URL")
	}
}

func TestBrandProxyServesImageAndRevalidates(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	open := func(ctx context.Context, reference string) (io.ReadCloser, error) {
		if reference != "uploads/logo/a.png" {
			t.Fatalf("unexpected storage key %q", reference)
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	request := func(method, etag string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(method, "/logo.png", nil)
		c.Request.Header.Set("If-None-Match", etag)
		serveSiteLogo(c, c.Request.Context(), "uploads/logo/a.png", open)
		c.Writer.WriteHeaderNow()
		return response
	}
	response := request(http.MethodGet, "")
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
	}
	if response.Header().Get("Location") != "" {
		t.Fatal("proxy redirected to storage")
	}
	etag := response.Header().Get("ETag")
	if etag == "" || request(http.MethodGet, etag).Code != 304 {
		t.Fatal("conditional GET did not revalidate")
	}
	head := request(http.MethodHead, "")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatal("invalid HEAD response")
	}
}

func TestBrandProxyRejectsMissingAndNonImageObjects(t *testing.T) {
	for _, tc := range []struct {
		name, reference, body string
		err                   error
		status                int
	}{
		{"unconfigured", "", "", nil, 404},
		{"storage unavailable", "uploads/logo/a.png", "", errors.New("offline"), 502},
		{"non image", "uploads/logo/a.png", "<script>alert(1)</script>", nil, 502},
		{"favicon", "uploads/favicon/a.ico", "\x00\x00\x01\x00\x01\x00", nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
			serveSiteLogo(c, c.Request.Context(), tc.reference, func(context.Context, string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(tc.body)), tc.err
			})
			c.Writer.WriteHeaderNow()
			if response.Code != tc.status {
				t.Fatalf("status=%d, want %d", response.Code, tc.status)
			}
		})
	}
}

func TestCommSettingsRejectConflictingDurations(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/config/comm-settings", strings.NewReader(`{"enabled":true,"retention_days":30,"min_duration_ms":1001,"max_duration_sec":1,"batch_upload_sec":10}`))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateCommSettings(c)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "最小录制阈值不能超过最大录制时长") {
		t.Fatalf("unexpected validation response: %d %s", response.Code, response.Body.String())
	}
}
