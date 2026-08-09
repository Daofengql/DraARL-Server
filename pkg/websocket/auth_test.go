package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePreAuthData_CookieOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)
	req.AddCookie(&http.Cookie{Name: "ws_token", Value: "cookie-token-123"})

	data := ParsePreAuthData(req)
	if data.Token != "cookie-token-123" {
		t.Errorf("expected token from cookie, got %q", data.Token)
	}
}

func TestParsePreAuthData_QueryParamOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?token=query-token-456", nil)

	data := ParsePreAuthData(req)
	if data.Token != "query-token-456" {
		t.Errorf("expected token from query param, got %q", data.Token)
	}
}

func TestParsePreAuthData_BothEmpty(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)

	data := ParsePreAuthData(req)
	if data.Token != "" {
		t.Errorf("expected empty token, got %q", data.Token)
	}
}

func TestParsePreAuthData_CookieTakesPriority(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?token=query-token-456", nil)
	req.AddCookie(&http.Cookie{Name: "ws_token", Value: "cookie-token-123"})

	data := ParsePreAuthData(req)
	if data.Token != "cookie-token-123" {
		t.Errorf("cookie should take priority, got %q", data.Token)
	}
}
