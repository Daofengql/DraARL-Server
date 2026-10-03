package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"draarl/internal/config"

	"github.com/gin-gonic/gin"
)

func TestShouldUseSecureCookieFollowsRequestTransport(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	previousConfig := config.Config
	config.Config = &config.Configuration{}
	config.Config.System.HTTPTrustedProxyCIDRs = []string{"192.0.2.1/32"}
	config.SetReleaseBuild(true)
	t.Cleanup(func() {
		config.SetReleaseBuild(previousRelease)
		config.Config = previousConfig
	})

	tests := []struct {
		name      string
		target    string
		forwarded string
		remote    string
		want      bool
	}{
		{name: "release build over HTTP", target: "http://radio.example.com", want: false},
		{name: "direct HTTPS", target: "https://radio.example.com", want: true},
		{name: "HTTPS reverse proxy", target: "http://radio.example.com", forwarded: "https", remote: "192.0.2.1:1234", want: true},
		{name: "untrusted forwarded HTTPS", target: "http://radio.example.com", forwarded: "https", remote: "198.51.100.1:1234", want: false},
		{name: "untrusted IPv6", target: "http://radio.example.com", forwarded: "https", remote: "[2001:db8::1]:1234", want: false},
		{name: "multiple forwarded schemes", target: "http://radio.example.com", forwarded: "https,http", remote: "192.0.2.1:1234", want: false},
		{name: "HTTP reverse proxy", target: "http://radio.example.com", forwarded: "http", want: false},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, tt.target, nil)
			context.Request.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				context.Request.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}

			if got := shouldUseSecureCookie(context); got != tt.want {
				t.Fatalf("shouldUseSecureCookie() = %v, want %v", got, tt.want)
			}
		})
	}
}
