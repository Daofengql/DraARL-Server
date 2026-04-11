package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOriginGuardRejectsDisallowedReferer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(originGuardMiddleware(map[string]struct{}{
		"http://localhost:9001": {},
	}))
	engine.GET("/api/radio/conflict", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/radio/conflict", nil)
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
	engine.GET("/api/radio/conflict", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/radio/conflict", nil)
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
