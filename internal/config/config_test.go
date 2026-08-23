package config

import (
	"strings"
	"testing"
)

func TestDefaultConfigFileName(t *testing.T) {
	if DefaultConfigFileName != "config.yaml" {
		t.Fatalf("default config file = %q, want config.yaml", DefaultConfigFileName)
	}
}

func TestDatabaseTimezoneDefaultsAndDSN(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.Database.Charset = "utf8mb4"
	cfg.Database.Collate = "utf8mb4_unicode_ci"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Timezone != "Local" || strings.Contains(cfg.GetDSN(), "time_zone=") {
		t.Fatalf("legacy timezone behavior changed: timezone=%q dsn=%q", cfg.Database.Timezone, cfg.GetDSN())
	}

	cfg.Database.Timezone = "Asia/Shanghai"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	dsn := cfg.GetDSN()
	if !strings.Contains(dsn, "loc=Asia%2FShanghai") || !strings.Contains(dsn, "time_zone=%27Asia%2FShanghai%27") {
		t.Fatalf("explicit timezone missing from DSN: %q", dsn)
	}
}

func TestDatabaseTimezoneRejectsInvalidValue(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.Database.Timezone = "not/a-real-zone"
	if err := cfg.SetDefaults(); err == nil || !strings.Contains(err.Error(), "Database.Timezone") {
		t.Fatalf("invalid timezone accepted: %v", err)
	}
}

func TestParseProxyTrustedCIDRsRejectsConfiguredInvalidEntries(t *testing.T) {
	nets, err := ParseProxyTrustedCIDRs([]string{" 192.0.2.0/24 ", "2001:db8::/32"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 2 || !nets[0].Contains([]byte{192, 0, 2, 10}) {
		t.Fatalf("unexpected parsed proxy networks: %v", nets)
	}
	for _, cidrs := range [][]string{{""}, {"not-a-cidr"}, {"192.0.2.0/24", "bad"}} {
		if _, err := ParseProxyTrustedCIDRs(cidrs); err == nil {
			t.Fatalf("invalid proxy trusted CIDRs were accepted: %q", cidrs)
		}
	}
	if nets, err := ParseProxyTrustedCIDRs(nil); err != nil || len(nets) != 0 {
		t.Fatalf("empty compatibility list failed: nets=%v err=%v", nets, err)
	}
}

func TestConfigurationRejectsInvalidSystemProxyTrustedCIDR(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.System.ProxyProtocol = "v2"
	cfg.System.ProxyTrustedCIDRs = []string{"not-a-cidr"}
	if err := cfg.SetDefaults(); err == nil || !strings.Contains(err.Error(), "System.ProxyTrustedCIDRs") {
		t.Fatalf("unexpected System.ProxyTrustedCIDRs validation error: %v", err)
	}
}

func TestConfigurationRequiresProxyTrustedCIDRsForReleaseV2(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(true)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.System.ProxyProtocol = " V2 "
	if err := cfg.SetDefaults(); err == nil || !strings.Contains(err.Error(), "ProxyTrustedCIDRs") {
		t.Fatalf("expected release v2 trust-boundary validation error, got %v", err)
	}

	cfg.System.ProxyTrustedCIDRs = []string{"192.0.2.0/24"}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatalf("valid release v2 configuration rejected: %v", err)
	}
	if cfg.System.ProxyProtocol != "v2" {
		t.Fatalf("ProxyProtocol was not normalized: %q", cfg.System.ProxyProtocol)
	}
}

func TestConfigurationKeepsEmptyProxyTrustForDevelopmentV2(t *testing.T) {
	previousRelease := IsReleaseBuild()
	SetReleaseBuild(false)
	t.Cleanup(func() {
		SetReleaseBuild(previousRelease)
	})

	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.System.ProxyProtocol = " V2 "
	if err := cfg.SetDefaults(); err != nil {
		t.Fatalf("development v2 compatibility configuration rejected: %v", err)
	}
}

func TestConfigurationKeepsLegacyV1ValueCompatible(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.System.ProxyProtocol = " V1 "
	if err := cfg.SetDefaults(); err != nil {
		t.Fatalf("legacy v1 configuration rejected: %v", err)
	}
	if cfg.System.ProxyProtocol != "v1" {
		t.Fatalf("ProxyProtocol was not normalized: %q", cfg.System.ProxyProtocol)
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
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.LegacyMinIO = MinIOConfig{
		Endpoint:  "minio.example.com",
		AccessKey: "access",
		SecretKey: "secret",
		UseSSL:    true,
		Bucket:    "draarl",
		BasePath:  "https://cdn.example.com/draarl",
	}

	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}

	if cfg.Storage.MinIO.Endpoint != "minio.example.com" || cfg.Storage.MinIO.AccessKey != "access" {
		t.Fatalf("legacy MinIO config was not migrated: %+v", cfg.Storage.MinIO)
	}
	if cfg.Storage.MinIO.DownloadURLPrefix != "https://cdn.example.com/draarl" {
		t.Fatalf("legacy BasePath was not migrated to DownloadURLPrefix: %+v", cfg.Storage.MinIO)
	}
	if cfg.Storage.MinIO.BasePath != "" {
		t.Fatalf("legacy BasePath was retained after migration: %+v", cfg.Storage.MinIO)
	}
	if cfg.LegacyMinIO.Endpoint != "" {
		t.Fatal("legacy MinIO config should be cleared after migration")
	}
}

func TestBroadcastConfigDefaultsAndBounds(t *testing.T) {
	cfg := &BroadcastConfig{}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.QuietWindowSeconds != 5 || cfg.MaxAudioDurationSeconds != 30 ||
		cfg.MaxUploadBytes != 20*1024*1024 || cfg.ScanIntervalMS != 1000 ||
		cfg.TranscodeMemoryLimitMB != DefaultBroadcastTranscodeMemoryMB ||
		cfg.TranscodeCPULimitSeconds != DefaultBroadcastTranscodeCPUSeconds ||
		cfg.TranscodeWorkers != DefaultBroadcastTranscodeWorkers ||
		cfg.FFmpegPath != "ffmpeg" || cfg.FFprobePath != "ffprobe" {
		t.Fatalf("unexpected broadcast defaults: %#v", cfg)
	}

	invalid := BroadcastConfig{QuietWindowSeconds: 31}
	if err := invalid.SetDefaults(); err == nil {
		t.Fatal("quiet window above bound was accepted")
	}
	invalid = BroadcastConfig{MaxAudioDurationSeconds: MaxBroadcastDurationSeconds + 1}
	if err := invalid.SetDefaults(); err == nil {
		t.Fatal("duration above hard limit was accepted")
	}
	invalid = BroadcastConfig{TranscodeMemoryLimitMB: 511}
	if err := invalid.SetDefaults(); err == nil {
		t.Fatal("transcode memory limit below bound was accepted")
	}
	invalid = BroadcastConfig{TranscodeCPULimitSeconds: 301}
	if err := invalid.SetDefaults(); err == nil {
		t.Fatal("transcode CPU limit above bound was accepted")
	}
}

func TestDeviceAuthConfigDefaultsAndBounds(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.UDP.DeviceAuthWorkers != 4 || cfg.UDP.DeviceAuthQueueSize != 512 {
		t.Fatalf("unexpected device auth defaults: workers=%d queue=%d", cfg.UDP.DeviceAuthWorkers, cfg.UDP.DeviceAuthQueueSize)
	}

	cfg.UDP.DeviceAuthWorkers = 17
	cfg.UDP.DeviceAuthQueueSize = 2049
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.UDP.DeviceAuthWorkers != 16 || cfg.UDP.DeviceAuthQueueSize != 2048 {
		t.Fatalf("unexpected device auth clamps: workers=%d queue=%d", cfg.UDP.DeviceAuthWorkers, cfg.UDP.DeviceAuthQueueSize)
	}
}

func TestDeprecatedS3PublicBaseURLMigratesToDownloadURLPrefix(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	cfg.Storage.S3.PublicBaseURL = "https://downloads.example.com/draarl/"
	cfg.Storage.Profiles = map[string]StorageProfile{
		"archive": {S3: S3Config{PublicBaseURL: "/archive/"}},
	}
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3.DownloadURLPrefix != "https://downloads.example.com/draarl" {
		t.Fatalf("top-level prefix=%q", cfg.Storage.S3.DownloadURLPrefix)
	}
	if cfg.Storage.S3.PublicBaseURL != "" {
		t.Fatalf("deprecated top-level PublicBaseURL was retained: %q", cfg.Storage.S3.PublicBaseURL)
	}
	if got := cfg.Storage.Profiles["archive"].S3.DownloadURLPrefix; got != "/archive" {
		t.Fatalf("profile prefix=%q", got)
	}
	if got := cfg.Storage.Profiles["archive"].S3.PublicBaseURL; got != "" {
		t.Fatalf("deprecated profile PublicBaseURL was retained: %q", got)
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
	if cfg.Interconnect.SessionRecoveryWindowSeconds != 180 {
		t.Fatalf("session recovery window=%d", cfg.Interconnect.SessionRecoveryWindowSeconds)
	}
}

func TestGhostSessionLimitDefaults(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.GhostSessions.MaxSessionsPerOwner != DefaultGhostSessionsPerOwner {
		t.Fatalf("ghost session default=%d want=%d", cfg.GhostSessions.MaxSessionsPerOwner, DefaultGhostSessionsPerOwner)
	}
	if cfg.GhostSessions.MaxSubscriptionsPerSession != DefaultGhostSubscriptionsPerSession {
		t.Fatalf("ghost subscription default=%d want=%d", cfg.GhostSessions.MaxSubscriptionsPerSession, DefaultGhostSubscriptionsPerSession)
	}
	if cfg.MessageAPI.DefaultPageSize != DefaultMessageAPIPageSize ||
		cfg.MessageAPI.MaxPageSize != DefaultMessageAPIMaxPageSize ||
		cfg.MessageAPI.RequestsPerMinutePerUser != DefaultMessageAPIRequestsPerUser ||
		cfg.MessageAPI.RequestsPerMinutePerIP != DefaultMessageAPIRequestsPerIP ||
		cfg.MessageAPI.MaxConcurrentQueries != DefaultMessageAPIMaxConcurrent {
		t.Fatalf("unexpected message API defaults: %+v", cfg.MessageAPI)
	}
}

func TestMessageAPIConfigValidation(t *testing.T) {
	for _, test := range []MessageAPIConfig{
		{DefaultPageSize: 101, MaxPageSize: 100, RequestsPerMinutePerUser: 1, RequestsPerMinutePerIP: 1, MaxConcurrentQueries: 1},
		{DefaultPageSize: 1, MaxPageSize: MaxMessageAPIPageSize + 1, RequestsPerMinutePerUser: 1, RequestsPerMinutePerIP: 1, MaxConcurrentQueries: 1},
		{DefaultPageSize: 1, MaxPageSize: 1, RequestsPerMinutePerUser: -1, RequestsPerMinutePerIP: 1, MaxConcurrentQueries: 1},
		{DefaultPageSize: 1, MaxPageSize: 1, RequestsPerMinutePerUser: 1, RequestsPerMinutePerIP: 1, MaxConcurrentQueries: -1},
	} {
		if err := test.SetDefaults(); err == nil {
			t.Fatalf("invalid message API config was accepted: %+v", test)
		}
	}
	cfg := MessageAPIConfig{MaxPageSize: 20}
	if err := cfg.SetDefaults(); err != nil || cfg.DefaultPageSize != 20 {
		t.Fatalf("configured maximum did not bound default: cfg=%+v err=%v", cfg, err)
	}
}

func TestGhostSessionLimitValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  GhostSessionConfig
	}{
		{name: "zero sessions", cfg: GhostSessionConfig{MaxSessionsPerOwner: -1, MaxSubscriptionsPerSession: 1}},
		{name: "too many sessions", cfg: GhostSessionConfig{MaxSessionsPerOwner: MaxGhostSessionsPerOwner + 1, MaxSubscriptionsPerSession: 1}},
		{name: "zero subscriptions", cfg: GhostSessionConfig{MaxSessionsPerOwner: 1, MaxSubscriptionsPerSession: -1}},
		{name: "too many subscriptions", cfg: GhostSessionConfig{MaxSessionsPerOwner: 1, MaxSubscriptionsPerSession: MaxGhostSubscriptionsPerSession + 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cfg.SetDefaults(); err == nil {
				t.Fatal("expected invalid ghost session limits to be rejected")
			}
		})
	}
}

func TestClientResourceUploadLimitDefault(t *testing.T) {
	cfg := &Configuration{}
	cfg.DeviceAuth.AESKey = "01234567890123456789012345678901"
	if err := cfg.SetDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.UploadLimits.ClientResourceBytes != DefaultClientResourceMaxBytes {
		t.Fatalf("client resource limit=%d want=%d", cfg.Storage.UploadLimits.ClientResourceBytes, DefaultClientResourceMaxBytes)
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
