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
	config.SetReleaseBuild(true)
	t.Cleanup(func() {
		config.SetReleaseBuild(previousRelease)
	})

	tests := []struct {
		name      string
		target    string
		forwarded string
		want      bool
	}{
		{name: "release build over HTTP", target: "http://radio.example.com", want: false},
		{name: "direct HTTPS", target: "https://radio.example.com", want: true},
		{name: "HTTPS reverse proxy", target: "http://radio.example.com", forwarded: "https", want: true},
		{name: "HTTP reverse proxy", target: "http://radio.example.com", forwarded: "http", want: false},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.forwarded != "" {
				context.Request.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}

			if got := shouldUseSecureCookie(context); got != tt.want {
				t.Fatalf("shouldUseSecureCookie() = %v, want %v", got, tt.want)
			}
		})
	}
}
