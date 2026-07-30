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

func TestDynamicCodeRateLimitDefaults(t *testing.T) {
	limiter := newDeviceRateLimiter()
	tests := []struct {
		name   string
		limit  int
		window time.Duration
	}{
		{name: "request-code-ip", limit: 30, window: time.Minute},
		{name: "request-code-mac", limit: 10, window: time.Minute},
		{name: "bind-user", limit: 20, window: time.Minute},
	}

	for _, tt := range tests {
		rule, ok := limiter.rules[tt.name]
		if !ok {
			t.Fatalf("missing rate limit rule %q", tt.name)
		}
		if rule.Limit != tt.limit || rule.Window != tt.window {
			t.Errorf("rule %q = %d/%s, want %d/%s", tt.name, rule.Limit, rule.Window, tt.limit, tt.window)
		}
	}
}

func TestPublicClientReleaseRateLimitDefaults(t *testing.T) {
	limiter := newDeviceRateLimiter()
	rule, ok := limiter.rules["public-client-release-ip"]
	if !ok {
		t.Fatal("missing public client release IP rate limit")
	}
	if rule.Limit != 30 || rule.Window != time.Minute || rule.Key != "ip" {
		t.Fatalf("public client release rule=%#v, want 30 requests per IP per minute", rule)
	}
}

func TestAccessDiscoveryTokenLimiterSupportsSharedNATButCapsEachUser(t *testing.T) {
	limiter := newDeviceRateLimiter()
	if got := accessDiscoveryTokenPrincipalKey("203.0.113.10", " Alice "); got != "203.0.113.10\x00alice" {
		t.Fatalf("normalized discovery principal = %q", got)
	}
	for i := 0; i < 50; i++ {
		if allowed, _ := limiter.checkLimit("access-discovery-token-ip-burst", "203.0.113.10"); !allowed {
			t.Fatalf("shared NAT was blocked at distinct request %d", i+1)
		}
		if allowed, _ := limiter.checkLimit("access-discovery-token-user", accessDiscoveryTokenPrincipalKey("203.0.113.10", "user-"+intToStr(i))); !allowed {
			t.Fatalf("distinct user %d under shared NAT was blocked", i)
		}
	}
	for i := 0; i < 10; i++ {
		if allowed, _ := limiter.checkLimit("access-discovery-token-user", accessDiscoveryTokenPrincipalKey("203.0.113.10", "one-user")); !allowed {
			t.Fatalf("one user was blocked before configured limit at %d", i+1)
		}
	}
	if allowed, _ := limiter.checkLimit("access-discovery-token-user", accessDiscoveryTokenPrincipalKey("203.0.113.10", "one-user")); allowed {
		t.Fatal("one user exceeded the configured minute limit")
	}
	if allowed, _ := limiter.checkLimit("access-discovery-token-user", accessDiscoveryTokenPrincipalKey("198.51.100.20", "one-user")); !allowed {
		t.Fatal("one source IP exhausted another source IP's unauthenticated username bucket")
	}
}
