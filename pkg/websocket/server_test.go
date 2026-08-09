package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOriginAllowsConfiguredOrigin(t *testing.T) {
	SetAllowedOrigins([]string{"https://app.example.com"})
	t.Cleanup(func() {
		SetAllowedOrigins(nil)
	})

	req := httptest.NewRequest(http.MethodGet, "https://server.example.com/ws", nil)
	req.Header.Set("Origin", "https://app.example.com")

	if !checkOrigin(req) {
		t.Fatal("expected configured origin to pass websocket origin check")
	}
}

func TestCheckOriginRejectsUnconfiguredServerOrigin(t *testing.T) {
	SetAllowedOrigins([]string{"https://app.example.com"})
	t.Cleanup(func() {
		SetAllowedOrigins(nil)
	})

	req := httptest.NewRequest(http.MethodGet, "https://server.example.com/ws", nil)
	req.Host = "server.example.com"
	req.Header.Set("Origin", "https://server.example.com")

	if checkOrigin(req) {
		t.Fatal("expected unconfigured server origin to be rejected")
	}
}
