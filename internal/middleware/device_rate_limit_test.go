package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestCaptchaRateLimitRejectsFrequentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldLimiter := deviceRateLimiter
	defer func() {
		deviceRateLimiter = oldLimiter
	}()

	deviceRateLimiter = &DeviceRateLimiter{
		limits: make(map[string]*RateLimitEntry),
		rules: map[string]RateLimitRule{
			"captcha-ip-burst": {
				Key:    "ip",
				Limit:  2,
				Window: time.Minute,
			},
			"captcha-ip-minute": {
				Key:    "ip",
				Limit:  10,
				Window: time.Minute,
			},
		},
	}

	engine := gin.New()
	engine.GET("/api/captcha", CaptchaRateLimit(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200})
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/captcha", nil)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected request %d to pass, got %d", i+1, recorder.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/captcha", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected third request to be rate limited, got %d", recorder.Code)
	}
}
