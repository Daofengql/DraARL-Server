package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"draarl/internal/gormdb"
	"draarl/pkg/cache"
	"draarl/pkg/storage"

	"github.com/gin-gonic/gin"
)

func logoSetting(theme string) (string, bool, error) {
	switch theme {
	case "", "light":
		return "system.logo_url", false, nil
	case "dark":
		return "system.logo_dark_url", true, nil
	default:
		return "", false, fmt.Errorf("Logo主题必须为 light 或 dark")
	}
}

// SiteLogoURL exposes a stable same-origin URL without a storage signature.
// The reference hash refreshes an already open page when a logo is replaced.
func SiteLogoURL(reference string, dark bool) string {
	if strings.TrimSpace(reference) == "" {
		return ""
	}
	route := "/logo.png"
	if dark {
		route = "/logo-dark.png"
	}
	return fmt.Sprintf("%s?v=%x", route, sha256.Sum256([]byte(reference)))
}

func SiteFaviconURL(reference string) string {
	if strings.TrimSpace(reference) == "" {
		return ""
	}
	return fmt.Sprintf("/favicon.ico?v=%x", sha256.Sum256([]byte(reference)))
}

func (h *SiteConfigHandler) ServeLogo(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var cfg *gormdb.SystemInfoConfig
	var err error
	if configCache := cache.GetConfigCache(); configCache != nil {
		cfg, err = configCache.GetSystemInfoConfig(ctx)
	} else {
		cfg, err = h.repo.WithContext(ctx).GetSystemInfoConfig()
	}
	if err != nil || cfg == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	reference := cfg.LogoURL
	if c.Request.URL.Path == "/favicon.ico" {
		reference = cfg.FaviconURL
	} else if c.Request.URL.Path == "/logo-dark.png" && cfg.LogoDarkURL != "" {
		reference = cfg.LogoDarkURL
	}
	serveSiteLogo(c, ctx, reference, openSiteLogo)
}

func serveSiteLogo(c *gin.Context, ctx context.Context, reference string, open func(context.Context, string) (io.ReadCloser, error)) {
	c.Header("Cache-Control", "public, max-age=0, must-revalidate")
	if strings.TrimSpace(reference) == "" {
		c.Status(http.StatusNotFound)
		return
	}
	reader, err := open(ctx, reference)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer reader.Close()
	const maxLogoBytes = 10 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(reader, maxLogoBytes+1))
	if err != nil || len(data) > maxLogoBytes {
		c.Status(http.StatusBadGateway)
		return
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/vnd.microsoft.icon":
	default:
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", contentType)
	c.Header("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	http.ServeContent(c.Writer, c.Request, "logo", time.Time{}, bytes.NewReader(data))
}

var legacyLogoHTTPClient = &http.Client{Timeout: 10 * time.Second}

func openSiteLogo(ctx context.Context, reference string) (io.ReadCloser, error) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return nil, err
	}
	// Uploaded logos used to be stored as absolute S3/CDN URLs. Recover the
	// original object key and read it with server credentials, even if the old
	// URL signature has expired.
	for _, prefix := range []string{"/uploads/logo/", "/uploads/favicon/"} {
		if index := strings.Index(parsed.Path, prefix); index >= 0 {
			return storage.Open(ctx, parsed.Path[index+1:])
		}
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("unsupported logo URL scheme")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reference, nil)
		if err != nil {
			return nil, err
		}
		response, err := legacyLogoHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("logo origin returned HTTP %d", response.StatusCode)
		}
		return response.Body, nil
	}
	return storage.Open(ctx, strings.TrimPrefix(reference, "/files/"))
}
