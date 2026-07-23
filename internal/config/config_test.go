package config

import "testing"

func TestDefaultConfigFileName(t *testing.T) {
	if DefaultConfigFileName != "config.yaml" {
		t.Fatalf("default config file = %q, want config.yaml", DefaultConfigFileName)
	}
}

func TestGetAllowedOriginsIncludesFrontendURL(t *testing.T) {
	cfg := &Configuration{}
	cfg.Web.AllowedOrigins = []string{
		"https://api.example.com/",
		"invalid-origin",
	}
	cfg.Web.FrontendURL = "https://app.example.com/dashboard"

	origins := cfg.GetAllowedOrigins()
	if len(origins) != 2 {
		t.Fatalf("expected 2 normalized origins, got %d (%v)", len(origins), origins)
	}
	if !containsOrigin(origins, "https://api.example.com") {
		t.Fatalf("expected explicit allowed origin to be preserved, got %v", origins)
	}
	if !containsOrigin(origins, "https://app.example.com") {
		t.Fatalf("expected frontend URL origin to be included, got %v", origins)
	}
}

func TestValidateAllowedOriginsAllowsFrontendURLInRelease(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(true)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}
	cfg.Web.FrontendURL = "https://app.example.com/docs"

	if err := cfg.ValidateAllowedOrigins(); err != nil {
		t.Fatalf("expected frontend URL to satisfy release origin validation, got %v", err)
	}
}

func TestValidateAllowedOriginsRejectsMissingOriginsInRelease(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(true)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}

	if err := cfg.ValidateAllowedOrigins(); err == nil {
		t.Fatal("expected release validation to fail when no origin is configured")
	}
}

func TestLegacyMinIOConfigMigratesToStorage(t *testing.T) {
	cfg := &Configuration{}
	cfg.LegacyMinIO = MinIOConfig{
		Endpoint:  "minio.example.com",
		AccessKey: "access",
		SecretKey: "secret",
		UseSSL:    true,
		Bucket:    "draarl",
		BasePath:  "https://cdn.example.com/draarl",
	}

	cfg.migrateLegacyStorageConfig()

	if cfg.Storage.MinIO.Endpoint != "minio.example.com" || cfg.Storage.MinIO.AccessKey != "access" {
		t.Fatalf("legacy MinIO config was not migrated: %+v", cfg.Storage.MinIO)
	}
	if cfg.LegacyMinIO.Endpoint != "" {
		t.Fatal("legacy MinIO config should be cleared after migration")
	}
}

func TestExplicitLocalDriverDoesNotMigrateLegacyMinIO(t *testing.T) {
	cfg := &Configuration{}
	cfg.Storage.Driver = "local"
	cfg.LegacyMinIO.Endpoint = "minio.example.com"

	cfg.migrateLegacyStorageConfig()

	if cfg.Storage.MinIO.Endpoint != "" {
		t.Fatal("explicit local driver must not inherit legacy MinIO config")
	}
}

func TestInterconnectResourceDefaults(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	r := cfg.Interconnect.Resources
	if r.MaxNodes != 256 || r.MaxPendingHandshakes != 64 || r.DataHardPPSPerNode != 100000 ||
		r.DataQueueGlobal != 4096 || r.ControlHardPPSPerNode != 2000 || r.MaxDeviceSessionsPerNode != 25000 {
		t.Fatalf("unexpected interconnect resource defaults: %#v", r)
	}
	if cfg.Interconnect.CredentialRotationGraceSeconds != 600 {
		t.Fatalf("credential rotation grace=%d", cfg.Interconnect.CredentialRotationGraceSeconds)
	}
}

func containsOrigin(origins []string, target string) bool {
	for _, origin := range origins {
		if origin == target {
			return true
		}
	}
	return false
}
