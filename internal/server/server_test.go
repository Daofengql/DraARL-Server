package server

import (
	"draarl/internal/config"
	"draarl/internal/middleware"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHTTPForwardedHeadersRespectTrustedProxyBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		proxies []string
		want    string
	}{
		{"direct", nil, "192.0.2.5"},
		{"untrusted", []string{"10.0.0.0/8"}, "192.0.2.5"},
		{"trusted", []string{"192.0.2.0/24"}, "198.51.100.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Configuration{}
			cfg.System.HTTPTrustedProxyCIDRs = tc.proxies
			engine := newHTTPEngine(cfg)
			engine.GET("/ip", func(c *gin.Context) { c.String(200, c.ClientIP()) })
			req := httptest.NewRequest("GET", "/ip", nil)
			req.RemoteAddr = "192.0.2.5:1234"
			req.Header.Set("X-Forwarded-For", "198.51.100.10")
			out := httptest.NewRecorder()
			engine.ServeHTTP(out, req)
			if out.Body.String() != tc.want {
				t.Fatalf("client IP=%q want=%q", out.Body, tc.want)
			}
		})
	}
	engine := newHTTPEngine(&config.Configuration{})
	middleware.InitDeviceRateLimiter()
	engine.GET("/captcha", middleware.CaptchaRateLimit(), func(c *gin.Context) { c.Status(200) })
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest("GET", "/captcha", nil)
		req.RemoteAddr = "192.0.2.222:1234"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, req)
		if i >= 5 && out.Code != http.StatusTooManyRequests {
			t.Fatalf("forged XFF bypassed limiter: status=%d", out.Code)
		}
	}
}

func TestLegacyGhostRoutesAreNotRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerRadioRoutes(engine.Group("/api/radio"))

	routes := make([]string, 0, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes = append(routes, route.Method+" "+route.Path)
	}
	for _, legacy := range []string{
		"PUT /api/radio/ssid",
		"PUT /api/radio/group",
		"GET /api/radio/conflict",
	} {
		if slices.Contains(routes, legacy) {
			t.Fatalf("legacy ghost route remains registered: %s", legacy)
		}
	}
}

func TestOriginGuardRejectsDisallowedReferer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{
		"http://localhost:9001": {},
	}))
	engine.GET("/api/test/origin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test/origin", nil)
	req.Header.Set("Referer", "http://localhost:5173/radio")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed referer fallback, got %d", recorder.Code)
	}
}

func TestOriginGuardAllowsAllowedReferer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{
		"http://localhost:9001": {},
	}))
	engine.GET("/api/test/origin", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test/origin", nil)
	req.Header.Set("Referer", "http://localhost:9001/radio")

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed referer fallback, got %d", recorder.Code)
	}
}

func TestOriginGuardAllowsRequestWithoutBrowserHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{
		"http://localhost:9001": {},
	}))
	engine.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request without browser headers to pass, got %d", recorder.Code)
	}
}

func TestOriginGuardAllowsSignedLocalStoragePut(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{
		"http://localhost:9001": {},
	}))
	engine.PUT("/api/storage/put", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPut, "/api/storage/put?token=signed-value&key=staging%2Fassets%2F1%2Ffile.bin", nil)
	req.Header.Set("Origin", "http://localhost:9001")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected signed local storage PUT to pass origin guard, got %d", recorder.Code)
	}
}

func TestOriginGuardStillRejectsTokenQueryOnOtherRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{}))
	engine.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz?token=sensitive", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected token query on unrelated route to be rejected, got %d", recorder.Code)
	}
}
