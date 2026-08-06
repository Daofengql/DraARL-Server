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
