//go:build embed
// +build embed

package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEmbeddedFrontendIsServedByMainProgram(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	setupFrontend(engine, nil)

	assetPath := ""
	err := fs.WalkDir(webFS, "web/dist/assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			assetPath = strings.TrimPrefix(path, "web/dist")
			return fs.SkipAll
		}
		return nil
	})
	if err != nil || assetPath == "" {
		t.Fatalf("find embedded asset path=%q err=%v", assetPath, err)
	}
	assetRequest := httptest.NewRequest(http.MethodGet, assetPath, nil)
	assetResponse := httptest.NewRecorder()
	engine.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK {
		t.Fatalf("asset status=%d path=%q", assetResponse.Code, assetPath)
	}
	if got := assetResponse.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control=%q", got)
	}
}

func TestFrontendTitleSuffix(t *testing.T) {
	if got := frontendTitleSuffix("/admin/settings"); got != " - 站点配置" {
		t.Fatalf("title suffix=%q", got)
	}
	if got := frontendTitleSuffix("/admin/custom"); got != " - 管理后台" {
		t.Fatalf("admin fallback suffix=%q", got)
	}
	if got := frontendTitleSuffix("/unknown"); got != "" {
		t.Fatalf("unknown suffix=%q", got)
	}
}
