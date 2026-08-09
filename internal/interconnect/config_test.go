package interconnect

import (
	"strings"
	"testing"
)

func TestEdgeDefaultsReuseDraARLUDPPort(t *testing.T) {
	cfg := &EdgeConfig{Edge: EdgeSettings{Center: "center.example.com:60100", NodeID: "edge-test", Token: "token"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Edge.Listen != ":60050" {
		t.Fatalf("Listen=%q, want :60050", cfg.Edge.Listen)
	}
	if cfg.Edge.CenterUDP != "center.example.com:60050" {
		t.Fatalf("CenterUDP=%q", cfg.Edge.CenterUDP)
	}
	if cfg.Edge.DeviceSessionTimeoutSeconds != 20 || cfg.Edge.GrantRenewBeforeSeconds != 30 || cfg.Edge.DisconnectedLocalGraceSeconds != 15 {
		t.Fatalf("edge lease defaults changed: timeout=%d renew=%d grace=%d", cfg.Edge.DeviceSessionTimeoutSeconds, cfg.Edge.GrantRenewBeforeSeconds, cfg.Edge.DisconnectedLocalGraceSeconds)
	}
}

func TestEdgeRejectsInvalidSessionLeaseSettings(t *testing.T) {
	cfg := &EdgeConfig{Edge: EdgeSettings{Center: "127.0.0.1:60100", NodeID: "edge-test", Token: "token", DeviceSessionTimeoutSeconds: 4, GrantRenewBeforeSeconds: 30}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DeviceSessionTimeoutSeconds") {
		t.Fatalf("unexpected timeout validation error: %v", err)
	}
	cfg.Edge.DeviceSessionTimeoutSeconds, cfg.Edge.GrantRenewBeforeSeconds = 20, 91
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GrantRenewBeforeSeconds") {
		t.Fatalf("unexpected renewal validation error: %v", err)
	}
	cfg.Edge.GrantRenewBeforeSeconds, cfg.Edge.DisconnectedLocalGraceSeconds = 30, 121
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DisconnectedLocalGraceSeconds") {
		t.Fatalf("unexpected grace validation error: %v", err)
	}
}

func TestEdgeCustomSharedUDPPortIsPreserved(t *testing.T) {
	cfg := &EdgeConfig{Edge: EdgeSettings{Center: "127.0.0.1:60100", CenterUDP: "127.0.0.1:61000", Listen: ":62000", NodeID: "edge-test", Token: "token"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Edge.CenterUDP != "127.0.0.1:61000" || cfg.Edge.Listen != ":62000" {
		t.Fatalf("custom UDP ports changed: %+v", cfg.Edge)
	}
}

func TestEdgeProxyProtocolV2IsNormalized(t *testing.T) {
	cfg := &EdgeConfig{Edge: EdgeSettings{Center: "127.0.0.1:60100", NodeID: "edge-test", Token: "token", ProxyProtocol: " V2 "}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Edge.ProxyProtocol != "v2" {
		t.Fatalf("ProxyProtocol=%q want=v2", cfg.Edge.ProxyProtocol)
	}
}

func TestEdgeRejectsUnsupportedProxyProtocol(t *testing.T) {
	cfg := &EdgeConfig{Edge: EdgeSettings{Center: "127.0.0.1:60100", NodeID: "edge-test", Token: "token", ProxyProtocol: "v1"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ProxyProtocol") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
