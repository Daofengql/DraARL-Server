package handler

import (
	"testing"
	"time"

	"draarl/internal/gormdb"
)

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
		TokenTTLSeconds:    300,
		CacheMaxAgeSeconds: 5,
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
		TokenTTLSeconds:    300,
		CacheMaxAgeSeconds: 5,
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
