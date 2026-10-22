package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCDNConfigYAMLAndDefaults(t *testing.T) {
	var cfg Configuration
	if err := yaml.Unmarshal([]byte(`
Storage:
  UseCDN: true
  CDNBaseURL: " https://cdn.example.com/files/ "
  CDNKey: "${EDGEONE_CDN_KEY}"
DeviceAuth:
  AESKey: "01234567890123456789012345678901"
`), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Storage.UseCDN || cfg.Storage.CDNBaseURL != "https://cdn.example.com/files" ||
		cfg.Storage.CDNSignParam != "sign" || cfg.Storage.CDNExpirySeconds != 3600 ||
		cfg.Storage.CDNKey != "${EDGEONE_CDN_KEY}" {
		t.Fatal("CDN YAML configuration or defaults were not applied")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil || !strings.Contains(string(data), `CDNKey: ${EDGEONE_CDN_KEY}`) {
		t.Fatalf("CDN key reference was not preserved when saving configuration: %v", err)
	}
}

func TestCDNSnakeCaseDeploymentConfig(t *testing.T) {
	var cfg Configuration
	if err := yaml.Unmarshal([]byte(`
Storage:
  cdn_enabled: true
  cdn_base_url: https://oss-fz.silverdragon.cn/draarls
  cdn_sign_param: sign
  cdn_sign_ttl: 24h
  cdn_sign_key: "7CG1OPB25MAR2Ko7mt0tVf3EoK"
DeviceAuth:
  AESKey: "01234567890123456789012345678901"
`), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Storage.UseCDN || cfg.Storage.CDNBaseURL != "https://oss-fz.silverdragon.cn/draarls" ||
		cfg.Storage.CDNSignParam != "sign" || cfg.Storage.CDNExpirySeconds != 24*60*60 ||
		cfg.Storage.CDNKey != "7CG1OPB25MAR2Ko7mt0tVf3EoK" {
		t.Fatalf("snake_case CDN config was not normalized: %+v", cfg.Storage)
	}
}

func TestCDNConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*StorageConfig)
		field  string
	}{
		{"missing base", func(c *StorageConfig) { c.CDNBaseURL = "" }, "CDNBaseURL"},
		{"relative base", func(c *StorageConfig) { c.CDNBaseURL = "/files" }, "CDNBaseURL"},
		{"invalid scheme", func(c *StorageConfig) { c.CDNBaseURL = "ftp://cdn.example.com" }, "CDNBaseURL"},
		{"credentials", func(c *StorageConfig) { c.CDNBaseURL = "https://user:pass@cdn.example.com" }, "CDNBaseURL"},
		{"query", func(c *StorageConfig) { c.CDNBaseURL += "?a=1" }, "CDNBaseURL"},
		{"empty query", func(c *StorageConfig) { c.CDNBaseURL += "?" }, "CDNBaseURL"},
		{"fragment", func(c *StorageConfig) { c.CDNBaseURL += "#part" }, "CDNBaseURL"},
		{"empty fragment", func(c *StorageConfig) { c.CDNBaseURL += "#" }, "CDNBaseURL"},
		{"invalid param", func(c *StorageConfig) { c.CDNSignParam = "sign&other" }, "CDNSignParam"},
		{"long param", func(c *StorageConfig) { c.CDNSignParam = strings.Repeat("s", 101) }, "CDNSignParam"},
		{"negative expiry", func(c *StorageConfig) { c.CDNExpirySeconds = -1 }, "CDNExpirySeconds"},
		{"large expiry", func(c *StorageConfig) { c.CDNExpirySeconds = MaxCDNExpirySeconds + 1 }, "CDNExpirySeconds"},
		{"missing key", func(c *StorageConfig) { c.CDNKey = " " }, "CDNKey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := StorageConfig{UseCDN: true, CDNBaseURL: "https://cdn.example.com", CDNKey: "test-secret"}
			tc.change(&cfg)
			if err := cfg.SetCDNDefaults(); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("invalid CDN configuration was not rejected for %s: %v", tc.field, err)
			}
		})
	}
	// Settings have no effect until the CDN switch is enabled.
	cfg := StorageConfig{CDNBaseURL: "not-a-url", CDNSignParam: "bad&param", CDNExpirySeconds: -1}
	if err := cfg.SetCDNDefaults(); err != nil {
		t.Fatalf("disabled CDN affected storage configuration: %v", err)
	}
}
