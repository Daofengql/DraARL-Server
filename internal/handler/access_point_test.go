package handler

import (
	"draarl/internal/gormdb"
	"encoding/json"
	"testing"
	"time"
)

func TestLocalAccessPointUsesValidatedNATEndpoint(t *testing.T) {
	now := time.Now()
	item, ok := localAccessPoint("site-a", "测试站", "radio.example.com", 16050, now)
	if !ok || item.ID != "site-a" || item.DisplayName != "测试站" || item.UDPHost != "radio.example.com" || item.UDPPort != 16050 || !item.HealthySampleAt.Equal(now) {
		t.Fatalf("endpoint=%+v accepted=%v", item, ok)
	}
	for _, host := range []string{"", "https://example.com", "example.com:60050"} {
		if _, ok := localAccessPoint("site-a", "测试站", host, 60050, now); ok {
			t.Fatalf("published invalid host %q", host)
		}
	}
	if _, ok := localAccessPoint("internal/a", "测试站", "example.com", 60050, now); ok {
		t.Fatal("published invalid ID")
	}
	for _, port := range []int{0, -1, 65536} {
		if _, ok := localAccessPoint("a", "测试站", "example.com", port, now); ok {
			t.Fatalf("published invalid port %d", port)
		}
	}
}

func TestNormalizeLocalAccessConfig(t *testing.T) {
	got, err := normalizeAccessDiscoveryConfig(&gormdb.AccessDiscoveryConfig{NodeID: " site-a ", UDPHost: " radio.example.com ", UDPPort: 16050})
	if err != nil || got.NodeID != "site-a" || got.UDPHost != "radio.example.com" || got.UDPPort != 16050 {
		t.Fatalf("settings=%+v err=%v", got, err)
	}
	for _, c := range []gormdb.AccessDiscoveryConfig{{UDPPort: 65536}, {NodeID: "a/b"}, {UDPHost: "https://example.com"}} {
		if _, err := normalizeAccessDiscoveryConfig(&c); err == nil {
			t.Fatalf("invalid settings accepted: %+v", c)
		}
	}
	if _, err := normalizeAccessDiscoveryConfig(&gormdb.AccessDiscoveryConfig{}); err != nil {
		t.Fatal("default local endpoint must be allowed", err)
	}
}

func TestLocalAccessConfigIgnoresRemovedOptions(t *testing.T) {
	var c gormdb.AccessDiscoveryConfig
	err := json.Unmarshal([]byte(`{"enabled":false,"node_id":"a","udp_host":"example.com","udp_port":60050,"priority":-10,"token_ttl_seconds":999999,"center":{"enabled":false}}`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeAccessDiscoveryConfig(&c); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(c)
	var fields map[string]any
	_ = json.Unmarshal(encoded, &fields)
	if len(fields) != 3 {
		t.Fatalf("obsolete fields exposed: %s", encoded)
	}
}

func TestDefaultAccessDiscoveryHostUsesCurrentAPIWithoutMutatingCache(t *testing.T) {
	settings := &gormdb.AccessDiscoveryConfig{NodeID: "center", UDPPort: 16050}
	for apiHost, want := range map[string]string{
		"api.example.com:9000": "api.example.com",
		"other.example.com":    "other.example.com",
		"192.0.2.10:9000":      "192.0.2.10",
		"[2001:db8::1]:9000":   "2001:db8::1",
	} {
		got, err := defaultAccessDiscoveryHost(settings, apiHost)
		if err != nil || got.UDPHost != want || got.UDPPort != 16050 || settings.UDPHost != "" {
			t.Fatalf("API %q: settings=%+v err=%v", apiHost, got, err)
		}
	}
	settings.UDPHost = "nat.example.com"
	got, err := defaultAccessDiscoveryHost(settings, "api.example.com:9000")
	if err != nil || got.UDPHost != "nat.example.com" {
		t.Fatal("explicit NAT endpoint did not override API host", err)
	}
	settings.UDPHost = ""
	if _, err := defaultAccessDiscoveryHost(settings, "invalid/host"); err == nil {
		t.Fatal("invalid default API host was accepted")
	}
}
