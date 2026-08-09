package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"draarl/internal/config"

	"github.com/gin-gonic/gin"
)

func newMessageAPITestRouter(guard *MessageAPIGuard, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/messages", func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Next()
	}, guard.Middleware(), handler)
	return router
}

func messageAPITestConfig() config.MessageAPIConfig {
	return config.MessageAPIConfig{
		DefaultPageSize: 4, MaxPageSize: 9,
		RequestsPerMinutePerUser: 2, RequestsPerMinutePerIP: 10, MaxConcurrentQueries: 2,
	}
}

func TestMessageAPIGuardAppliesPageLimitsAndUserRateLimit(t *testing.T) {
	guard := NewMessageAPIGuard(messageAPITestConfig())
	now := time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC)
	guard.now = func() time.Time { return now }
	router := newMessageAPITestRouter(guard, func(c *gin.Context) {
		defaultPageSize, maxPageSize := MessageAPIPageLimits(c)
		c.JSON(http.StatusOK, gin.H{"default": defaultPageSize, "max": maxPageSize})
	})
	for index := 0; index < 2; index++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/messages", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"default\":4,\"max\":9}" {
			t.Fatalf("request %d status=%d body=%s", index, recorder.Code, recorder.Body.String())
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/messages", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "60" {
		t.Fatalf("rate limit status=%d retry=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body["error"] != "message_api_user_rate_limited" {
		t.Fatalf("rate limit body=%s err=%v", recorder.Body.String(), err)
	}
}

func TestMessageAPIGuardAppliesIPRateLimit(t *testing.T) {
	cfg := messageAPITestConfig()
	cfg.RequestsPerMinutePerUser = 10
	cfg.RequestsPerMinutePerIP = 1
	guard := NewMessageAPIGuard(cfg)
	guard.now = func() time.Time { return time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC) }
	router := newMessageAPITestRouter(guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for index, want := range []int{http.StatusNoContent, http.StatusTooManyRequests} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/messages", nil)
		request.RemoteAddr = "198.51.100.2:4567"
		router.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Fatalf("request %d status=%d want=%d body=%s", index, recorder.Code, want, recorder.Body.String())
		}
		if index == 1 && !json.Valid(recorder.Body.Bytes()) {
			t.Fatalf("invalid rejection body: %s", recorder.Body.String())
		}
	}
}

func TestMessageAPIGuardRejectsWhenConcurrencyIsFull(t *testing.T) {
	cfg := messageAPITestConfig()
	cfg.RequestsPerMinutePerUser = 10
	cfg.MaxConcurrentQueries = 1
	guard := NewMessageAPIGuard(cfg)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	router := newMessageAPITestRouter(guard, func(c *gin.Context) {
		once.Do(func() { close(started) })
		<-release
		c.Status(http.StatusNoContent)
	})
	firstDone := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/messages", nil)
		request.RemoteAddr = "203.0.113.3:1111"
		router.ServeHTTP(recorder, request)
		firstDone <- recorder.Code
	}()
	<-started
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/messages", nil)
	request.RemoteAddr = "203.0.113.4:2222"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrency status=%d retry=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	close(release)
	if status := <-firstDone; status != http.StatusNoContent {
		t.Fatalf("first request status=%d", status)
	}
}

func TestMessageAPIPageLimitsFallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	defaultPageSize, maxPageSize := MessageAPIPageLimits(context)
	if defaultPageSize != config.DefaultMessageAPIPageSize || maxPageSize != config.DefaultMessageAPIMaxPageSize {
		t.Fatalf("fallback limits=(%d,%d)", defaultPageSize, maxPageSize)
	}
}
