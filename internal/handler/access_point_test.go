package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
)

func TestPublishedEdgeAccessPointRequiresFreshOnlineRegisteredNode(t *testing.T) {
	now := time.Now()
	nodeID, publicID := "edge-private-internal-id", "ap-public-id"
	registeredAt := now.Add(-time.Hour)
	lastHeartbeat := now.Add(-time.Second)
	node := &gormdb.Server{
		NodeID: &nodeID, PublicAccessID: &publicID, DisplayName: "福州电信入口",
		PublicAccessEnabled: true, PublicUDPHost: "edge.example.com", PublicUDPPort: 60050,
		PublicRegion: "福建省 福州市", PublicNetwork: "电信", PublicPriority: 10, Status: 1,
		NodeRegisteredAt: &registeredAt, Note: "private-note", NodeRemoteAddr: "10.0.0.1:1234",
	}
	status := interconnect.NodeStatus{Online: true, LastHeartbeat: &lastHeartbeat}

	item, ok := publishedEdgeAccessPoint(node, status, now, 20*time.Second)
	if !ok {
		t.Fatal("fresh online node was not published")
	}
	if item.ID != publicID || item.DisplayName != node.DisplayName || item.UDPHost != "edge.example.com" {
		t.Fatalf("unexpected public item: %+v", item)
	}

	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{nodeID, node.Note, node.NodeRemoteAddr, "node_id", "remote_addr", "metrics", "credential", "token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public item leaked %q: %s", forbidden, encoded)
		}
	}

	tests := []struct {
		name   string
		mutate func(*gormdb.Server, *interconnect.NodeStatus)
	}{
		{"not public", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicAccessEnabled = false }},
		{"disabled", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.Status = 0 }},
		{"unregistered", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.NodeRegisteredAt = nil }},
		{"offline", func(_ *gormdb.Server, s *interconnect.NodeStatus) { s.Online = false }},
		{"no heartbeat", func(_ *gormdb.Server, s *interconnect.NodeStatus) { s.LastHeartbeat = nil }},
		{"stale", func(_ *gormdb.Server, s *interconnect.NodeStatus) {
			stale := now.Add(-time.Minute)
			s.LastHeartbeat = &stale
		}},
		{"invalid host", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicUDPHost = "https://bad.example" }},
		{"empty region", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicRegion = "" }},
		{"province-only region", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicRegion = "福建省" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copyNode, copyStatus := *node, status
			tt.mutate(&copyNode, &copyStatus)
			if _, ok := publishedEdgeAccessPoint(&copyNode, copyStatus, now, 20*time.Second); ok {
				t.Fatal("ineligible node was published")
			}
		})
	}
}

func TestCenterAccessPointUsesValidatedSiteUDPAddress(t *testing.T) {
	now := time.Now()
	item, ok := centerAccessPoint(
		"center",
		"中心直连",
		"radio.example.com",
		60050,
		"福建省 福州市",
		"",
		100,
		now,
	)
	if !ok {
		t.Fatal("valid site UDP endpoint was rejected")
	}
	if item.ID != "center" || item.DisplayName != "中心直连" || item.UDPHost != "radio.example.com" || item.UDPPort != 60050 || item.Region != "福建省 福州市" || !item.HealthySampleAt.Equal(now) {
		t.Fatalf("unexpected center access point: %+v", item)
	}

	tests := []struct {
		name string
		id   string
		host string
		port int
	}{
		{name: "invalid public id", id: "center/internal", host: "radio.example.com", port: 60050},
		{name: "URL is not a UDP host", id: "center", host: "https://radio.example.com", port: 60050},
		{name: "missing host", id: "center", host: "", port: 60050},
		{name: "zero port", id: "center", host: "radio.example.com", port: 0},
		{name: "port too large", id: "center", host: "radio.example.com", port: 65536},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, accepted := centerAccessPoint(tt.id, "中心直连", tt.host, tt.port, "", "", 100, now); accepted {
				t.Fatal("invalid site UDP endpoint was published")
			}
		})
	}
}

func TestNormalizeAccessDiscoveryConfigForNATEndpoint(t *testing.T) {
	settings := &gormdb.AccessDiscoveryConfig{
		TokenTTLSeconds:      300,
		EdgeHealthTTLSeconds: 20,
		CacheMaxAgeSeconds:   5,
		Center: gormdb.AccessDiscoveryCenterConfig{
			Enabled:     true,
			PublicID:    " center ",
			DisplayName: " 中心直连 ",
			UDPHost:     " frp.example.com ",
			UDPPort:     16050,
			Region:      " 福建省 福州市 ",
			Priority:    100,
		},
	}

	got, err := normalizeAccessDiscoveryConfig(settings)
	if err != nil {
		t.Fatal(err)
	}
	if got.Center.PublicID != "center" || got.Center.DisplayName != "中心直连" || got.Center.UDPHost != "frp.example.com" || got.Center.UDPPort != 16050 || got.Center.Region != "福建省 福州市" {
		t.Fatalf("unexpected normalized settings: %+v", got)
	}
}

func TestNormalizeAccessDiscoveryConfigRejectsInvalidSettings(t *testing.T) {
	valid := gormdb.AccessDiscoveryConfig{
		TokenTTLSeconds:      300,
		EdgeHealthTTLSeconds: 20,
		CacheMaxAgeSeconds:   5,
		Center: gormdb.AccessDiscoveryCenterConfig{
			Enabled:     true,
			PublicID:    "center",
			DisplayName: "中心直连",
			UDPHost:     "radio.example.com",
			UDPPort:     60050,
		},
	}

	tests := []struct {
		name   string
		mutate func(*gormdb.AccessDiscoveryConfig)
	}{
		{name: "token ttl", mutate: func(c *gormdb.AccessDiscoveryConfig) { c.TokenTTLSeconds = 301 }},
		{name: "health ttl", mutate: func(c *gormdb.AccessDiscoveryConfig) { c.EdgeHealthTTLSeconds = 0 }},
		{name: "cache age", mutate: func(c *gormdb.AccessDiscoveryConfig) { c.CacheMaxAgeSeconds = 31 }},
		{name: "center host missing", mutate: func(c *gormdb.AccessDiscoveryConfig) { c.Center.UDPHost = "" }},
		{name: "center port", mutate: func(c *gormdb.AccessDiscoveryConfig) { c.Center.UDPPort = 65536 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := valid
			tt.mutate(&settings)
			if _, err := normalizeAccessDiscoveryConfig(&settings); err == nil {
				t.Fatal("invalid settings were accepted")
			}
		})
	}
}
