package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadIgnoresRemovedUDPProxyOptions(t *testing.T) {
	oldConfig, oldPath, oldRelease := Config, configFilePath, IsReleaseBuild()
	t.Cleanup(func() { Config, configFilePath = oldConfig, oldPath; SetReleaseBuild(oldRelease) })
	SetReleaseBuild(true)
	for _, obsolete := range []string{
		"ProxyProtocol: v2\n  ProxyTrustedCIDRs: []",
		"ProxyProtocol: unsupported\n  ProxyTrustedCIDRs: [bad-cidr]",
		"ProxyProtocol: {invalid: type}\n  ProxyTrustedCIDRs: 123",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		data := "System:\n  " + obsolete + "\nDeviceAuth:\n  AESKey: '01234567890123456789012345678901'\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("obsolete options prevented startup: %v", err)
		}
		if err := cfg.SaveToFile(path); err != nil {
			t.Fatal(err)
		}
		saved, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(saved), "ProxyProtocol") || strings.Contains(string(saved), "\n    ProxyTrustedCIDRs:") {
			t.Fatal("removed options were saved")
		}
	}
}

func TestHTTPProxyTrustStillValidated(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.System.HTTPTrustedProxyCIDRs = []string{"invalid"}
	if err := cfg.SetDefaults(); err == nil {
		t.Fatal("HTTP trust validation was removed")
	}
}
